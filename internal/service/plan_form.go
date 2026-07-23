package service

import (
	"context"
	"sort"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// PlanFormService — сборка формы «План подвода» экрана «Рассылка» (перенос
// gtport SmsPlan.GenerateSMSPlan). По терминалу — сводная карточка «ЖД сутки»:
//
//	«Вчера» (факт)   — учётный лист грузовой работы за прошлые сутки как есть.
//	«Сегодня» (прогноз) — движок аналитики (calcCargoWorkDay) над поездами
//	                    расчётных суток: ПРИБЫВШИЕ сегодня (история) + ПОДХОДЯЩИЕ
//	                    сегодня (снимок дислокации), от остатка на начало суток.
//	                    Это отличие от карточки «Грузовой работы», где движок
//	                    видит только прибывших: план подвода смотрит вперёд.
//	Поезда           — прибывшие («приб») + все подходящие, для списка под картинку.
//
// Переиспуёт CargoWorkService (учётный лист, способность линий, час отсечки) и
// движок calcCargoWorkDay; подход берёт из ActualCache тем же фильтром, что
// NearestService (не на станции назначения).
type PlanFormService struct {
	cw      *CargoWorkService
	actual  *ActualCache
	history port.HistoryRepository
	dir     *DirectoryCache
}

func NewPlanFormService(cw *CargoWorkService, actual *ActualCache, history port.HistoryRepository, dir *DirectoryCache) *PlanFormService {
	return &PlanFormService{cw: cw, actual: actual, history: history, dir: dir}
}

// PlanFormLineDTO — одна линия учёта: факт вчера + прогноз сегодня.
type PlanFormLineDTO struct {
	CargoKey string `json:"cargo_key"`
	Label    string `json:"label"`

	// «Вчера» (факт прошлых суток).
	Ost18    int `json:"ost_18"`
	Prib     int `json:"prib"`
	UsefulY  int `json:"useful_y"`  // полезное образование вчера
	TotalY   int `json:"total_y"`   // образование вчера
	VigrFact int `json:"vigr_fact"` // выгрузка вчера (факт порта)
	OstY     int `json:"ost_y"`     // остаток на конец вчера

	// «Сегодня» (прогноз по подходу + прибывшим).
	OstToday      int    `json:"ost_today"`      // остаток на начало суток (= вчерашний остаток)
	UsefulToday   int    `json:"useful_today"`   // полезное образование прогноз
	TotalToday    int    `json:"total_today"`    // образование прогноз
	DowntimeToday string `json:"downtime_today"` // простой порта прогноз «Ч:ММ»
}

// PlanFormTrainDTO — поезд для списка под картинку (приб или подход).
type PlanFormTrainDTO struct {
	Index   string            `json:"index"`
	Arrived bool              `json:"arrived"`  // true — прибывший («приб»)
	Time    *domain.LocalTime `json:"time"`     // прибытие (приб) либо прогноз (подход), ЖД
	Count   int               `json:"count"`    // вагонов
	Cargo   string            `json:"cargo"`    // группа груза (для строки «… уголь»)
	Sms     string            `json:"sms,omitempty"`
}

// PlanFormTerminalDTO — карточка одного терминала.
type PlanFormTerminalDTO struct {
	Terminal string             `json:"terminal"`
	Color    string             `json:"color"`
	Lines    []PlanFormLineDTO  `json:"lines"`
	Trains   []PlanFormTrainDTO `json:"trains"`
}

// Form собирает карточки всех терминалов на дату (ЖД-сутки, yyyy-MM-dd).
func (s *PlanFormService) Form(ctx context.Context, date time.Time) ([]PlanFormTerminalDTO, error) {
	targets := terminalTargets(s.dir)
	out := make([]PlanFormTerminalDTO, 0, len(targets))
	for _, t := range targets {
		card, err := s.terminalCard(ctx, date, t)
		if err != nil {
			return nil, err
		}
		out = append(out, card)
	}
	return out, nil
}

func (s *PlanFormService) terminalCard(ctx context.Context, date time.Time, t TargetDTO) (PlanFormTerminalDTO, error) {
	card := PlanFormTerminalDTO{Terminal: t.Name, Color: t.Color}

	// «Вчера» — учётный лист прошлых суток как есть.
	yest, err := s.cw.Day(ctx, date.AddDate(0, 0, -1), t.Name)
	if err != nil {
		return card, err
	}
	// «Сегодня» база — учётный лист текущих суток: остаток на начало (ost_18) и
	// способность линий (pc). Образование/простой пересчитаем над подходом.
	today, err := s.cw.Day(ctx, date, t.Name)
	if err != nil {
		return card, err
	}

	yestByKey := map[string]CargoWorkLineDTO{}
	for _, l := range yest.Lines {
		yestByKey[l.CargoKey] = l
	}

	// Поезда расчётных суток: прибывшие (история) + подходящие (снимок).
	d := domain.LocalTime(dayStart(date))
	arrived, err := s.history.ArrivedRows(ctx, d, d, []string{t.Name})
	if err != nil {
		return card, err
	}
	approaching := s.approaching(t.Name)

	cutoff := s.cw.CutoffHour()
	start := dayStart(date)

	for _, tl := range today.Lines {
		y := yestByKey[tl.CargoKey]

		// Прогноз сегодня: движок над (приб + подход) этой линии, от ost_18.
		arrTrains, _ := cargoWorkTrains(arrived, tl.CargoKey)
		trains := append(arrTrains, approaching.trainsForLine(tl.CargoKey)...)
		a := calcCargoWorkDay(start, tl.Ost18, tl.Pc, cutoff, trains)

		card.Lines = append(card.Lines, PlanFormLineDTO{
			CargoKey: tl.CargoKey,
			Label:    tl.Label,
			Ost18:    y.Ost18,
			Prib:     y.Prib,
			UsefulY:  y.UsefulFormation,
			TotalY:   y.TotalFormation,
			VigrFact: y.VigrFact,
			OstY:     y.Ost,
			OstToday:      tl.Ost18,
			UsefulToday:   a.UsefulFormation,
			TotalToday:    a.TotalFormation,
			DowntimeToday: a.Downtime,
		})
	}

	card.Trains = s.trainList(arrived, approaching)
	return card, nil
}

// ── Подход из снимка (тем же фильтром, что NearestService) ───────────────────

// approachingTrain — подходящий поезд, свёрнутый по IdDisl.
type approachingTrain struct {
	index string
	time  *domain.LocalTime // прогноз (ProgJd → RaschJd)
	byKey map[string]int    // cargo_group → вагонов
	total int
	cargo string // преобладающая группа (для строки списка)
	sms   string
}

type approachingSet struct {
	trains []approachingTrain
}

// trainsForLine — поезда для аналитики линии cargoKey (пусто — вся линия).
func (a approachingSet) trainsForLine(cargoKey string) []CargoWorkTrain {
	out := []CargoWorkTrain{}
	for _, t := range a.trains {
		if t.time == nil {
			continue
		}
		n := t.total
		if cargoKey != "" {
			n = t.byKey[cargoKey]
		}
		if n <= 0 {
			continue
		}
		out = append(out, CargoWorkTrain{Name: t.index, Wagons: n, Arrival: *t.time})
	}
	return out
}

// approaching собирает подходящие поезда терминала: не на станции назначения
// (статус 9/≥10 исключены — они «прибывшие»), свёртка по IdDisl.
func (s *PlanFormService) approaching(terminal string) approachingSet {
	if s.actual == nil {
		return approachingSet{}
	}
	byId := map[string]*approachingTrain{}
	var order []string
	for _, r := range s.actual.All() {
		st := 0
		if r.Status != nil {
			st = *r.Status
		}
		if st == 9 || st >= 10 || r.IdDisl == "" || r.Naznach != terminal {
			continue
		}
		t, ok := byId[r.IdDisl]
		if !ok {
			t = &approachingTrain{index: r.Index, byKey: map[string]int{}, time: firstLT(r.ProgJd, r.RaschJd)}
			if t.index == "" {
				t.index = r.IndexMain
			}
			t.sms = r.Sms1
			byId[r.IdDisl] = t
			order = append(order, r.IdDisl)
		}
		t.total++
		if r.CargoGroup != "" {
			t.byKey[r.CargoGroup]++
		}
	}
	out := approachingSet{}
	for _, id := range order {
		t := byId[id]
		t.cargo = dominantCargo(t.byKey)
		out.trains = append(out.trains, *t)
	}
	return out
}

// trainList — список поездов под картинку: прибывшие («приб») + подходящие,
// по времени (без времени — в конец).
func (s *PlanFormService) trainList(arrived []domain.VagonHistory, ap approachingSet) []PlanFormTrainDTO {
	var out []PlanFormTrainDTO

	// Прибывшие: свёртка по индексу поезда (IndexPp).
	type arr struct {
		index string
		time  *domain.LocalTime
		byKey map[string]int
		total int
		sms   string
	}
	byIdx := map[string]*arr{}
	var order []string
	for _, r := range arrived {
		idx := r.IndexPp
		if idx == "" {
			idx = r.IndexMain
		}
		a, ok := byIdx[idx]
		if !ok {
			a = &arr{index: idx, time: r.DatePrib, byKey: map[string]int{}, sms: r.Sms1}
			byIdx[idx] = a
			order = append(order, idx)
		}
		a.total++
		if r.CargoGroup != "" {
			a.byKey[r.CargoGroup]++
		}
	}
	for _, idx := range order {
		a := byIdx[idx]
		out = append(out, PlanFormTrainDTO{
			Index: a.index, Arrived: true, Time: a.time,
			Count: a.total, Cargo: dominantCargo(a.byKey), Sms: a.sms,
		})
	}

	// Подходящие.
	for _, t := range ap.trains {
		out = append(out, PlanFormTrainDTO{
			Index: t.index, Arrived: false, Time: t.time,
			Count: t.total, Cargo: t.cargo, Sms: t.sms,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := out[i].Time, out[j].Time
		switch {
		case ti == nil && tj == nil:
			return out[i].Index < out[j].Index
		case ti == nil:
			return false
		case tj == nil:
			return true
		default:
			return ti.Time().Before(tj.Time())
		}
	})
	return out
}

// dominantCargo — группа груза с наибольшим числом вагонов (для строки «… уголь»).
func dominantCargo(byKey map[string]int) string {
	best, bestN := "", 0
	for k, n := range byKey {
		if n > bestN || (n == bestN && k < best) {
			best, bestN = k, n
		}
	}
	return best
}
