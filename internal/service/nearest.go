package service

import (
	"context"
	"sort"

	"github.com/Gtport/DPmodule/internal/domain"
)

// NearestService — блок «Ближайшие поезда» домашней страницы (перенос gtport
// Nearest/TrainsMini «Подход»). Агрегация подходящих поездов из RAM-снимка:
// не на станции назначения (статусы 9/10/12 исключены — они «Прибывшие»/
// кандидаты), фильтр по терминалам станции (naznach), группа = поезд (IdDisl).
//
// Отличие от gtport (решение владельца): показываются все поезда С ПРОГНОЗОМ
// Stage 4 (не только плановые); у плановых прогноз равен плану, поэтому
// сортировка по прогнозу сохраняет порядок прогнозной очереди («нитки в порядке
// плана, бесплановые — за планом»). Поезда без прогноза не показываются.
// Плановые помечены has_plan (зелёная метка в UI). Собственник вагона — наше
// поле owner (в gtport колонка была пустой).
type NearestService struct {
	actual *ActualCache
	dir    *DirectoryCache
}

func NewNearestService(actual *ActualCache, dir *DirectoryCache) *NearestService {
	return &NearestService{actual: actual, dir: dir}
}

// NearestVagonDTO — вагон подгруппы (натурный лист / Excel / СМС).
type NearestVagonDTO struct {
	ID       string   `json:"id"`
	Vagon    string   `json:"vagon"`
	NppVag   *int     `json:"npp_vag"`
	Invoice  string   `json:"invoice"`
	CargoS   string   `json:"cargo_s"`
	Ves      *float64 `json:"ves"`
	Owner    string   `json:"owner"` // собственник/оператор (наше поле, PR #100)
	Naznach  string   `json:"naznach"`
	GruzpolS string   `json:"gruzpol_s"`
	Sms1     string   `json:"sms_1,omitempty"`
}

// NearestSubgroupDTO — подгруппа поезда (одно назначение/получатель).
type NearestSubgroupDTO struct {
	Key         string            `json:"key"`
	IndexMain   string            `json:"index_main"`
	StationNach string            `json:"station_nach"`
	Naznach     string            `json:"naznach"`
	GruzpolS    string            `json:"gruzpol_s"`
	Sms1        string            `json:"sms_1,omitempty"`
	VagonCount  int               `json:"vagon_count"`
	Display     string            `json:"display"`
	Vagons      []NearestVagonDTO `json:"vagons"`
}

// NearestTrainDTO — подходящий поезд: лучшее время прибытия и состав.
type NearestTrainDTO struct {
	Key         string               `json:"key"` // IdDisl
	Index       string               `json:"index"`
	StanNazn    string               `json:"stan_nazn"`
	StationOper string               `json:"station_oper"` // текущая станция операции
	DorogaOper  string               `json:"doroga_oper"`
	Rasst       *int                 `json:"rasst"`     // остаток до станции назначения, км
	TimeJd      *domain.LocalTime    `json:"time_jd"`   // время для показа (ЖД): план либо прогноз
	TimeMsk     *domain.LocalTime    `json:"time_msk"`  // время сортировки (МСК)
	HasPlan     bool                 `json:"has_plan"`  // время из нитки плана (зелёная метка)
	Broshen     bool                 `json:"broshen"`   // в составе есть брошенные (статус 5)
	VagonCount  int                  `json:"vagon_count"`
	Ves         float64              `json:"ves"`
	SubGroups   []NearestSubgroupDTO `json:"sub_groups"`
}

// Trains — ближайшие поезда по терминалам naznach (пусто — все), не больше
// limit (≤0 — 50), отсортированные по лучшему времени прибытия (без времени —
// в конец, по остатку км).
func (s *NearestService) Trains(_ context.Context, naznach []string, limit int) []NearestTrainDTO {
	if limit <= 0 {
		limit = 50
	}
	nz := map[string]struct{}{}
	for _, n := range naznach {
		nz[n] = struct{}{}
	}

	type subKey struct{ im, nzn, gp, sms string }
	var order []string
	trains := map[string]*NearestTrainDTO{}
	subs := map[string]map[subKey]*NearestSubgroupDTO{}
	subOrder := map[string][]subKey{}

	for _, r := range s.actual.All() {
		st := 0
		if r.Status != nil {
			st = *r.Status
		}
		if st == 9 || st >= 10 {
			continue // уже на станции назначения — это «Прибывшие»/кандидаты
		}
		if r.IdDisl == "" || r.Naznach == "" {
			continue
		}
		if len(nz) > 0 {
			if _, ok := nz[r.Naznach]; !ok {
				continue
			}
		}

		t, ok := trains[r.IdDisl]
		if !ok {
			t = &NearestTrainDTO{
				Key: r.IdDisl, Index: r.Index, StanNazn: r.StanNazn,
				StationOper: r.StationOper, DorogaOper: r.DorogaOper, Rasst: r.RasstStanNazn,
			}
			trains[r.IdDisl] = t
			subs[r.IdDisl] = map[subKey]*NearestSubgroupDTO{}
			order = append(order, r.IdDisl)
		}
		t.VagonCount++
		if r.Ves != nil {
			t.Ves += *r.Ves
		}
		if st == 5 {
			t.Broshen = true
		}
		// Время прибытия — ТОЛЬКО прогноз Stage 4 (правило владельца: без
		// прогноза поезд в «Ближайшие» не попадает). У плановых прогноз равен
		// плану, поэтому сортировка по прогнозу сама даёт «нитки в порядке
		// плана, бесплановые — за планом». has_plan — зелёная метка нитки.
		if t.TimeMsk == nil && r.ProgMsk != nil {
			t.TimeMsk, t.TimeJd = r.ProgMsk, firstLT(r.ProgJd, r.ProgMsk)
		}
		if r.PlanMsk != nil {
			t.HasPlan = true
		}

		sk := subKey{r.IndexMain, r.Naznach, r.GruzpolS, r.Sms1}
		sg, ok := subs[r.IdDisl][sk]
		if !ok {
			sg = &NearestSubgroupDTO{
				Key:       r.IndexMain + "|" + r.Naznach + "|" + r.GruzpolS + "|" + r.Sms1,
				IndexMain: r.IndexMain, StationNach: r.StationNach,
				Naznach: r.Naznach, GruzpolS: r.GruzpolS, Sms1: r.Sms1,
			}
			subs[r.IdDisl][sk] = sg
			subOrder[r.IdDisl] = append(subOrder[r.IdDisl], sk)
		}
		sg.VagonCount++
		sg.Vagons = append(sg.Vagons, NearestVagonDTO{
			ID: r.ID, Vagon: r.Vagon, NppVag: r.NppVag, Invoice: r.Invoice,
			CargoS: r.CargoS, Ves: r.Ves, Owner: r.Owner,
			Naznach: r.Naznach, GruzpolS: r.GruzpolS, Sms1: r.Sms1,
		})
	}

	out := make([]NearestTrainDTO, 0, len(order))
	for _, id := range order {
		t := trains[id]
		if t.TimeMsk == nil {
			continue // без прогноза Stage 4 — не «ближайший» (правило владельца)
		}
		for _, sk := range subOrder[id] {
			sg := subs[id][sk]
			sg.Display = arrivalDisplay(&ArrivalSubgroupDTO{
				IndexMain: sg.IndexMain, StationNach: sg.StationNach,
				Naznach: sg.Naznach, GruzpolS: sg.GruzpolS, VagonCount: sg.VagonCount,
			})
			sortNearestVagons(sg.Vagons)
			t.SubGroups = append(t.SubGroups, *sg)
		}
		sort.SliceStable(t.SubGroups, func(i, j int) bool { return t.SubGroups[i].Key < t.SubGroups[j].Key })
		out = append(out, *t)
	}

	// Ближайшие сверху — по прогнозу (у плановых прогноз = план, порядок
	// прогнозной очереди сохраняется сам собой).
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].TimeMsk.Time().Before(out[j].TimeMsk.Time())
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Terminals — реестр целей (для колонок модалки; тот же формат, что arrivals).
func (s *NearestService) Terminals() []TargetDTO {
	return terminalTargets(s.dir)
}

// firstLT — a, если непустое, иначе b (ЖД-времена не всегда заполнены).
func firstLT(a, b *domain.LocalTime) *domain.LocalTime {
	if a != nil && !a.IsZero() {
		return a
	}
	return b
}

// sortNearestVagons — по npp_vag (натурный лист), без номера — в конец по номеру вагона.
func sortNearestVagons(v []NearestVagonDTO) {
	sort.SliceStable(v, func(i, j int) bool {
		a, b := v[i].NppVag, v[j].NppVag
		switch {
		case a == nil && b == nil:
			return v[i].Vagon < v[j].Vagon
		case a == nil:
			return false
		case b == nil:
			return true
		default:
			return *a < *b
		}
	})
}
