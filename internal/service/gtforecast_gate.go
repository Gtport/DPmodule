package service

import (
	"sort"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Очередь у ворот как факт (docs/ANALYTICS.md §7.1, шаг 4 плана владельца
// 05.09.2026): гружёные вагоны, уже стоящие на станции назначения — статусы 9
// «кандидат в прибывшие» и 10 «прибыл» (docs/STATUSES.md), но ещё не
// выгруженные (после выгрузки — 12). Это слой между дислокацией и портом,
// которого в симуляции нет: там «ожидание поезда» — расчётное. Фаза по коду
// последней операции (справочник cargo_operations): подан на подъездной путь /
// выгружается — «на фронте», прибыл на станцию и стоит — «ждёт подачи».

// Фазы поезда у ворот.
const (
	GtGateOnFront     = "на_фронте"
	GtGateWaitingFeed = "ждёт_подачи"
)

// gtGateFrontCodes — коды операций, означающие, что вагон подан под выгрузку
// или выгружается: 80 подача на ПП, 78 прочие подачи ГУ-45М, 73 подача на
// тракционные пути, 20/21/29 выгрузка, 28 выгрузка без зачёта, 44 окончание
// операции выгрузки. Всё прочее у прибывшего (8 прибытие на станцию, 0 окончание
// перевозки, 81 уборка с ПП…) — стоит на станции.
var gtGateFrontCodes = map[string]bool{
	"80": true, "78": true, "73": true, "20": true, "21": true, "29": true, "28": true, "44": true,
}

// GtGateTrainDTO — поезд у ворот на момент расчёта.
type GtGateTrainDTO struct {
	Index          string            `json:"index"`
	Terminal       string            `json:"terminal"` // терминал большинства вагонов (naznach)
	VagonCount     int               `json:"vagon_count"`
	Phase          string            `json:"phase"`  // на_фронте | ждёт_подачи (по большинству вагонов)
	Status         string            `json:"status"` // 10, если хоть один вагон прибыл по факту; иначе 9
	DatePrib       *domain.LocalTime `json:"date_prib,omitempty"`        // факт прибытия (ЖД-штамп, как в снимке), самый ранний по вагонам
	HoursAtStation *float64          `json:"hours_at_station,omitempty"` // от факта прибытия (МСК) до момента расчёта
	CodeOper       string            `json:"code_oper"`                  // последняя операция (самая поздняя по вагонам)
	Oper           string            `json:"oper"`
	TimeOp         *domain.LocalTime `json:"time_op,omitempty"`
	StationOper    string            `json:"station_oper"`
}

// gtGateTrains группирует вагоны статусов 9/10 в адрес терминалов станции в
// поезда у ворот. Ключ — плановая нитка (index_pp) либо индекс, плюс станция
// операции. now — момент расчёта (МСК).
func gtGateTrains(rows []domain.Dislocation, known map[string]bool, now time.Time) []GtGateTrainDTO {
	type agg struct {
		t     GtGateTrainDTO
		byNaz map[string]int
		front int
		prib  *time.Time // МСК
	}
	groups := map[string]*agg{}
	var order []string

	for i := range rows {
		r := &rows[i]
		if r.Status == nil || (*r.Status != 9 && *r.Status != 10) || !known[r.Naznach] {
			continue
		}
		index := r.Index
		if r.IndexPp != "" {
			index = r.IndexPp
		}
		if index == "" {
			index = "Б/И"
		}
		key := index + "|" + r.StationOper
		g, ok := groups[key]
		if !ok {
			g = &agg{t: GtGateTrainDTO{Index: index, StationOper: r.StationOper, Status: "9"}, byNaz: map[string]int{}}
			groups[key] = g
			order = append(order, key)
		}
		g.t.VagonCount++
		g.byNaz[r.Naznach]++
		if *r.Status == 10 {
			g.t.Status = "10"
		}
		if gtGateFrontCodes[r.CodeOper] {
			g.front++
		}
		if r.TimeOp != nil && (g.t.TimeOp == nil || time.Time(*r.TimeOp).After(time.Time(*g.t.TimeOp))) {
			g.t.TimeOp, g.t.CodeOper, g.t.Oper = r.TimeOp, r.CodeOper, r.Oper
		}
		if r.DatePrib != nil {
			msk := gtFromJd(time.Time(*r.DatePrib))
			if g.prib == nil || msk.Before(*g.prib) {
				g.prib = &msk
				g.t.DatePrib = r.DatePrib
			}
		}
	}

	out := make([]GtGateTrainDTO, 0, len(order))
	for _, key := range order {
		g := groups[key]
		// Терминал большинства вагонов; при равенстве — первый по алфавиту (детерминизм).
		nazs := make([]string, 0, len(g.byNaz))
		for naz := range g.byNaz {
			nazs = append(nazs, naz)
		}
		sort.Strings(nazs)
		best := -1
		for _, naz := range nazs {
			if g.byNaz[naz] > best {
				best, g.t.Terminal = g.byNaz[naz], naz
			}
		}
		if g.front > 0 && g.front*2 >= g.t.VagonCount {
			g.t.Phase = GtGateOnFront
		} else {
			g.t.Phase = GtGateWaitingFeed
		}
		if g.prib != nil {
			h := now.Sub(*g.prib).Hours()
			if h < 0 {
				h = 0
			}
			g.t.HoursAtStation = &h
		}
		out = append(out, g.t)
	}
	// Раньше прибывшие — первыми; без факта прибытия — в конце.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].HoursAtStation, out[j].HoursAtStation
		switch {
		case a == nil && b == nil:
			return false
		case a == nil:
			return false
		case b == nil:
			return true
		}
		return *a > *b
	})
	return out
}

// gtFromJd — ЖД-штамп → московское время (обратное jd18: час ≥ 18 → −сутки).
func gtFromJd(t time.Time) time.Time {
	if t.Hour() >= 18 {
		return t.Add(-24 * time.Hour)
	}
	return t
}
