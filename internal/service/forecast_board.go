package service

import (
	"sort"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// ForecastTrain — агрегированный поезд для экрана «Прогнозы»: состав + прогнозные поля
// Stage 3/4. Прогнозные поля одинаковы внутри поезда (берём первое непустое значение).
type ForecastTrain struct {
	IdDisl     string            `json:"id_disl"`
	IndexPp    string            `json:"index_pp"`
	Naznach    string            `json:"naznach"`     // терминал (площадка назначения)
	StanNazn   string            `json:"stan_nazn"`   // станция назначения
	CargoGroup string            `json:"cargo_group"` // род (УГОЛЬ/МЕТАЛЛ)
	CargoS     string            `json:"cargo_s"`     // имя груза
	VagonCount int               `json:"vagon_count"`
	Ves        float64           `json:"ves"`       // масса состава, тонны
	HasPlan    bool              `json:"has_plan"`  // нитка задана планом (зелёная подсветка)
	PlanMsk    *domain.LocalTime `json:"plan_msk"`  // плановое прибытие
	RaschMsk   *domain.LocalTime `json:"rasch_msk"` // расчётный ход (Stage 3)
	ProgMsk    *domain.LocalTime `json:"prog_msk"`  // прогноз прибытия на порт (Stage 4)
	ProgJd     *domain.LocalTime `json:"prog_jd"`   // прогноз в ЖД-формате
	Mistake    *float64          `json:"mistake"`   // необъяснённый простой, дни
}

// ForecastBoard — сводка прогнозов по поездам из RAM-снимка (ActualCache) для экрана.
type ForecastBoard struct {
	actual *ActualCache
}

func NewForecastBoard(actual *ActualCache) *ForecastBoard {
	return &ForecastBoard{actual: actual}
}

// Trains агрегирует активные (не прибывшие) поезда по IdDisl и возвращает только те,
// у кого есть прогноз или план (нитка), отсортированные по терминалу и прогнозу.
func (b *ForecastBoard) Trains() []ForecastTrain {
	byTrain := map[string]*ForecastTrain{}
	var order []string
	for _, r := range b.actual.All() {
		if r.Status != nil && *r.Status == 10 {
			continue // прибыл — не в прогнозе
		}
		if r.IdDisl == "" || r.Naznach == "" {
			continue
		}
		t, ok := byTrain[r.IdDisl]
		if !ok {
			t = &ForecastTrain{
				IdDisl: r.IdDisl, IndexPp: r.IndexPp, Naznach: r.Naznach,
				StanNazn: r.StanNazn, CargoGroup: r.CargoGroup, CargoS: r.CargoS,
			}
			byTrain[r.IdDisl] = t
			order = append(order, r.IdDisl)
		}
		t.VagonCount++
		if r.Ves != nil {
			t.Ves += *r.Ves
		}
		if t.PlanMsk == nil && r.PlanMsk != nil {
			t.PlanMsk = r.PlanMsk
			t.HasPlan = true
		}
		if t.RaschMsk == nil && r.RaschMsk != nil {
			t.RaschMsk = r.RaschMsk
		}
		if t.ProgMsk == nil && r.ProgMsk != nil {
			t.ProgMsk = r.ProgMsk
		}
		if t.ProgJd == nil && r.ProgJd != nil {
			t.ProgJd = r.ProgJd
		}
		if t.Mistake == nil && r.Mistake != nil {
			t.Mistake = r.Mistake
		}
	}

	out := make([]ForecastTrain, 0, len(order))
	for _, id := range order {
		t := byTrain[id]
		if t.ProgMsk == nil && t.PlanMsk == nil {
			continue // ни прогноза, ни нитки — на экран прогнозов не выводим
		}
		out = append(out, *t)
	}

	// Терминал, затем прогноз (пустые — в конец), затем расчёт.
	lt := func(p *domain.LocalTime) (time.Time, bool) {
		if p == nil {
			return time.Time{}, false
		}
		return time.Time(*p), true
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Naznach != out[j].Naznach {
			return out[i].Naznach < out[j].Naznach
		}
		ti, oki := lt(out[i].ProgMsk)
		tj, okj := lt(out[j].ProgMsk)
		if oki != okj {
			return oki // с прогнозом — выше
		}
		return ti.Before(tj)
	})
	return out
}
