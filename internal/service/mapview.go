package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"unicode/utf8"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// MapService — экран «Карта»: группы поездов из RAM-снимка с координатами
// станции текущей операции (Stage 1 проставил их из справочника stations).
//
// Перенос концепции карты gtport с отходами (решения владельца 06.08.2026):
//   - маркер = группа «поезд на станции», ключ ТОТ ЖЕ, что у экрана «Поезда»
//     (trainKey) — а не отдельная группировка по координатам, как в gtport;
//   - порога «min 20 вагонов» нет: отцепки часто мелкие и должны быть видны,
//     плотность маркеров решает кластеризация на клиенте;
//   - вагоны в основной ответ не входят — отдельная ручка по ключу группы;
//   - пометка диспетчера (info_1 текст + info_2 цвет) ставится на группу
//     целиком через канонический MutateSnapshot.
type MapService struct {
	actual *ActualCache
	dir    *DirectoryCache
	proc   *LKProcessor // нужен только пометкам; nil → ApplyMark отвечает ErrNotReady
}

func NewMapService(actual *ActualCache, dir *DirectoryCache, proc *LKProcessor) *MapService {
	return &MapService{actual: actual, dir: dir, proc: proc}
}

// MapGroupDTO — маркер карты: агрегат группы «поезд на станции».
type MapGroupDTO struct {
	Key         string            `json:"key"` // trainKey — им же ходят ручки вагонов и пометки
	Index       string            `json:"index"`
	StationOper string            `json:"station_oper"`
	DorogaOper  string            `json:"doroga_oper"`
	StanNazn    string            `json:"stan_nazn"`
	Lat         *float64          `json:"lat"` // nil → группа уходит в no_coords
	Lon         *float64          `json:"lon"`
	VagonCount  int               `json:"vagon_count"`
	Status      *int              `json:"status"`  // большинство по вагонам (как «Поезда»)
	Broshen     bool              `json:"broshen"` // есть вагон-5
	Arrived     bool              `json:"arrived"` // все вагоны на станции назначения (9/10/12)
	TimeOp      *domain.LocalTime `json:"time_op"`
	OperS       string            `json:"oper_s"`
	PlanJd      *domain.LocalTime `json:"plan_jd"`
	ProgJd      *domain.LocalTime `json:"prog_jd"` // непустой = «ходовой», пустой = «отцепка»
	// Обе шкалы времён + расчёт (решение владельца 07.08.2026): попап карты
	// показывает план ИЛИ прогноз ИЛИ «ход+расчёт» в шкале профиля (ЖД/МСК),
	// комментарий строится по mistake — как в «Прогнозе прибытия/выгрузки».
	PlanMsk  *domain.LocalTime `json:"plan_msk"`
	ProgMsk  *domain.LocalTime `json:"prog_msk"`
	RaschJd  *domain.LocalTime `json:"rasch_jd"`
	RaschMsk *domain.LocalTime `json:"rasch_msk"`
	ToGo     *float64          `json:"to_go"`   // расчётное время хода до порта, часы
	Mistake  *float64          `json:"mistake"` // «необъяснённый простой», дни (Stage 4)
	Rasst    *int              `json:"rasst"`   // остаток до станции назначения, км
	Naznach     string            `json:"naznach"`      // терминал-большинство (фильтры/попап)
	NaznachList []string          `json:"naznach_list"` // все терминалы группы → фильтр чипами
	Color       string            `json:"color"`        // marka.color, если ЕДИНОГЛАСНА по вагонам (правило gtport); пусто → фронт красит по статусу
	MarkText    string            `json:"mark_text"`    // пометка диспетчера: info_1
	MarkColor   string            `json:"mark_color"`   // пометка диспетчера: info_2
	SubGroups   []MapSubgroupDTO  `json:"sub_groups"`   // состав поезда для попапа (без вагонов)
}

// MapSubgroupDTO — подгруппа отправки в попапе маркера («Состав (N)» gtport):
// ключ тот же, что у «Поездов» (index_main×station_nach×gruzpol_s×naznach×
// cargo_group), вагоны НЕ входят — за ними отдельная ручка.
type MapSubgroupDTO struct {
	Key         string  `json:"key"`
	IndexMain   string  `json:"index_main"`
	StationNach string  `json:"station_nach"`
	Gruzotpr    string  `json:"gruzotpr"`
	GruzpolS    string  `json:"gruzpol_s"`
	Naznach     string  `json:"naznach"`
	CargoS      string  `json:"cargo_s"`
	CargoGroup  string  `json:"cargo_group"`
	Client      string  `json:"client,omitempty"`
	VagonCount  int     `json:"vagon_count"`
	Ves         float64 `json:"ves"`
	Color       string  `json:"color"` // marka.color первого вагона подгруппы — цветная полоса в списке состава
}

// MapDataDTO — ответ основной ручки карты.
type MapDataDTO struct {
	Groups   []MapGroupDTO `json:"groups"`    // с координатами — на карту
	NoCoords []MapGroupDTO `json:"no_coords"` // станция без координат в справочнике — боковой список
	Targets  []TargetDTO   `json:"targets"`   // терминалы для чипов (правило унификации)
	Total    int           `json:"total"`     // групп всего
	Vagons   int           `json:"vagons"`    // вагонов всего в снимке
}

// Data — все группы снимка. Режим все/ходовые/отцепки и фильтр терминалов —
// на клиенте (ответ лёгкий: сотни групп без вагонов).
func (s *MapService) Data(_ context.Context) MapDataDTO {
	records := s.actual.All()
	sortNaturalList(records)

	type subKey struct{ im, sn, gp, nz, cg string }
	groups := map[string]*MapGroupDTO{}
	statusVotes := map[string]map[int]int{}
	naznachVotes := map[string]map[string]int{}
	notArrived := map[string]bool{}
	colorMixed := map[string]bool{} // цвет маркера — только при единогласии (gtport determineMarkerColor)
	subs := map[string]map[subKey]*MapSubgroupDTO{}
	subOrder := map[string][]subKey{}
	var order []string

	for i := range records {
		r := &records[i]
		index, key := trainKey(r)

		g, ok := groups[key]
		if !ok {
			g = &MapGroupDTO{
				Key: key, Index: index, StationOper: r.StationOper,
				DorogaOper: r.DorogaOper, StanNazn: r.StanNazn, Rasst: r.RasstStanNazn,
				NaznachList: []string{},
			}
			groups[key] = g
			statusVotes[key] = map[int]int{}
			naznachVotes[key] = map[string]int{}
			subs[key] = map[subKey]*MapSubgroupDTO{}
			order = append(order, key)
		}
		g.VagonCount++

		if g.Lat == nil {
			if lat, lon, ok := parseCoords(r.Latitude, r.Longitude); ok {
				g.Lat, g.Lon = &lat, &lon
			}
		}

		st := -1
		if r.Status != nil {
			st = *r.Status
			statusVotes[key][st]++
		}
		if st == 5 {
			g.Broshen = true
		}
		if st < 9 { // 9/10/12 — на станции назначения; всё прочее рушит arrived
			notArrived[key] = true
		}
		if r.Naznach != "" {
			naznachVotes[key][r.Naznach]++
		}
		// Цвет marka: у первого вагона запоминаем, дальше сверяем — разошёлся
		// хоть один (или пустой), группа остаётся без цвета клиента.
		if g.VagonCount == 1 {
			g.Color = r.Color
		} else if g.Color != r.Color {
			colorMixed[key] = true
		}
		// Поля группы — первое непустое по порядку натурного листа (как «Поезда»).
		if g.TimeOp == nil {
			g.TimeOp = r.TimeOp
		}
		if g.OperS == "" {
			g.OperS = r.OperS
		}
		if g.PlanJd == nil {
			g.PlanJd = firstLT(r.PlanJd, r.PlanMsk)
		}
		if g.ProgJd == nil {
			g.ProgJd = firstLT(r.ProgJd, r.ProgMsk)
		}
		if g.PlanMsk == nil {
			g.PlanMsk = r.PlanMsk
		}
		if g.ProgMsk == nil {
			g.ProgMsk = r.ProgMsk
		}
		if g.RaschJd == nil {
			g.RaschJd = r.RaschJd
		}
		if g.RaschMsk == nil {
			g.RaschMsk = r.RaschMsk
		}
		if g.ToGo == nil {
			g.ToGo = r.ToGo
		}
		if g.Mistake == nil {
			g.Mistake = r.Mistake
		}
		// Пометка — парой с первого помеченного вагона (текст и цвет ставятся вместе).
		if g.MarkText == "" && g.MarkColor == "" && (r.Info1 != "" || r.Info2 != "") {
			g.MarkText, g.MarkColor = r.Info1, r.Info2
		}

		// Состав поезда — подгруппы тем же ключом, что «Поезда» (без вагонов).
		sk := subKey{r.IndexMain, r.StationNach, r.GruzpolS, r.Naznach, r.CargoGroup}
		sg, ok := subs[key][sk]
		if !ok {
			sg = &MapSubgroupDTO{
				Key:       r.IndexMain + "|" + r.StationNach + "|" + r.GruzpolS + "|" + r.Naznach + "|" + r.CargoGroup,
				IndexMain: r.IndexMain, StationNach: r.StationNach,
				Gruzotpr: r.Gruzotpr, GruzpolS: r.GruzpolS, Naznach: r.Naznach,
				CargoS: r.CargoS, CargoGroup: r.CargoGroup, Client: r.Client,
				Color: r.Color,
			}
			subs[key][sk] = sg
			subOrder[key] = append(subOrder[key], sk)
		}
		sg.VagonCount++
		if r.Ves != nil {
			sg.Ves += *r.Ves
		}
	}

	var out, noCoords []MapGroupDTO
	for _, key := range order {
		g := groups[key]
		// Статус группы — большинство вагонов; при равенстве — меньший код.
		best, bestCnt := -1, 0
		for st, cnt := range statusVotes[key] {
			if cnt > bestCnt || (cnt == bestCnt && best != -1 && st < best) {
				best, bestCnt = st, cnt
			}
		}
		if bestCnt > 0 {
			st := best
			g.Status = &st
		}
		g.Arrived = g.VagonCount > 0 && !notArrived[key]

		// Терминал-большинство (цвет маркера) и полный список (фильтр чипами).
		bestNz, bestNzCnt := "", 0
		for nz, cnt := range naznachVotes[key] {
			g.NaznachList = append(g.NaznachList, nz)
			if cnt > bestNzCnt || (cnt == bestNzCnt && nz < bestNz) {
				bestNz, bestNzCnt = nz, cnt
			}
		}
		sort.Strings(g.NaznachList)
		g.Naznach = bestNz
		if colorMixed[key] {
			g.Color = ""
		}
		for _, sk := range subOrder[key] {
			g.SubGroups = append(g.SubGroups, *subs[key][sk])
		}
		sort.SliceStable(g.SubGroups, func(i, j int) bool {
			return g.SubGroups[i].Key < g.SubGroups[j].Key
		})

		if g.Lat != nil {
			out = append(out, *g)
		} else {
			noCoords = append(noCoords, *g)
		}
	}

	// Крупные группы вперёд — и стабильный порядок для тестов и автообновления.
	byCountThenKey := func(list []MapGroupDTO) func(i, j int) bool {
		return func(i, j int) bool {
			if list[i].VagonCount != list[j].VagonCount {
				return list[i].VagonCount > list[j].VagonCount
			}
			return list[i].Key < list[j].Key
		}
	}
	sort.SliceStable(out, byCountThenKey(out))
	sort.SliceStable(noCoords, byCountThenKey(noCoords))

	targets := []TargetDTO{}
	if s.dir != nil {
		targets = terminalTargets(s.dir)
	}
	return MapDataDTO{
		Groups: out, NoCoords: noCoords, Targets: targets,
		Total: len(out) + len(noCoords), Vagons: len(records),
	}
}

// MapWagonDTO — вагон группы (drill-down по требованию).
type MapWagonDTO struct {
	ID          string            `json:"id"` // id рейса — для «Истории движения вагона» по ПКМ
	Vagon       string            `json:"vagon"`
	NppVag      *int              `json:"npp_vag"`
	Invoice     string            `json:"invoice"`
	Freight     string            `json:"freight"` // freight_exact_name, пусто — cargo_s
	Ves         *float64          `json:"ves"`
	Owner       string            `json:"owner"`
	Status      *int              `json:"status"`
	GruzpolS    string            `json:"gruzpol_s"`
	Naznach     string            `json:"naznach"`
	StationNach string            `json:"station_nach"` // станция отправления
	Gruzotpr    string            `json:"gruzotpr"`     // отправитель
	// Две РАЗНЫЕ даты источника, и путать их нельзя (разбор случая 12.08.2026,
	// вагон 73315582: 08.08 17:13 против 09.08 05:20). DateNach — «начало
	// рейса» ЛК, ею подписаны погрузочные колонки прочих экранов и она же в
	// ключе рейса; DateOtpr — фактическое отправление по АСОУП.
	DateNach   *domain.LocalTime `json:"date_nach"`   // дата погрузки (начало рейса)
	DateOtpr   *domain.LocalTime `json:"date_otpr"`   // дата отправления (АСОУП)
	DateDostav *domain.LocalTime `json:"date_dostav"` // нормативный срок доставки
}

// MapWagonsDTO — ответ ручки вагонов группы.
type MapWagonsDTO struct {
	Key    string        `json:"key"`
	Wagons []MapWagonDTO `json:"wagons"`
}

// Wagons — вагоны одной группы по её ключу (в основной ответ не входят —
// клиент просит по требованию из попапа маркера).
func (s *MapService) Wagons(key string) MapWagonsDTO {
	records := s.actual.All()
	sortNaturalList(records)

	out := MapWagonsDTO{Key: key, Wagons: []MapWagonDTO{}}
	for i := range records {
		r := &records[i]
		if _, k := trainKey(r); k != key {
			continue
		}
		freight := r.FreightExactName
		if freight == "" {
			freight = r.CargoS
		}
		out.Wagons = append(out.Wagons, MapWagonDTO{
			ID: r.ID, Vagon: r.Vagon, NppVag: r.NppVag, Invoice: r.Invoice,
			Freight: freight, Ves: r.Ves, Owner: r.Owner, Status: r.Status,
			GruzpolS: r.GruzpolS, Naznach: r.Naznach,
			StationNach: r.StationNach, Gruzotpr: r.Gruzotpr,
			DateNach: r.DateNach, DateOtpr: r.DateOtpr, DateDostav: r.DateDostav,
		})
	}
	return out
}

// ── Пометка диспетчера ───────────────────────────────────────────────────────

// ErrBadMap — ошибка валидации запроса пометки (→ 400).
var ErrBadMap = fmt.Errorf("некорректный запрос пометки")

// markColorRe — цвет пометки строго #rrggbb: значение уезжает в info_2 снимка
// и обратно в разметку маркера, мусор здесь недопустим.
var markColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// maxMarkTextLen — потолок текста пометки (короткая заметка, не журнал).
const maxMarkTextLen = 80

// MapMarkRequest — пометка выбранных групп: текст (info_1) + цвет (info_2).
// Оба пустые = снять пометку.
type MapMarkRequest struct {
	Keys  []string `json:"keys"`
	Text  string   `json:"text"`
	Color string   `json:"color"`
}

// ApplyMark — пометка всем вагонам выбранных групп через канонический
// MutateSnapshot (мьютекс → правка копии → Stage 3–4 → подмена → журнал
// map_mark). Пометка переживает пересборку снимка carry-over'ом и снимается
// только этим же запросом с пустыми text и color.
func (s *MapService) ApplyMark(ctx context.Context, req MapMarkRequest) (RearrApplyResult, error) {
	if s.proc == nil {
		return RearrApplyResult{}, fmt.Errorf("%w: конвейер дислокации не собран", ErrNotReady)
	}
	if len(req.Keys) == 0 {
		return RearrApplyResult{}, fmt.Errorf("%w: не выбраны группы", ErrBadMap)
	}
	if utf8.RuneCountInString(req.Text) > maxMarkTextLen {
		return RearrApplyResult{}, fmt.Errorf("%w: текст длиннее %d символов", ErrBadMap, maxMarkTextLen)
	}
	if req.Color != "" && !markColorRe.MatchString(req.Color) {
		return RearrApplyResult{}, fmt.Errorf("%w: цвет ожидается в виде #rrggbb", ErrBadMap)
	}

	keys := toSet(req.Keys)
	now := clock.Now()
	n, forecastN, progN, err := s.proc.MutateSnapshot(ctx, "map_mark",
		map[string]any{"keys": len(req.Keys), "text": req.Text, "color": req.Color},
		func(all []domain.Dislocation) int {
			n := 0
			for i := range all {
				r := &all[i]
				_, key := trainKey(r)
				if _, sel := keys[key]; !sel {
					continue
				}
				if r.Info1 == req.Text && r.Info2 == req.Color {
					continue // уже так — не дёргаем пересбор ради ничего
				}
				r.Info1, r.Info2 = req.Text, req.Color
				r.UpdatedAt = now
				n++
			}
			return n
		})
	if err != nil {
		return RearrApplyResult{}, err
	}
	return RearrApplyResult{Updated: n, Selected: len(req.Keys), ForecastComputed: forecastN, ProgComputed: progN}, nil
}

// parseCoords — координаты снимка (строки "%f" из Stage 1) в числа.
// Пустые/нулевые (isBlankCoord) и мусор = координат нет.
func parseCoords(latS, lonS string) (lat, lon float64, ok bool) {
	if isBlankCoord(latS) || isBlankCoord(lonS) {
		return 0, 0, false
	}
	lat, errLat := strconv.ParseFloat(latS, 64)
	lon, errLon := strconv.ParseFloat(lonS, 64)
	if errLat != nil || errLon != nil || lat == 0 || lon == 0 {
		return 0, 0, false
	}
	return lat, lon, true
}
