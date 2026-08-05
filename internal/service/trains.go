package service

import (
	"context"
	"sort"

	"github.com/Gtport/DPmodule/internal/domain"
)

// TrainsService — экран «Поезда в движении» (перенос gtport
// OperatorToolsDislocation + service/dislocation_extended.go): трёхуровневое
// дерево «поезд → подгруппа отправки → вагоны» из RAM-снимка целиком, все
// фильтры и поиски — на клиенте (как в gtport). Только чтение.
//
// Отходы от gtport (решение владельца 31.07.2026):
//   - статус/план/прогноз поезда в gtport брались «от первого вагона» при
//     недетерминированном обходе мапы — здесь статус агрегируется по большинству
//     вагонов, остальные поля — первое непустое по порядку натурного листа;
//   - простой поезда — максимум по вагонам (фактическое поведение gtport,
//     комментарий «суммарный» в его модели был неверен);
//   - фильтр filter_type=УТ-1 из gtport не переносится: тамошний сервис его
//     игнорировал и всегда отдавал всё — здесь это зафиксировано явно;
//   - собственник вагона — owner (в gtport колонку заполнял rod_vag_uch),
//     марка груза — freight_exact_name; терминалы для фильтров — из реестра
//     ports (в gtport — хардкод трёх портов с цветами).
type TrainsService struct {
	actual *ActualCache
	dir    *DirectoryCache
}

func NewTrainsService(actual *ActualCache, dir *DirectoryCache) *TrainsService {
	return &TrainsService{actual: actual, dir: dir}
}

// TrainVagonDTO — вагон подгруппы (нижний уровень дерева и Excel).
type TrainVagonDTO struct {
	ID         string            `json:"id"` // id рейса — для «Истории движения вагона» по ПКМ
	Vagon      string            `json:"vagon"`
	NppVag     *int              `json:"npp_vag"`
	Invoice    string            `json:"invoice"`
	Freight    string            `json:"freight"` // марка груза: freight_exact_name, пусто — cargo_s
	Ves        *float64          `json:"ves"`
	Owner      string            `json:"owner"`   // собственник/оператор (наше поле)
	RodVag     string            `json:"rod_vag"` // код рода вагона (rod_vag_uch)
	Status     *int              `json:"status"`
	DateNach   *domain.LocalTime `json:"date_nach"`   // дата погрузки
	DateDostav *domain.LocalTime `json:"date_dostav"` // нормативный срок доставки
}

// TrainSubgroupDTO — подгруппа отправки: одна станция погрузки/получатель/груз.
type TrainSubgroupDTO struct {
	Key         string          `json:"key"`
	IndexMain   string          `json:"index_main"`
	StationNach string          `json:"station_nach"`
	Gruzotpr    string          `json:"gruzotpr"`
	GruzpolS    string          `json:"gruzpol_s"`
	Naznach     string          `json:"naznach"`
	CargoS      string          `json:"cargo_s"`
	CargoGroup  string          `json:"cargo_group"`
	Client      string          `json:"client,omitempty"`
	Shipments   string          `json:"shipments,omitempty"`
	VagonCount  int             `json:"vagon_count"`
	Ves         float64         `json:"ves"`
	Vagons      []TrainVagonDTO `json:"vagons"`
}

// TrainDTO — строка-поезд: агрегат по вагонам одной группы.
type TrainDTO struct {
	Key         string             `json:"key"` // нормализованный индекс + станция операции
	Index       string             `json:"index"`
	StationOper string             `json:"station_oper"`
	DorogaOper  string             `json:"doroga_oper"`
	StanNazn    string             `json:"stan_nazn"`
	Rasst       *int               `json:"rasst"` // остаток до станции назначения, км
	Status      *int               `json:"status"`
	Broshen     bool               `json:"broshen"` // есть вагон-5
	Arrived     bool               `json:"arrived"` // все вагоны на станции назначения (9/10/12)
	HasPlan     bool               `json:"has_plan"`
	TimeOp      *domain.LocalTime  `json:"time_op"`
	OperS       string             `json:"oper_s"`
	PlanJd      *domain.LocalTime  `json:"plan_jd"`
	RaschJd     *domain.LocalTime  `json:"rasch_jd"`
	ProgJd      *domain.LocalTime  `json:"prog_jd"` // непустой = «ходовой», пустой = «отцепка»
	ProstDn     *int               `json:"prost_dn"` // максимум по вагонам
	ProstCh     *int               `json:"prost_ch"`
	VagonCount  int                `json:"vagon_count"`
	Ves         float64            `json:"ves"`
	SubGroups   []TrainSubgroupDTO `json:"sub_groups"`
}

// TrainsDTO — ответ ручки: все поезда снимка + терминалы для чипов фильтров.
type TrainsDTO struct {
	Trains  []TrainDTO  `json:"trains"`
	Targets []TargetDTO `json:"targets"`
	Total   int         `json:"total"`
}

// Trains — весь снимок деревом. Ключ поезда — нормализованный индекс (index_pp,
// если есть, иначе index) + станция операции: отцепленная часть того же индекса
// на другой станции показывается отдельной строкой (правило gtport).
func (s *TrainsService) Trains(_ context.Context) TrainsDTO {
	records := s.actual.All()
	// Детерминированность: обход мапы случаен, а «первое непустое» поле поезда
	// должно быть стабильным между вызовами — упорядочиваем натурным листом.
	sortNaturalList(records)

	type subKey struct{ im, sn, gp, nz, cg string }
	trains := map[string]*TrainDTO{}
	statusVotes := map[string]map[int]int{}
	notArrived := map[string]bool{}
	subs := map[string]map[subKey]*TrainSubgroupDTO{}
	subOrder := map[string][]subKey{}
	var order []string

	for _, r := range records {
		index, key := trainKey(&r)

		t, ok := trains[key]
		if !ok {
			t = &TrainDTO{
				Key: key, Index: index, StationOper: r.StationOper,
				DorogaOper: r.DorogaOper, StanNazn: r.StanNazn, Rasst: r.RasstStanNazn,
			}
			trains[key] = t
			statusVotes[key] = map[int]int{}
			subs[key] = map[subKey]*TrainSubgroupDTO{}
			order = append(order, key)
		}
		t.VagonCount++
		if r.Ves != nil {
			t.Ves += *r.Ves
		}
		st := -1
		if r.Status != nil {
			st = *r.Status
			statusVotes[key][st]++
		}
		if st == 5 {
			t.Broshen = true
		}
		if st < 9 { // 9/10/12 — на станции назначения; всё прочее рушит arrived
			notArrived[key] = true
		}
		// Поля поезда — первое непустое по порядку натурного листа.
		if t.TimeOp == nil {
			t.TimeOp = r.TimeOp
		}
		if t.OperS == "" {
			t.OperS = r.OperS
		}
		if t.PlanJd == nil {
			t.PlanJd = firstLT(r.PlanJd, r.PlanMsk)
		}
		if t.RaschJd == nil {
			t.RaschJd = firstLT(r.RaschJd, r.RaschMsk)
		}
		if t.ProgJd == nil {
			t.ProgJd = firstLT(r.ProgJd, r.ProgMsk)
		}
		if r.PlanMsk != nil || r.PlanJd != nil {
			t.HasPlan = true
		}
		// Простой поезда — максимум по вагонам (в часах), обратно в дн/ч.
		if r.ProstDn != nil || r.ProstCh != nil {
			hours := 0
			if r.ProstDn != nil {
				hours += *r.ProstDn * 24
			}
			if r.ProstCh != nil {
				hours += *r.ProstCh
			}
			cur := 0
			if t.ProstDn != nil {
				cur += *t.ProstDn * 24
			}
			if t.ProstCh != nil {
				cur += *t.ProstCh
			}
			if hours > cur {
				d, h := hours/24, hours%24
				t.ProstDn, t.ProstCh = &d, &h
			}
		}

		sk := subKey{r.IndexMain, r.StationNach, r.GruzpolS, r.Naznach, r.CargoGroup}
		sg, ok := subs[key][sk]
		if !ok {
			sg = &TrainSubgroupDTO{
				Key:       r.IndexMain + "|" + r.StationNach + "|" + r.GruzpolS + "|" + r.Naznach + "|" + r.CargoGroup,
				IndexMain: r.IndexMain, StationNach: r.StationNach,
				Gruzotpr: r.Gruzotpr, GruzpolS: r.GruzpolS, Naznach: r.Naznach,
				CargoS: r.CargoS, CargoGroup: r.CargoGroup,
				Client: r.Client, Shipments: r.Shipments,
			}
			subs[key][sk] = sg
			subOrder[key] = append(subOrder[key], sk)
		}
		sg.VagonCount++
		if r.Ves != nil {
			sg.Ves += *r.Ves
		}
		freight := r.FreightExactName
		if freight == "" {
			freight = r.CargoS
		}
		sg.Vagons = append(sg.Vagons, TrainVagonDTO{
			ID: r.ID, Vagon: r.Vagon, NppVag: r.NppVag, Invoice: r.Invoice,
			Freight: freight, Ves: r.Ves, Owner: r.Owner, RodVag: r.RodVagUch,
			Status: r.Status, DateNach: r.DateNach, DateDostav: r.DateDostav,
		})
	}

	out := make([]TrainDTO, 0, len(order))
	for _, key := range order {
		t := trains[key]
		// Статус поезда — большинство вагонов; при равенстве — меньший код.
		best, bestCnt := -1, 0
		for st, cnt := range statusVotes[key] {
			if cnt > bestCnt || (cnt == bestCnt && best != -1 && st < best) {
				best, bestCnt = st, cnt
			}
		}
		if bestCnt > 0 {
			st := best
			t.Status = &st
		}
		t.Arrived = t.VagonCount > 0 && !notArrived[key]

		for _, sk := range subOrder[key] {
			t.SubGroups = append(t.SubGroups, *subs[key][sk])
		}
		sort.SliceStable(t.SubGroups, func(i, j int) bool {
			return t.SubGroups[i].Key < t.SubGroups[j].Key
		})
		out = append(out, *t)
	}

	// Сортировка gtport: остаток км ↑ (без rasst — в конец), затем индекс, станция.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Rasst, out[j].Rasst
		switch {
		case a == nil && b != nil:
			return false
		case a != nil && b == nil:
			return true
		case a != nil && b != nil && *a != *b:
			return *a < *b
		}
		if out[i].Index != out[j].Index {
			return out[i].Index < out[j].Index
		}
		return out[i].StationOper < out[j].StationOper
	})

	targets := []TargetDTO{}
	if s.dir != nil {
		targets = terminalTargets(s.dir)
	}
	return TrainsDTO{Trains: out, Targets: targets, Total: len(out)}
}

// trainKey — нормализованный индекс поезда (index_pp, если есть, иначе index) и
// ключ группы «индекс|станция операции». ЕДИНЫЙ для «Поездов» и «Карты»: пометка,
// поставленная с карты, находит ровно ту группу, что видна на экране поездов.
func trainKey(r *domain.Dislocation) (index, key string) {
	index = r.Index
	if r.IndexPp != "" {
		index = r.IndexPp
	}
	return index, index + "|" + r.StationOper
}

// sortNaturalList упорядочивает записи натурным листом (npp_vag, затем номер
// вагона): «первое непустое» поле группы становится стабильным между вызовами.
func sortNaturalList(records []domain.Dislocation) {
	sort.SliceStable(records, func(i, j int) bool {
		a, b := records[i].NppVag, records[j].NppVag
		switch {
		case a == nil && b == nil:
			return records[i].Vagon < records[j].Vagon
		case a == nil:
			return false
		case b == nil:
			return true
		case *a != *b:
			return *a < *b
		default:
			return records[i].Vagon < records[j].Vagon
		}
	})
}
