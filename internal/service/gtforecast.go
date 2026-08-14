package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/internal/service/stage4"
	"github.com/Gtport/DPmodule/internal/service/unloadsim"
)

// Вкладка «Прогноз прибытия/выгрузки» страницы «Прогнозы» (перенос страницы
// «Прогноз GT» gtport): очередь поездов по причальной станции + симуляция
// выгрузки терминалов по суткам (диаграмма Ганта).
//
// Отличия от эталона gtport (канонизированы решениями владельца 05.08.2026):
//   - симуляция выгрузки считается ЗДЕСЬ, на сервере (пакет unloadsim с
//     golden-тестами), а не в браузере;
//   - прогноз прибытия НЕ пересчитывается по фиксированным интервалам gtport —
//     берётся готовым из снимка (Stage 4 DPmodule, интервалы от способности
//     причала, миграция 000026); вкладка показывает те же prog_jd, что и
//     остальные экраны;
//   - режим страницы — код причальной станции (АЭ+ГУТ-2 и УТ-1 у нас — две
//     станции из ports.station_code), а не захардкоженные пары портов;
//   - скорости выгрузки — port_cargo_line (план plan_speed, норма pc), а не
//     таблица gt_port_speeds;
//   - «универсальность» груза — naznach_station.univers по паре станций
//     (назначения, отправления), а не по коду sms_1.

// ── DTO ──────────────────────────────────────────────────────────────────────

// GtLineDTO — линия выгрузки терминала (поток симуляции).
type GtLineDTO struct {
	Terminal  string `json:"terminal"`
	CargoKey  string `json:"cargo_key"` // пусто — все грузы терминала одним потоком
	Label     string `json:"label"`
	PlanSpeed int    `json:"plan_speed"` // план, ваг/сут (правится в Админе)
	NormSpeed int    `json:"norm_speed"` // норма = способность линии
}

// GtTerminalDTO — терминал причальной станции.
type GtTerminalDTO struct {
	Name  string      `json:"name"`
	Color string      `json:"color"`
	Lines []GtLineDTO `json:"lines"`
}

// GtStationDTO — режим страницы: причальная станция с её терминалами.
type GtStationDTO struct {
	Code      string          `json:"code"`
	Terminals []GtTerminalDTO `json:"terminals"`
}

// GtContextDTO — справочная часть вкладки (режимы и линии со скоростями).
type GtContextDTO struct {
	Stations []GtStationDTO `json:"stations"`
}

// GtSubGroupDTO — подгруппа поезда (одна станция отправления × назначение × груз).
type GtSubGroupDTO struct {
	Key         string            `json:"key"`
	StationNach string            `json:"station_nach"`
	DateNach    *domain.LocalTime `json:"date_nach"`
	VagonCount  int               `json:"vagon_count"`
	CargoGroup  string            `json:"cargo_group"`
	Naznach     string            `json:"naznach"`
	Color       string            `json:"color"`
	IndexMain   string            `json:"index_main"`
	IsUniversal bool              `json:"is_universal"` // груз можно переместить на другой терминал
}

// GtTrainDTO — поезд очереди (транзит из снимка либо прибывший из истории).
type GtTrainDTO struct {
	Index       string            `json:"index"`
	StationOper string            `json:"station_oper"`
	Status      string            `json:"status"` // номер статуса; прибывший — "history"
	IsArrived   bool              `json:"is_arrived"`
	PlanJd      *domain.LocalTime `json:"plan_jd"`
	PlanMsk     *domain.LocalTime `json:"plan_msk"`
	ProgJd      *domain.LocalTime `json:"prog_jd"`
	ProgMsk     *domain.LocalTime `json:"prog_msk"`
	RaschJd     *domain.LocalTime `json:"rasch_jd"`
	RaschMsk    *domain.LocalTime `json:"rasch_msk"`
	Mistake     *float64          `json:"mistake"`
	ToGo        *float64          `json:"to_go"`
	// DelayHours — эффективная задержка (эталон delay_hours): 72 у брошенных
	// конвейером, значение правки у what-if-бросков/восстановлений, 0 иначе.
	DelayHours float64         `json:"delay_hours"`
	VagonCount int             `json:"vagon_count"`
	SubGroups  []GtSubGroupDTO `json:"sub_groups"`
}

// GtOperationDTO — блок диаграммы Ганта (выгрузка / остаток / простой).
type GtOperationDTO struct {
	TrainIndex    string            `json:"train_index"`
	TrainName     string            `json:"train_name"`
	StationNach   string            `json:"station_nach,omitempty"`
	IndexMain     string            `json:"index_main,omitempty"`
	GruzpolS      string            `json:"gruzpol_s,omitempty"`
	OrigIndex     string            `json:"orig_index,omitempty"` // чей остаток
	StartCalc     domain.LocalTime  `json:"start_calc"`
	EndCalc       domain.LocalTime  `json:"end_calc"`
	StartJd       domain.LocalTime  `json:"start_jd"`
	EndJd         domain.LocalTime  `json:"end_jd"`
	Wagons        int               `json:"wagons"`
	TotalWagons   int               `json:"total_wagons"`
	Color         string            `json:"color"`
	IsRemainder   bool              `json:"is_remainder"`
	IsCarriedOver bool              `json:"is_carried_over"`
	IsPartial     bool              `json:"is_partial"`
	WaitMin       float64           `json:"wait_min"`
	OrigArrivalJd *domain.LocalTime `json:"original_arrival_jd,omitempty"`
}

// GtCarriedDTO — поезд, перенесённый на следующие сутки.
type GtCarriedDTO struct {
	Index  string `json:"index"`
	Wagons int    `json:"wagons"`
}

// GtDayDTO — одни расчётные сутки потока (строка диаграммы).
type GtDayDTO struct {
	Date            string           `json:"date"`
	PlanSpeed       int              `json:"plan_speed"`
	NormSpeed       int              `json:"norm_speed"`
	IncomingTotal   int              `json:"incoming_total"`
	Arrival         int              `json:"arrival"`
	TotalFormation  int              `json:"total_formation"`
	UsefulFormation int              `json:"useful_formation"`
	Unloaded        int              `json:"unloaded"`
	Remaining       int              `json:"remaining"`
	TotalWaitMin    float64          `json:"total_wait_min"`
	CarriedOver     []GtCarriedDTO   `json:"carried_over"`
	Operations      []GtOperationDTO `json:"operations"`
}

// GtFlowDTO — поток выгрузки: одна диаграмма Ганта.
type GtFlowDTO struct {
	Terminal         string    `json:"terminal"`
	CargoKey         string    `json:"cargo_key"`
	Label            string    `json:"label"`
	Color            string    `json:"color"` // цвет терминала (шапка диаграммы)
	InitialRemainder int       `json:"initial_remainder"`
	Days             []GtDayDTO `json:"days"`
}

// GtOverride — what-if правка поезда (эталон gtport TrainEditDialog):
//   - throw:   бросить на DelayDays суток (delay_hours = N×24, план снимается, статус 5);
//   - restore: восстановить с остаточной задержкой DelayHours часов (0 = немедленно);
//   - assign:  поставить на нитку Slot (МСК) — поезд становится плановым;
//   - move:    переместить УНИВЕРСАЛЬНЫЕ подгруппы на терминал MoveTo.
//
// RaschMsk правкой не меняется никогда (физическое прибытие).
type GtOverride struct {
	Index      string            `json:"index"`
	Action     string            `json:"action"` // throw | restore | assign | move
	DelayDays  int               `json:"delay_days"`
	DelayHours float64           `json:"delay_hours"`
	Slot       *domain.LocalTime `json:"slot"`    // МСК нитки (assign)
	MoveTo     string            `json:"move_to"` // терминал (move)
}

// GtFreeSlotDTO — свободная нитка расписания станции на горизонте прогноза.
type GtFreeSlotDTO struct {
	Msk domain.LocalTime `json:"msk"`
	Jd  domain.LocalTime `json:"jd"`
}

// GtSimulateRequest — запрос пересчёта: скорости по дням, тумблер План↔Норма и
// what-if правки поездов. Фронт шлёт ВЕСЬ накопленный список правок сеанса —
// сервер каждый раз считает от базового снимка (stateless).
type GtSimulateRequest struct {
	Station   string `json:"station"`    // код причальной станции (режим)
	StartDate string `json:"start_date"` // YYYY-MM-DD (расчётные ЖД-сутки)
	Days      int    `json:"days"`       // 1..14
	UseNorm   bool   `json:"use_norm"`   // считать по нормам вместо плана
	// SpeedOverrides: "терминал|груз" → дата YYYY-MM-DD → ваг/сут.
	SpeedOverrides map[string]map[string]int `json:"speed_overrides"`
	Overrides      []GtOverride              `json:"overrides"`
}

// GtSimulateDTO — полный ответ пересчёта: очередь поездов + диаграммы.
type GtSimulateDTO struct {
	Trains         []GtTrainDTO    `json:"trains"`
	Flows          []GtFlowDTO     `json:"flows"`
	FreeSlots      []GtFreeSlotDTO `json:"free_slots"`
	MaxTrainWagons int             `json:"max_train_wagons"`
}

// ── Сервис ───────────────────────────────────────────────────────────────────

// GtForecastService — сборка данных вкладки и запуск симуляции.
type GtForecastService struct {
	actual    *ActualCache
	dir       *DirectoryCache
	history   port.HistoryRepository
	cargo     *CargoWorkService
	cfg       *ConfigCache
	snapshots port.GtSnapshotRepository
	plans     port.PlanRepository      // свободные нитки — из текущего плана подвода
	lines     port.CargoWorkRepository // правка скоростей линий из вкладки
}

func NewGtForecastService(actual *ActualCache, dir *DirectoryCache,
	history port.HistoryRepository, cargo *CargoWorkService, cfg *ConfigCache,
	snapshots port.GtSnapshotRepository, plans port.PlanRepository,
	lines port.CargoWorkRepository) *GtForecastService {
	return &GtForecastService{actual: actual, dir: dir, history: history, cargo: cargo,
		cfg: cfg, snapshots: snapshots, plans: plans, lines: lines}
}

// GtSpeedUpdate — правка скоростей линии выгрузки (диалог настроек вкладки).
type GtSpeedUpdate struct {
	Terminal  string `json:"terminal"`
	CargoKey  string `json:"cargo_key"`
	PlanSpeed int    `json:"plan_speed"`
	NormSpeed int    `json:"norm_speed"`
}

// UpdateSpeeds сохраняет скорости линий (план plan_speed, норма pc) —
// перенос настройки gtport gt_port_speeds, права operator+ (мутация).
func (s *GtForecastService) UpdateSpeeds(ctx context.Context, updates []GtSpeedUpdate) error {
	if len(updates) == 0 {
		return fmt.Errorf("пустой список правок")
	}
	for _, u := range updates {
		if u.PlanSpeed <= 0 || u.NormSpeed <= 0 {
			return fmt.Errorf("линия %s|%s: скорости должны быть больше нуля", u.Terminal, u.CargoKey)
		}
	}
	for _, u := range updates {
		if err := s.lines.UpdateLineSpeed(ctx, u.Terminal, u.CargoKey, u.PlanSpeed, u.NormSpeed); err != nil {
			return err
		}
	}
	return nil
}

// Context — режимы вкладки: причальные станции с терминалами и линиями выгрузки.
func (s *GtForecastService) Context(ctx context.Context) (GtContextDTO, error) {
	byStation := map[string][]GtTerminalDTO{}
	var order []string
	for _, term := range s.dir.EnabledTerminals() {
		p, ok := s.dir.PortByNameS(term)
		if !ok || p.StationCode == "" {
			continue
		}
		lns, err := s.cargo.UnloadLines(ctx, term)
		if err != nil {
			return GtContextDTO{}, fmt.Errorf("линии выгрузки %s: %w", term, err)
		}
		t := GtTerminalDTO{Name: term, Color: p.Color}
		for _, ln := range lns {
			t.Lines = append(t.Lines, GtLineDTO{
				Terminal: term, CargoKey: ln.CargoKey, Label: ln.Label,
				PlanSpeed: s.linePlanSpeed(ln, term), NormSpeed: s.cargo.LinePc(ln, term),
			})
		}
		if _, seen := byStation[p.StationCode]; !seen {
			order = append(order, p.StationCode)
		}
		byStation[p.StationCode] = append(byStation[p.StationCode], t)
	}
	dto := GtContextDTO{}
	for _, code := range order {
		dto.Stations = append(dto.Stations, GtStationDTO{Code: code, Terminals: byStation[code]})
	}
	return dto, nil
}

// linePlanSpeed — плановая скорость линии; не задана → норма (pc).
func (s *GtForecastService) linePlanSpeed(ln domain.PortCargoLine, terminal string) int {
	if ln.PlanSpeed != nil && *ln.PlanSpeed > 0 {
		return *ln.PlanSpeed
	}
	return s.cargo.LinePc(ln, terminal)
}

// Simulate — сборка очереди поездов режима и прогон симуляции выгрузки.
func (s *GtForecastService) Simulate(ctx context.Context, req GtSimulateRequest) (GtSimulateDTO, error) {
	if req.Days < 1 || req.Days > 14 {
		return GtSimulateDTO{}, fmt.Errorf("days: ожидается 1..14, получено %d", req.Days)
	}
	start, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		return GtSimulateDTO{}, fmt.Errorf("start_date: %w", err)
	}

	// Терминалы режима (причальной станции).
	var terminals []string
	termColor := map[string]string{}
	for _, term := range s.dir.EnabledTerminals() {
		if p, ok := s.dir.PortByNameS(term); ok && p.StationCode == req.Station {
			terminals = append(terminals, term)
			termColor[term] = p.Color
		}
	}
	if len(terminals) == 0 {
		return GtSimulateDTO{}, fmt.Errorf("станция %q: терминалы не найдены", req.Station)
	}
	known := map[string]bool{}
	for _, t := range terminals {
		known[t] = true
	}

	// Признак универсальности: пара (станция назначения, станция отправления).
	univers, err := s.universSet(ctx)
	if err != nil {
		return GtSimulateDTO{}, err
	}

	// Очередь: транзит из снимка + прибывшие за стартовые сутки из истории.
	trains := gtTransitTrains(s.actual.All(), known, univers)
	startLT := domain.LocalTime(start)
	rows, err := s.history.ArrivedRows(ctx, startLT, startLT, terminals)
	if err != nil {
		return GtSimulateDTO{}, fmt.Errorf("прибывшие за сутки: %w", err)
	}
	trains = append(trains, gtArrivedTrains(rows, known)...)

	// Фиксированное стартовое время what-if — от БАЗОВЫХ поездов, до правок
	// (эталон fixedStartTime: правка, снявшая план с последнего планового,
	// не должна двигать прогнозы остальных).
	var maxPlan time.Time
	for _, t := range trains {
		if !t.IsArrived && t.PlanMsk != nil && time.Time(*t.PlanMsk).After(maxPlan) {
			maxPlan = time.Time(*t.PlanMsk)
		}
	}
	fixedStart := stage4.NextEighteen(maxPlan, clock.Now().Time())

	// What-if правки → пересчёт прибытия алгоритмом DPmodule stage4.
	if len(req.Overrides) > 0 {
		delays, err := applyGtOverrides(trains, req.Overrides)
		if err != nil {
			return GtSimulateDTO{}, err
		}
		s.recomputeArrival(trains, req.Station, fixedStart, delays)
	}

	sort.SliceStable(trains, func(i, j int) bool {
		return gtCalcTime(trains[i]).Before(gtCalcTime(trains[j]))
	})

	free, err := s.freeSlots(ctx, req.Station, terminals, start, req.Days)
	if err != nil {
		return GtSimulateDTO{}, err
	}
	dto := GtSimulateDTO{
		Trains:         trains,
		FreeSlots:      free,
		MaxTrainWagons: s.maxTrainWagons(req.Station),
	}

	// Потоки выгрузки: линия справочника = диаграмма Ганта.
	yesterday := start.AddDate(0, 0, -1)
	for _, term := range terminals {
		lns, err := s.cargo.UnloadLines(ctx, term)
		if err != nil {
			return GtSimulateDTO{}, fmt.Errorf("линии выгрузки %s: %w", term, err)
		}
		if len(lns) == 0 {
			continue
		}
		// «Остаток на 18:00» перед стартовыми сутками — вчерашний лист
		// грузовой работы (несохранённый пересобирается на лету).
		day, err := s.cargo.Day(ctx, yesterday, term)
		if err != nil {
			return GtSimulateDTO{}, fmt.Errorf("остаток за %s (%s): %w", yesterday.Format("2006-01-02"), term, err)
		}
		ost := map[string]int{}
		for _, l := range day.Lines {
			ost[l.CargoKey] = l.Ost
		}
		for _, ln := range lns {
			plan := s.linePlanSpeed(ln, term)
			norm := s.cargo.LinePc(ln, term)
			if req.UseNorm {
				plan = norm
			}
			flow := unloadsim.Flow{
				Port: term, Cargo: gtFlowCargo(ln.CargoKey),
				Trains:           gtFlowTrains(trains, term, ln.CargoKey),
				InitialRemainder: ost[ln.CargoKey],
				PlanSpeed:        plan,
				PlanOverrides:    req.SpeedOverrides[term+"|"+ln.CargoKey],
				NormSpeed:        norm,
				MaxTrainWagons:   dto.MaxTrainWagons,
			}
			days := unloadsim.SimulateFlow(flow, start, req.Days)
			dto.Flows = append(dto.Flows, GtFlowDTO{
				Terminal: term, CargoKey: ln.CargoKey, Label: ln.Label,
				Color:            termColor[term],
				InitialRemainder: flow.InitialRemainder,
				Days:             gtDaysDTO(days),
			})
		}
	}
	return dto, nil
}

// universSet — пары (станция назначения|станция отправления) с univers=true.
func (s *GtForecastService) universSet(ctx context.Context) (map[string]bool, error) {
	rows, err := s.dir.NaznachStationRows(ctx)
	if err != nil {
		return nil, fmt.Errorf("справочник назначений: %w", err)
	}
	set := map[string]bool{}
	for _, r := range rows {
		if r.Univers {
			set[r.DestStation+"|"+r.OriginStation] = true
		}
	}
	return set, nil
}

// maxTrainWagons — технологический максимум состава станции (plan_profile);
// профиля нет → эталон gtport (63).
func (s *GtForecastService) maxTrainWagons(station string) int {
	for _, p := range s.cfg.PlanProfiles() {
		if p.StationCode == station && p.MaxTrainLength > 0 {
			return p.MaxTrainLength
		}
	}
	return 63
}

// gtFlowCargo — имя потока для цветового фолбэка симуляции.
func gtFlowCargo(cargoKey string) string {
	if cargoKey == "" {
		return "ОБЩИЙ"
	}
	return cargoKey
}

// gtCalcTime — прибытие поезда в расчётной шкале (для сортировки очереди).
func gtCalcTime(t GtTrainDTO) time.Time {
	if t.ProgJd == nil {
		return time.Time{}
	}
	return unloadsim.RailwayToCalc(time.Time(*t.ProgJd))
}

// gtTransitTrains — группировка снимка в поезда очереди GT (перенос gtport
// groupByExtendedCollectiveTrain + has_prog_jd): вагоны в подходе (статус < 9)
// с прогнозом и назначением на терминал режима. Ключ поезда — как в эталоне:
// индекс | станция операции | остаток пути | статус. Поезда без индекса
// нумеруются «Б/И N» (уникальность для выделения и симуляции).
func gtTransitTrains(rows []domain.Dislocation, known map[string]bool, univers map[string]bool) []GtTrainDTO {
	type agg struct {
		t    GtTrainDTO
		subs map[string]*GtSubGroupDTO
		ord  []string
	}
	trains := map[string]*agg{}
	var order []string

	for i := range rows {
		r := &rows[i]
		if r.Status != nil && *r.Status >= 9 {
			continue
		}
		if r.ProgJd == nil || !known[r.Naznach] {
			continue
		}
		index := r.Index
		if r.IndexPp != "" {
			index = r.IndexPp
		}
		status := ""
		if r.Status != nil {
			status = strconv.Itoa(*r.Status)
		}
		rasst := -1
		if r.RasstStanNazn != nil {
			rasst = *r.RasstStanNazn
		}
		key := fmt.Sprintf("%s|%s|%d|%s", index, r.StationOper, rasst, status)
		t, ok := trains[key]
		if !ok {
			delay := 0.0
			if status == "5" {
				delay = 72 // серверный штраф бросания (эталон initDelayHours)
			}
			t = &agg{
				t: GtTrainDTO{
					Index: index, StationOper: r.StationOper, Status: status,
					PlanJd: r.PlanJd, PlanMsk: r.PlanMsk, ProgJd: r.ProgJd, ProgMsk: r.ProgMsk,
					RaschJd: r.RaschJd, RaschMsk: r.RaschMsk, Mistake: r.Mistake, ToGo: r.ToGo,
					DelayHours: delay,
				},
				subs: map[string]*GtSubGroupDTO{},
			}
			trains[key] = t
			order = append(order, key)
		}

		subKey := r.IndexMain + "|" + r.StationNach + "|" + r.GruzpolS + "|" + r.Naznach + "|" + r.CargoGroup
		sg, ok := t.subs[subKey]
		if !ok {
			sg = &GtSubGroupDTO{
				Key: subKey, StationNach: r.StationNach, CargoGroup: r.CargoGroup,
				Naznach: r.Naznach, Color: r.Color, IndexMain: r.IndexMain,
				IsUniversal: univers[r.StanNazn+"|"+r.StationNach],
			}
			t.subs[subKey] = sg
			t.ord = append(t.ord, subKey)
		}
		sg.VagonCount++
		if r.DateNach != nil && (sg.DateNach == nil ||
			time.Time(*r.DateNach).After(time.Time(*sg.DateNach))) {
			sg.DateNach = r.DateNach
		}
		if sg.Color == "" {
			sg.Color = r.Color
		}
	}

	out := make([]GtTrainDTO, 0, len(order))
	bi := 0
	for _, key := range order {
		t := trains[key]
		for _, sk := range t.ord {
			t.t.SubGroups = append(t.t.SubGroups, *t.subs[sk])
			t.t.VagonCount += t.subs[sk].VagonCount
		}
		if t.t.Index == "" || t.t.Index == "Б/И" {
			bi++
			t.t.Index = fmt.Sprintf("Б/И %d", bi)
		}
		out = append(out, t.t)
	}
	return out
}

// gtArrivedTrains — прибывшие за сутки поезда из вех истории: группа по
// index_pp + ЖД-СУТКИ прибытия (date_prib_d), «прибытие» = самая ранняя веха.
// В gtport группа шла по точному date_prib (оператор штамповал поезд одним
// временем); в DPmodule вехи из потока АСУ проставляются вагонам порциями
// (21:00/21:30/22:20 у одного поезда) — точное время развалило бы поезд на
// части (боевой случай 05.08.2026, поезд 128).
func gtArrivedTrains(rows []domain.VagonHistory, known map[string]bool) []GtTrainDTO {
	type agg struct {
		t    GtTrainDTO
		subs map[string]*GtSubGroupDTO
		ord  []string
	}
	groups := map[string]*agg{}
	var order []string

	for _, r := range rows {
		if !known[r.Naznach] || r.DatePrib == nil {
			continue
		}
		day := time.Time(*r.DatePrib).Format("2006-01-02")
		if r.DatePribD != nil {
			day = time.Time(*r.DatePribD).Format("2006-01-02")
		}
		key := r.IndexPp + "|" + day
		g, ok := groups[key]
		if !ok {
			g = &agg{
				t: GtTrainDTO{
					Index: r.IndexPp, Status: "history", IsArrived: true,
					ProgJd: r.DatePrib, StationOper: r.StationNach,
				},
				subs: map[string]*GtSubGroupDTO{},
			}
			groups[key] = g
			order = append(order, key)
		}
		if g.t.ProgJd == nil || time.Time(*r.DatePrib).Before(time.Time(*g.t.ProgJd)) {
			g.t.ProgJd = r.DatePrib // прибытие поезда = первая веха
		}

		subKey := r.IndexMain + "|" + r.StationNach + "|" + r.Naznach + "|" + r.CargoGroup
		sg, ok := g.subs[subKey]
		if !ok {
			sg = &GtSubGroupDTO{
				Key: subKey, StationNach: r.StationNach, CargoGroup: r.CargoGroup,
				Naznach: r.Naznach, Color: r.Color, IndexMain: r.IndexMain,
			}
			g.subs[subKey] = sg
			g.ord = append(g.ord, subKey)
		}
		sg.VagonCount++
		if r.DateNachD != nil && (sg.DateNach == nil ||
			time.Time(*r.DateNachD).After(time.Time(*sg.DateNach))) {
			sg.DateNach = r.DateNachD
		}
		if sg.Color == "" {
			sg.Color = r.Color
		}
	}

	out := make([]GtTrainDTO, 0, len(order))
	bi := 0
	for _, key := range order {
		g := groups[key]
		for _, sk := range g.ord {
			g.t.SubGroups = append(g.t.SubGroups, *g.subs[sk])
			g.t.VagonCount += g.subs[sk].VagonCount
		}
		if g.t.Index == "" {
			bi++
			g.t.Index = fmt.Sprintf("Б/И прибыв. %d", bi)
		}
		out = append(out, g.t)
	}
	return out
}

// gtFlowTrains — поезда потока (терминал × груз): эталон дублирует поезд по
// подгруппам, у каждой записи симуляции ровно одна подгруппа.
func gtFlowTrains(trains []GtTrainDTO, terminal, cargoKey string) []unloadsim.Train {
	var out []unloadsim.Train
	for _, t := range trains {
		if t.ProgJd == nil {
			continue
		}
		jd := time.Time(*t.ProgJd)
		for _, sg := range t.SubGroups {
			if sg.Naznach != terminal {
				continue
			}
			if cargoKey != "" && sg.CargoGroup != cargoKey {
				continue
			}
			out = append(out, unloadsim.Train{
				Index:    t.Index,
				CalcTime: unloadsim.RailwayToCalc(jd),
				OrigJd:   jd,
				Sub: unloadsim.SubGroup{
					Key: sg.Key, VagonCount: sg.VagonCount, Color: sg.Color,
					StationNach: sg.StationNach, IndexMain: sg.IndexMain, GruzpolS: sg.Naznach,
				},
			})
		}
	}
	return out
}

// jd18 — ЖД-время от МСК: час ≥ 18 → +1 сутки (бизнес-правило ЖД-суток).
func jd18(msk time.Time) time.Time {
	if msk.Hour() >= 18 {
		return msk.Add(24 * time.Hour)
	}
	return msk
}

// applyGtOverrides применяет what-if правки к очереди (по индексу поезда) и
// возвращает явные задержки delay_hours для распределения. Неизвестный поезд
// или правка прибывшего — ошибка (падать громко).
func applyGtOverrides(trains []GtTrainDTO, ovs []GtOverride) (map[string]time.Duration, error) {
	byIndex := map[string]int{}
	for i, t := range trains {
		byIndex[t.Index] = i
	}
	delays := map[string]time.Duration{}
	for _, ov := range ovs {
		i, ok := byIndex[ov.Index]
		if !ok {
			return nil, fmt.Errorf("правка %s: поезд %q не найден в очереди", ov.Action, ov.Index)
		}
		t := &trains[i]
		if t.IsArrived {
			return nil, fmt.Errorf("правка %s: поезд %q уже прибыл", ov.Action, ov.Index)
		}
		switch ov.Action {
		case "throw":
			if ov.DelayDays < 1 {
				return nil, fmt.Errorf("бросить %q: суток простоя должно быть ≥ 1", ov.Index)
			}
			t.Status = "5"
			t.PlanJd, t.PlanMsk = nil, nil // выбиваем из расписания → в пересчёт
			t.DelayHours = float64(ov.DelayDays * 24)
			delays[t.Index] = time.Duration(ov.DelayDays) * 24 * time.Hour
		case "restore":
			if ov.DelayHours < 0 {
				return nil, fmt.Errorf("восстановить %q: остаточная задержка не может быть отрицательной", ov.Index)
			}
			t.Status = "0"
			t.PlanJd, t.PlanMsk = nil, nil // в общую очередь
			t.DelayHours = ov.DelayHours
			delays[t.Index] = time.Duration(ov.DelayHours * float64(time.Hour))
			if delays[t.Index] == 0 {
				// «немедленно»: маркер, что штраф брошенного снят (Delay=0 в stage4
				// вернул бы BrosPenalty при Bros — но статус уже сброшен, помечать нечего)
				delete(delays, t.Index)
			}
		case "assign":
			if ov.Slot == nil {
				return nil, fmt.Errorf("на нитку %q: не указан слот", ov.Index)
			}
			msk := time.Time(*ov.Slot)
			lt := domain.LocalTime(msk)
			jd := domain.LocalTime(jd18(msk))
			t.PlanMsk, t.PlanJd = &lt, &jd
			t.DelayHours = 0
		case "move":
			if ov.MoveTo == "" {
				return nil, fmt.Errorf("переместить %q: не указан терминал", ov.Index)
			}
			moved := false
			for j := range t.SubGroups {
				if t.SubGroups[j].IsUniversal {
					t.SubGroups[j].Naznach = ov.MoveTo
					moved = true
				}
			}
			if !moved {
				return nil, fmt.Errorf("переместить %q: у поезда нет универсальных подгрупп", ov.Index)
			}
		default:
			return nil, fmt.Errorf("неизвестное действие правки: %q", ov.Action)
		}
	}
	return delays, nil
}

// recomputeArrival пересчитывает прогноз прибытия очереди алгоритмом DPmodule
// stage4 (pc-интервалы, реш. 000026 — НЕ фиксированные интервалы gtport) с
// фиксированным стартовым временем. Обновляются только поезда, попавшие в
// распределение (плановые + беспланные ≥ порога с RaschMsk); остальные
// сохраняют снимковый прогноз. RaschMsk не меняется никогда.
func (s *GtForecastService) recomputeArrival(trains []GtTrainDTO, station string, fixedStart time.Time, delays map[string]time.Duration) {
	pol := s.cfg.Settings().Stage4
	if pol.MinVagonCount <= 0 {
		pol.MinVagonCount = 20
	}
	if pol.MinVagonBros <= 0 {
		pol.MinVagonBros = 10
	}
	brosPenalty := time.Duration(pol.BrosPenaltyH) * time.Hour
	if brosPenalty <= 0 {
		brosPenalty = 72 * time.Hour
	}

	tol := map[string]time.Duration{}
	maxLen := map[string]int{}
	coef := map[string]float64{}
	for _, p := range s.cfg.PlanProfiles() {
		if p.SlotToleranceH > 0 {
			tol[p.StationCode] = time.Duration(p.SlotToleranceH * float64(time.Hour))
		}
		if p.MaxTrainLength > 0 {
			maxLen[p.StationCode] = p.MaxTrainLength
		}
		// Та же поправка приёма, что в конвейере (stage4_apply): вкладка what-if
		// обязана считать очередь причала теми же числами, что главный экран.
		if p.CorrectionCoef > 0 && p.CorrectionCoef != 1 {
			coef[p.StationCode] = p.CorrectionCoef
		}
	}

	var list []stage4.Train
	for i := range trains {
		t := &trains[i]
		if t.IsArrived || len(t.SubGroups) == 0 || t.RaschMsk == nil {
			continue
		}
		st, pc := resolveStationPc(s.dir, t.SubGroups[0].Naznach, t.SubGroups[0].CargoGroup)
		if st != station {
			continue
		}
		list = append(list, stage4.Train{
			Key: t.Index, Station: st,
			Group:      t.SubGroups[0].Naznach + "|" + cargoRod(t.SubGroups[0].CargoGroup),
			PlanMsk:    localTimePtr(t.PlanMsk),
			RaschMsk:   localTimePtr(t.RaschMsk),
			VagonCount: t.VagonCount, Pc: pc,
			Bros:  t.Status == "5",
			Delay: delays[t.Index],
		})
	}
	if len(list) == 0 {
		return
	}

	out := stage4.Distribute(list, toStage4Schedules(s.cfg.NitkaSchedule()), stage4.Config{
		MinVagon: pol.MinVagonCount, MinVagonBros: pol.MinVagonBros,
		BrosPenalty: brosPenalty, Tolerance: tol, MaxLen: maxLen, Coef: coef,
		Now: clock.Now().Time(), StartTime: &fixedStart,
	})

	for i := range trains {
		t := &trains[i]
		prog, ok := out[t.Index]
		if !ok {
			continue
		}
		pm := domain.LocalTime(prog)
		pj := domain.LocalTime(jd18(prog))
		t.ProgMsk, t.ProgJd = &pm, &pj
		// Mistake = (прог − (Rasch + эффективная задержка)) в сутках, со знаком:
		// явная what-if задержка замещает штраф бросания (эталон calcMistake).
		if t.RaschMsk != nil {
			eff := time.Time(*t.RaschMsk)
			if d, has := delays[t.Index]; has && d != 0 {
				eff = eff.Add(d)
			} else if t.Status == "5" {
				eff = eff.Add(brosPenalty)
			}
			m := prog.Sub(eff).Hours() / 24.0
			t.Mistake = &m
		}
	}
}

// freeSlots — НЕИСПОЛЬЗУЕМЫЕ нитки текущего плана подвода (эталон gtport:
// unused_slots аналитики плана, решение владельца 05.08.2026): по каждым
// суткам, на которые есть нитки свежего плана станции, — слоты расписания
// (nitka_schedule), не занятые нитками плана. Показывают резерв закреплённого
// расписания, а не «пустые дни» горизонта. Плана нет — ниток нет (не ошибка).
func (s *GtForecastService) freeSlots(ctx context.Context, station string, terminals []string, start time.Time, days int) ([]GtFreeSlotDTO, error) {
	slots := s.cfg.NitkaSchedule()[station]
	if len(slots) == 0 || s.plans == nil {
		return []GtFreeSlotDTO{}, nil
	}
	planCode := ""
	for _, term := range terminals {
		if p, ok := s.dir.PortByNameS(term); ok && p.PlanCode != "" {
			planCode = p.PlanCode
			break
		}
	}
	if planCode == "" {
		return []GtFreeSlotDTO{}, nil
	}
	_, nitki, err := s.plans.GetLatestPlan(ctx, planCode)
	if err != nil {
		return nil, fmt.Errorf("план подвода %s: %w", planCode, err)
	}
	return freeSlotsInHorizon(slots, nitki, start, days), nil
}

// freeSlotsInHorizon — слоты расписания минус нитки плана, по ЖД-СУТКАМ плана
// и в пределах горизонта прогноза. План подвода живёт в ЖД-шкале: дата PlanJd
// нитки — это ЖД-сутки её листа, слоты nitka_schedule — ЖД-времена тех же
// суток. Занятость — НЕ точным совпадением времени, а нормализацией эталона
// (gtport findUnusedSlotsForDateWithNormalization, plan_ma/nk_analytics.go):
// план верстают руками и времена ниток отклоняются от канона расписания, поезд
// «21:42» занимает слот 21:00 — каждая нитка потребляет БЛИЖАЙШИЙ ещё
// свободный слот (расстояние циклическое через полночь), свободные — остаток.
// Отсечка горизонтом дополнительно гасит слоты залежавшегося плана; расчётные
// ЖД-сутки D физически: (D−1) 18:00 … D 18:00.
func freeSlotsInHorizon(slots []domain.NitkaSlot, nitki []domain.PlanNitka, start time.Time, days int) []GtFreeSlotDTO {
	out := []GtFreeSlotDTO{}

	// Нитки по ЖД-суткам (дата PlanJd — ЖД-сутки листа), время минутами.
	byDay := map[time.Time][]int{}
	for _, n := range nitki {
		if n.IsOstatok || n.PlanJd == nil {
			continue
		}
		t := time.Time(*n.PlanJd)
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		byDay[day] = append(byDay[day], t.Hour()*60+t.Minute())
	}

	horFrom := start.AddDate(0, 0, -1).Add(18 * time.Hour)
	horTo := start.AddDate(0, 0, days-1).Add(18 * time.Hour)

	for jdDay, times := range byDay {
		sort.Ints(times) // эталон: времена по возрастанию
		free := make([]domain.NitkaSlot, len(slots))
		copy(free, slots)
		for _, tm := range times {
			if i := closestSlotIdx(tm, free); i >= 0 {
				free = append(free[:i], free[i+1:]...)
			}
		}
		for _, sl := range free {
			// Физическое время слота: ЖД-вечер (час ≥ 18) — предыдущий
			// календарный день (обратная сторона правила jd18).
			physDay := jdDay
			if sl.Hour >= 18 {
				physDay = jdDay.AddDate(0, 0, -1)
			}
			msk := time.Date(physDay.Year(), physDay.Month(), physDay.Day(), sl.Hour, sl.Minute, 0, 0, physDay.Location())
			if msk.Before(horFrom) || !msk.Before(horTo) {
				continue
			}
			out = append(out, GtFreeSlotDTO{Msk: domain.LocalTime(msk), Jd: domain.LocalTime(jd18(msk))})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return time.Time(out[i].Msk).Before(time.Time(out[j].Msk))
	})
	return out
}

// closestSlotIdx — индекс ближайшего к времени нитки слота (минуты ЖД-суток),
// расстояние циклическое через полночь (эталон findClosestAvailableSlotMa/Nk:
// diff > 12ч → 24ч − diff). Пустой список — −1.
func closestSlotIdx(minutes int, avail []domain.NitkaSlot) int {
	best, bestDiff := -1, 24*60
	for i, sl := range avail {
		diff := minutes - (sl.Hour*60 + sl.Minute)
		if diff < 0 {
			diff = -diff
		}
		if diff > 12*60 {
			diff = 24*60 - diff
		}
		if diff < bestDiff {
			bestDiff = diff
			best = i
		}
	}
	return best
}

// gtDaysDTO — результат симуляции → DTO диаграммы.
func gtDaysDTO(days []unloadsim.DayResult) []GtDayDTO {
	out := make([]GtDayDTO, 0, len(days))
	for _, d := range days {
		day := GtDayDTO{
			Date: d.Date, PlanSpeed: d.PlanSpeed, NormSpeed: d.NormSpeed,
			IncomingTotal: d.IncomingTotal, Arrival: d.Arrival,
			TotalFormation: d.TotalFormation, UsefulFormation: d.UsefulFormation,
			Unloaded: d.Unloaded, Remaining: d.Remaining, TotalWaitMin: d.TotalWaitMin,
			CarriedOver: []GtCarriedDTO{}, Operations: []GtOperationDTO{},
		}
		for _, c := range d.CarriedOver {
			day.CarriedOver = append(day.CarriedOver, GtCarriedDTO{Index: c.Index, Wagons: c.Sub.VagonCount})
		}
		for _, op := range d.Operations {
			o := GtOperationDTO{
				TrainIndex: op.TrainIndex, TrainName: op.TrainName,
				StationNach: op.StationNach, IndexMain: op.IndexMain,
				GruzpolS: op.GruzpolS, OrigIndex: op.OrigIndex,
				StartCalc: domain.LocalTime(op.StartCalc), EndCalc: domain.LocalTime(op.EndCalc),
				StartJd: domain.LocalTime(op.StartJd), EndJd: domain.LocalTime(op.EndJd),
				Wagons: op.Wagons, TotalWagons: op.TotalWagons, Color: op.Color,
				IsRemainder: op.IsRemainder, IsCarriedOver: op.IsCarriedOver,
				IsPartial: op.IsPartial, WaitMin: op.WaitMin,
			}
			if !op.OrigArrivalJd.IsZero() {
				lt := domain.LocalTime(op.OrigArrivalJd)
				o.OrigArrivalJd = &lt
			}
			day.Operations = append(day.Operations, o)
		}
		out = append(out, day)
	}
	return out
}
