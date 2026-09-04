package service

import (
	"math"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Агрегаты обстановки на момент расчёта — часть паспорта снапшота прогноза ГТ
// (docs/ANALYTICS.md §7.1, решение владельца 05.09.2026). Считаются при
// сохранении из очереди, диаграмм и свободных ниток, чтобы панель «обстановка
// дня» (порт ↔ дислокация ↔ брошенные ↔ план подвода) собиралась прямо из базы,
// одним методом, без перерасчёта по сырым слепкам. Летом 2026 именно волна
// 24–72 ч хода предсказывала броски назавтра (Спирмен +0,43), остаток в порту —
// нет; корзины ниже — те, что понадобились в том разборе.

// GtSnapshotSummary — агрегаты по станции в целом (Total; там же свободные
// нитки — пул станции) и по терминалам (ключ — терминал назначения).
type GtSnapshotSummary struct {
	Total      GtSnapshotSummaryRow            `json:"total"`
	ByTerminal map[string]GtSnapshotSummaryRow `json:"by_terminal"`
}

// GtSnapshotSummaryRow — одна строка агрегатов.
//
// Поезд относится к терминалу, куда адресовано больше его вагонов; вагоны
// считаются по подгруппам точно. Корзины по часам хода (to_go) — только у
// поездов в пути (без брошенных и прибывших): «у ворот» ≤ 24 ч, «волна»
// 24–72 ч, 3–7 суток, дальше 7 суток. Брошенные — статус 5 в очереди.
// Сутки — ЖД-сутки от стартовой даты расчёта (0 — стартовые, 1 — следующие).
type GtSnapshotSummaryRow struct {
	TrainsQueue     int     `json:"trains_queue"`      // поездов в очереди (в пути + брошенные, без прибывших)
	WagonsQueue     int     `json:"wagons_queue"`      // вагонов в очереди
	TrainsGate      int     `json:"trains_gate"`       // в пути, ≤ 24 ч хода
	WagonsGate      int     `json:"wagons_gate"`
	TrainsWave      int     `json:"trains_wave"`       // в пути, 24–72 ч хода
	WagonsWave      int     `json:"wagons_wave"`
	Trains3to7      int     `json:"trains_3_7d"`       // в пути, 3–7 суток хода
	TrainsFar       int     `json:"trains_far"`        // в пути, дальше 7 суток
	TrainsBros      int     `json:"trains_bros"`       // брошенных в очереди
	WagonsBros      int     `json:"wagons_bros"`
	TrainsBrosGate  int     `json:"trains_bros_gate"`  // брошенных в ≤ 24 ч хода
	TrainsWaiting   int     `json:"trains_waiting"`    // поездов, ждущих терминал по расчёту (операция с ожиданием)
	NitkiDay0       int     `json:"nitki_day0"`        // поездов с ниткой плана на стартовые сутки
	NitkiDay1       int     `json:"nitki_day1"`        // … на следующие сутки
	WagonsNitkiDay1 int     `json:"wagons_nitki_day1"` // вагонов в нитках на следующие сутки
	ArrivalDay0     int     `json:"arrival_day0"`      // прибытие по диаграмме за стартовые сутки, вагонов
	ArrivalDay1     int     `json:"arrival_day1"`      // … за следующие сутки
	RemainingDay0   int     `json:"remaining_day0"`    // остаток на конец стартовых суток по расчёту
	CarriedDay0     int     `json:"carried_day0"`      // поездов перенесено на следующие сутки
	IdleMinDay0     int     `json:"idle_min_day0"`     // простой линий за стартовые сутки, мин (сумма по линиям)
	NormSpeed       int     `json:"norm_speed"`        // сумма норм линий (знаменатель нагрузки плана)
	PlanLoadDay1    float64 `json:"plan_load_day1"`    // вагонов в нитках на завтра / сумма норм
	FreeSlotsH0     int     `json:"free_slots_h0"`     // свободных ниток станции на стартовые сутки (только Total)
	FreeSlotsH1     int     `json:"free_slots_h1"`
	FreeSlotsH2     int     `json:"free_slots_h2"`
	// Очередь у ворот по факту (gtforecast_gate.go): прибыли на станцию
	// назначения, не выгружены (статусы 9/10).
	TrainsAtStation   int     `json:"trains_at_station"`
	WagonsAtStation   int     `json:"wagons_at_station"`
	TrainsOnFront     int     `json:"trains_on_front"`      // из них поданы / выгружаются
	TrainsWaitingFeed int     `json:"trains_waiting_feed"`  // из них ждут подачи
	MaxHoursAtStation float64 `json:"max_hours_at_station"` // дольше всех стоящий, ч от факта прибытия
}

// gtSnapshotSummary считает агрегаты по результату симуляции и очереди у ворот.
// start — стартовые ЖД-сутки расчёта (дата без времени, как в
// GtSimulateRequest.StartDate); gate — поезда у ворот на момент расчёта.
func gtSnapshotSummary(start time.Time, res GtSimulateDTO, gate []GtGateTrainDTO) GtSnapshotSummary {
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	total := &GtSnapshotSummaryRow{}
	rows := map[string]*GtSnapshotSummaryRow{}
	row := func(term string) *GtSnapshotSummaryRow {
		r, ok := rows[term]
		if !ok {
			r = &GtSnapshotSummaryRow{}
			rows[term] = r
		}
		return r
	}
	// Смещение ЖД-суток от старта; PlanJd и Jd нитки уже в ЖД-шкале (дата = ЖД-сутки).
	dayOf := func(lt *domain.LocalTime) int {
		if lt == nil {
			return math.MinInt32
		}
		t := time.Time(*lt)
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return int(math.Round(d.Sub(start).Hours() / 24))
	}

	// Ожидающие терминал — из операций диаграмм (ожидание > 0 у операции поезда).
	waiting := map[string]bool{}
	for _, f := range res.Flows {
		for _, d := range f.Days {
			for _, op := range d.Operations {
				if !op.IsRemainder && op.WaitMin > 0 && op.TrainIndex != "" {
					waiting[op.TrainIndex] = true
				}
			}
		}
	}

	for _, t := range res.Trains {
		if t.IsArrived {
			continue
		}
		wagonsBy := map[string]int{}
		term, best := "", -1
		for _, sg := range t.SubGroups {
			wagonsBy[sg.Naznach] += sg.VagonCount
			if wagonsBy[sg.Naznach] > best {
				best, term = wagonsBy[sg.Naznach], sg.Naznach
			}
		}
		major := row(term)
		bros := t.Status == "5"
		toGo := -1.0
		if t.ToGo != nil {
			toGo = *t.ToGo
		}
		// Категория поезда — одна на все его вагоны.
		var trainsInc func(r *GtSnapshotSummaryRow)
		var wagonsInc func(r *GtSnapshotSummaryRow, n int)
		switch {
		case bros:
			trainsInc = func(r *GtSnapshotSummaryRow) {
				r.TrainsBros++
				if toGo >= 0 && toGo <= 24 {
					r.TrainsBrosGate++
				}
			}
			wagonsInc = func(r *GtSnapshotSummaryRow, n int) { r.WagonsBros += n }
		case toGo < 0:
			trainsInc = func(*GtSnapshotSummaryRow) {}
			wagonsInc = func(*GtSnapshotSummaryRow, int) {}
		case toGo <= 24:
			trainsInc = func(r *GtSnapshotSummaryRow) { r.TrainsGate++ }
			wagonsInc = func(r *GtSnapshotSummaryRow, n int) { r.WagonsGate += n }
		case toGo <= 72:
			trainsInc = func(r *GtSnapshotSummaryRow) { r.TrainsWave++ }
			wagonsInc = func(r *GtSnapshotSummaryRow, n int) { r.WagonsWave += n }
		case toGo <= 168:
			trainsInc = func(r *GtSnapshotSummaryRow) { r.Trains3to7++ }
			wagonsInc = func(*GtSnapshotSummaryRow, int) {}
		default:
			trainsInc = func(r *GtSnapshotSummaryRow) { r.TrainsFar++ }
			wagonsInc = func(*GtSnapshotSummaryRow, int) {}
		}

		planDay := dayOf(t.PlanJd)
		for _, r := range []*GtSnapshotSummaryRow{total, major} {
			r.TrainsQueue++
			trainsInc(r)
			if waiting[t.Index] {
				r.TrainsWaiting++
			}
			switch planDay {
			case 0:
				r.NitkiDay0++
			case 1:
				r.NitkiDay1++
			}
		}
		total.WagonsQueue += t.VagonCount
		wagonsInc(total, t.VagonCount)
		if planDay == 1 {
			total.WagonsNitkiDay1 += t.VagonCount
		}
		for naz, n := range wagonsBy {
			r := row(naz)
			r.WagonsQueue += n
			wagonsInc(r, n)
			if planDay == 1 {
				r.WagonsNitkiDay1 += n
			}
		}
	}

	for _, f := range res.Flows {
		r := row(f.Terminal)
		if len(f.Days) > 0 {
			d := f.Days[0]
			for _, x := range []*GtSnapshotSummaryRow{total, r} {
				x.ArrivalDay0 += d.Arrival
				x.RemainingDay0 += d.Remaining
				x.CarriedDay0 += len(d.CarriedOver)
				x.IdleMinDay0 += int(math.Round(d.TotalWaitMin))
				x.NormSpeed += d.NormSpeed
			}
		}
		if len(f.Days) > 1 {
			total.ArrivalDay1 += f.Days[1].Arrival
			r.ArrivalDay1 += f.Days[1].Arrival
		}
	}

	for _, sl := range res.FreeSlots {
		jd := sl.Jd
		switch dayOf(&jd) {
		case 0:
			total.FreeSlotsH0++
		case 1:
			total.FreeSlotsH1++
		case 2:
			total.FreeSlotsH2++
		}
	}

	for _, g := range gate {
		for _, r := range []*GtSnapshotSummaryRow{total, row(g.Terminal)} {
			r.TrainsAtStation++
			r.WagonsAtStation += g.VagonCount
			if g.Phase == GtGateOnFront {
				r.TrainsOnFront++
			} else {
				r.TrainsWaitingFeed++
			}
			if g.HoursAtStation != nil && *g.HoursAtStation > r.MaxHoursAtStation {
				r.MaxHoursAtStation = math.Round(*g.HoursAtStation*10) / 10
			}
		}
	}

	load := func(r *GtSnapshotSummaryRow) {
		if r.NormSpeed > 0 {
			r.PlanLoadDay1 = math.Round(float64(r.WagonsNitkiDay1)/float64(r.NormSpeed)*100) / 100
		}
	}
	load(total)
	out := GtSnapshotSummary{Total: *total, ByTerminal: map[string]GtSnapshotSummaryRow{}}
	for term, r := range rows {
		if term == "" {
			continue
		}
		load(r)
		out.ByTerminal[term] = *r
	}
	return out
}
