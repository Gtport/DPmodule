package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// RearrangeService — экран «Перестановки/Переадресация» (перенос gtport
// RearrangementService, без хардкода станций и терминалов).
//
// Перестановки: распределение вагонов, идущих на наши станции с «управляемых»
// станций погрузки (пара станций разрешена справочником naznach_station,
// enabled), по терминалам (naznach). Переадресация: увод поезда целиком на
// терминал другой станции или внешний порт (pereadr_* вместо info_1/info_2).
// Правила целей (не хардкод, из реестра портов): перестановка — терминалы ТОЙ ЖЕ
// станции назначения; переадресация — терминалы ДРУГИХ станций + внешний порт.
//
// Запись — батчем: одна операция = одно применение к снимку + один пересчёт
// Stage 3–4 + одна атомарная подмена + одно событие журнала (rearrangement).
type RearrangeService struct {
	proc *LKProcessor // мьютекс пересборки, снимок, справочники, репозиторий, журнал
}

func NewRearrangeService(proc *LKProcessor) *RearrangeService {
	return &RearrangeService{proc: proc}
}

// ── Группировки (чтение из RAM-снимка) ──────────────────────────────────────

// RearrVagonDTO — вагон в подгруппе (детализация выбора).
type RearrVagonDTO struct {
	ID       string `json:"id"`
	Vagon    string `json:"vagon"`
	NppVag   *int   `json:"npp_vag"`
	Invoice  string `json:"invoice"`
	GruzpolS string `json:"gruzpol_s"`
	Naznach  string `json:"naznach"`
}

// RearrSubGroupDTO — второй уровень группировки. Заполнены поля своего режима.
type RearrSubGroupDTO struct {
	Key           string          `json:"key"`
	IndexMain     string          `json:"index_main,omitempty"`
	Index         string          `json:"index,omitempty"`
	StationOper   string          `json:"station_oper"`
	StationNach   string          `json:"station_nach,omitempty"`
	GruzpolS      string          `json:"gruzpol_s"`
	Naznach       string          `json:"naznach"`
	RasstStanNazn *int            `json:"rasst_stan_nazn"`
	Status        *int            `json:"status"`
	VagonCount    int             `json:"vagon_count"`
	Vagons        []RearrVagonDTO `json:"vagons"`
}

// RearrGroupDTO — первый уровень группировки (обе вкладки и оба режима).
type RearrGroupDTO struct {
	Key          string             `json:"key"`
	IndexMain    string             `json:"index_main,omitempty"`
	Index        string             `json:"index,omitempty"`
	StationNach  string             `json:"station_nach,omitempty"`
	StationOper  string             `json:"station_oper,omitempty"`
	StanNazn     string             `json:"stan_nazn"`      // станция назначения (имя, для показа)
	StanNaznCode string             `json:"stan_nazn_code"` // 4-значный код — опора правил целей
	GruzpolS     string             `json:"gruzpol_s,omitempty"`
	Naznach      string             `json:"naznach,omitempty"`
	PereadrPort  string             `json:"pereadr_port,omitempty"`
	Status       *int               `json:"status,omitempty"`
	Available    bool               `json:"available"`
	VagonCount   int                `json:"vagon_count"`
	SubGroups    []RearrSubGroupDTO `json:"sub_groups"`
}

// TargetDTO — цель перестановки/переадресации: терминал и ЕГО станция (из
// реестра портов; правила «своя/чужая станция» фронт считает по КОДАМ станций —
// имена в потоке и справочнике могут отличаться написанием).
type TargetDTO struct {
	Name        string `json:"name"`         // NameS терминала (значение naznach)
	Station     string `json:"station"`      // имя причальной станции терминала
	StationCode string `json:"station_code"` // 4-значный код станции (= code4_stan_nazn вагона)
}

// RearrGroupsDTO — ответ ручек группировок.
type RearrGroupsDTO struct {
	GroupBy string          `json:"group_by"`
	Groups  []RearrGroupDTO `json:"groups"`
	Targets []TargetDTO     `json:"targets"`
	Total   int             `json:"total"`
}

// terminalTargets — включённые терминалы с их станциями (реестр портов;
// ports.station_code — ШЕСТИзначный код станции → справочник станций по kod,
// оттуда имя и 4-значный код для сопоставления с code4_stan_nazn вагона).
func terminalTargets(dir *DirectoryCache) []TargetDTO {
	names := dir.EnabledTerminals()
	out := make([]TargetDTO, 0, len(names))
	for _, n := range names {
		t := TargetDTO{Name: n}
		if p, ok := dir.PortByNameS(n); ok {
			if kod, err := strconv.Atoi(p.StationCode); err == nil {
				if st, ok := dir.GetStationByKod(kod); ok {
					t.Station = st.Name
					t.StationCode = strconv.Itoa(st.Kod4)
				}
			}
		}
		out = append(out, t)
	}
	return out
}

// rearrangeTargets — цели ПЕРЕСТАНОВОК: терминалы только тех станций, где
// терминалов больше одного (перестановка действует в пределах станции —
// единственному терминалу станции переставлять не с кем; решение владельца).
func rearrangeTargets(dir *DirectoryCache) []TargetDTO {
	all := terminalTargets(dir)
	perStation := map[string]int{}
	for _, t := range all {
		if t.StationCode != "" {
			perStation[t.StationCode]++
		}
	}
	out := make([]TargetDTO, 0, len(all))
	for _, t := range all {
		if perStation[t.StationCode] >= 2 {
			out = append(out, t)
		}
	}
	return out
}

// RearrangementGroups — вкладка «Перестановки»: вагоны, у которых пара
// (станция назначения, станция погрузки) разрешена справочником naznach_station
// (перенос фильтра gtport «МЫС АСТАФЬЕВА + available-станции» без хардкода).
// groupBy: parent_index (по родительскому индексу) | collective_train (по сборному).
func (s *RearrangeService) RearrangementGroups(groupBy string) RearrGroupsDTO {
	dir := s.proc.intake.dir
	var filtered []domain.Dislocation
	for _, r := range s.proc.actual.All() {
		if _, ok := dir.GetNaznach(r.StanNazn, r.StationNach); ok {
			filtered = append(filtered, r)
		}
	}

	var groups []RearrGroupDTO
	switch groupBy {
	case "collective_train":
		groups = groupCollective(filtered)
	default:
		groupBy = "parent_index"
		groups = groupParentIndex(filtered)
	}
	return RearrGroupsDTO{
		GroupBy: groupBy, Groups: groups,
		Targets: rearrangeTargets(dir), Total: len(groups),
	}
}

func vagonDTO(r *domain.Dislocation) RearrVagonDTO {
	return RearrVagonDTO{
		ID: r.ID, Vagon: r.Vagon, NppVag: r.NppVag, Invoice: r.Invoice,
		GruzpolS: r.GruzpolS, Naznach: r.Naznach,
	}
}

// groupParentIndex — режим «по родительскому индексу»: index_main + станция
// погрузки → подгруппы по станции операции / индексу / naznach (эталон gtport).
func groupParentIndex(records []domain.Dislocation) []RearrGroupDTO {
	type subKey struct{ oper, index, naznach string }
	groups := map[string]*RearrGroupDTO{}
	subs := map[string]map[subKey]*RearrSubGroupDTO{}

	for i := range records {
		r := &records[i]
		gk := r.IndexMain + "|" + r.StationNach
		g, ok := groups[gk]
		if !ok {
			g = &RearrGroupDTO{
				Key: gk, IndexMain: r.IndexMain, StationNach: r.StationNach,
				StanNazn: r.StanNazn, StanNaznCode: r.Code4StanNazn,
				GruzpolS: r.GruzpolS, Naznach: r.Naznach,
				Available: true,
			}
			groups[gk] = g
			subs[gk] = map[subKey]*RearrSubGroupDTO{}
		}
		g.VagonCount++

		sk := subKey{r.StationOper, r.Index, r.Naznach}
		sg, ok := subs[gk][sk]
		if !ok {
			sg = &RearrSubGroupDTO{
				Key:         r.StationOper + "|" + r.Index + "|" + r.Naznach,
				StationOper: r.StationOper, Index: r.Index,
				GruzpolS: r.GruzpolS, Naznach: r.Naznach,
				RasstStanNazn: r.RasstStanNazn, Status: r.Status,
			}
			subs[gk][sk] = sg
		}
		sg.VagonCount++
		sg.Vagons = append(sg.Vagons, vagonDTO(r))
	}
	return assembleGroups(groups, subs)
}

// groupCollective — режим «по сборному поезду»: индекс + станция операции +
// расстояние + статус → подгруппы по родительскому индексу / станции погрузки /
// получателю / naznach (эталон gtport).
func groupCollective(records []domain.Dislocation) []RearrGroupDTO {
	type subKey struct{ im, sn, gp, nz string }
	groups := map[string]*RearrGroupDTO{}
	subs := map[string]map[subKey]*RearrSubGroupDTO{}

	for i := range records {
		r := &records[i]
		rasst := 0
		if r.RasstStanNazn != nil {
			rasst = *r.RasstStanNazn
		}
		st := ""
		if r.Status != nil {
			st = strconv.Itoa(*r.Status)
		}
		gk := r.Index + "|" + r.StationOper + "|" + strconv.Itoa(rasst) + "|" + st
		g, ok := groups[gk]
		if !ok {
			g = &RearrGroupDTO{
				Key: gk, Index: r.Index, StationOper: r.StationOper,
				StanNazn: r.StanNazn, StanNaznCode: r.Code4StanNazn,
				Status: r.Status, Available: true,
			}
			groups[gk] = g
			subs[gk] = map[subKey]*RearrSubGroupDTO{}
		}
		g.VagonCount++

		sk := subKey{r.IndexMain, r.StationNach, r.GruzpolS, r.Naznach}
		sg, ok := subs[gk][sk]
		if !ok {
			sg = &RearrSubGroupDTO{
				Key:       r.IndexMain + "|" + r.StationNach + "|" + r.GruzpolS + "|" + r.Naznach,
				IndexMain: r.IndexMain, StationNach: r.StationNach,
				GruzpolS: r.GruzpolS, Naznach: r.Naznach,
				RasstStanNazn: r.RasstStanNazn, Status: r.Status,
			}
			subs[gk][sk] = sg
		}
		sg.VagonCount++
		sg.Vagons = append(sg.Vagons, vagonDTO(r))
	}
	return assembleGroups(groups, subs)
}

// RedirectionGroups — вкладка «Переадресация»: весь снимок, группировка по
// index_main + pereadr_port + станция назначения + naznach; available — крупнейшая
// подгруппа ≥ порога и её накладные уникальны среди прочих подгрупп (эталон gtport).
// Порог — stage4.min_vagon_count (то же бизнес-значение «минимальный состав», 20).
func (s *RearrangeService) RedirectionGroups() RearrGroupsDTO {
	minVagons := s.proc.intake.cfg.Settings().Stage4.MinVagonCount
	if minVagons <= 0 {
		minVagons = 20
	}

	type subKey struct{ oper, gp, nz string }
	groups := map[string]*RearrGroupDTO{}
	subs := map[string]map[subKey]*RearrSubGroupDTO{}

	all := s.proc.actual.All()
	for i := range all {
		r := &all[i]
		gk := r.IndexMain + "|" + r.PereadrPort + "|" + r.StanNazn + "|" + r.Naznach
		g, ok := groups[gk]
		if !ok {
			g = &RearrGroupDTO{
				Key: gk, IndexMain: r.IndexMain, PereadrPort: r.PereadrPort,
				StanNazn: r.StanNazn, StanNaznCode: r.Code4StanNazn,
				StationNach: r.StationNach, Naznach: r.Naznach,
			}
			groups[gk] = g
			subs[gk] = map[subKey]*RearrSubGroupDTO{}
		}
		g.VagonCount++

		sk := subKey{r.StationOper, r.GruzpolS, r.Naznach}
		sg, ok := subs[gk][sk]
		if !ok {
			sg = &RearrSubGroupDTO{
				Key:         r.StationOper + "|" + r.GruzpolS + "|" + r.Naznach,
				StationOper: r.StationOper, GruzpolS: r.GruzpolS, Naznach: r.Naznach,
				RasstStanNazn: r.RasstStanNazn, Status: r.Status,
			}
			subs[gk][sk] = sg
		}
		sg.VagonCount++
		sg.Vagons = append(sg.Vagons, vagonDTO(r))
	}

	out := assembleGroups(groups, subs)
	for i := range out {
		out[i].Available = redirectAvailable(&out[i], minVagons)
	}
	return RearrGroupsDTO{
		GroupBy: "redirection", Groups: out,
		Targets: terminalTargets(s.proc.intake.dir), Total: len(out),
	}
}

// redirectAvailable — правило доступности переадресации (эталон gtport):
// крупнейшая подгруппа ≥ minVagons; единственная подгруппа доступна сразу;
// иначе накладные крупнейшей не должны встречаться в других подгруппах.
func redirectAvailable(g *RearrGroupDTO, minVagons int) bool {
	if len(g.SubGroups) == 0 {
		return false
	}
	largest := &g.SubGroups[0]
	for i := range g.SubGroups {
		if g.SubGroups[i].VagonCount > largest.VagonCount {
			largest = &g.SubGroups[i]
		}
	}
	if largest.VagonCount < minVagons {
		return false
	}
	if len(g.SubGroups) == 1 {
		return true
	}
	invoices := map[string]struct{}{}
	for _, v := range largest.Vagons {
		if v.Invoice != "" {
			invoices[v.Invoice] = struct{}{}
		}
	}
	if len(invoices) == 0 {
		return false
	}
	for i := range g.SubGroups {
		if &g.SubGroups[i] == largest {
			continue
		}
		for _, v := range g.SubGroups[i].Vagons {
			if _, clash := invoices[v.Invoice]; clash && v.Invoice != "" {
				return false
			}
		}
	}
	return true
}

// assembleGroups — map → отсортированный срез (группы по ключу, подгруппы по
// ключу, вагоны по npp_vag, затем по номеру — эталонная сортировка gtport).
func assembleGroups[K comparable](groups map[string]*RearrGroupDTO, subs map[string]map[K]*RearrSubGroupDTO) []RearrGroupDTO {
	out := make([]RearrGroupDTO, 0, len(groups))
	for gk, g := range groups {
		for _, sg := range subs[gk] {
			sortVagons(sg.Vagons)
			g.SubGroups = append(g.SubGroups, *sg)
		}
		sort.Slice(g.SubGroups, func(i, j int) bool { return g.SubGroups[i].Key < g.SubGroups[j].Key })
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func sortVagons(v []RearrVagonDTO) {
	sort.Slice(v, func(i, j int) bool {
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

// ── Панель станций (справочник naznach_station, операторская) ───────────────

// NaznachStationDTO — строка панели: пара станций и её дефолтное назначение.
type NaznachStationDTO struct {
	DestStation   string `json:"dest_station"`
	OriginStation string `json:"origin_station"`
	Naznach       string `json:"naznach"` // пусто = «по назначению» (родному получателю)
	Univers       bool   `json:"univers"`
	Enabled       bool   `json:"enabled"`
}

// Stations — включённые (enabled) строки справочника naznach_station, в том
// числе без назначения — колонка «По назначению» панели. Выключенные пары в
// панель не попадают: перестановки для них запрещены (Stage 2 их тоже не видит),
// управляют ими через админ-редактор справочников.
func (s *RearrangeService) Stations(ctx context.Context) ([]NaznachStationDTO, error) {
	rows, err := s.proc.intake.dir.NaznachStationRows(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]NaznachStationDTO, 0, len(rows))
	for _, r := range rows {
		if !r.Enabled {
			continue
		}
		out = append(out, NaznachStationDTO{
			DestStation: r.DestStation, OriginStation: r.OriginStation,
			Naznach: r.Naznach, Univers: r.Univers, Enabled: r.Enabled,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OriginStation == out[j].OriginStation {
			return out[i].DestStation < out[j].DestStation
		}
		return out[i].OriginStation < out[j].OriginStation
	})
	return out, nil
}

// StationNaznachUpdate — смена дефолтного назначения пары станций (drag&drop /
// ПКМ панели). Пустой naznach = «по назначению» (авто-перестановка выключается).
type StationNaznachUpdate struct {
	DestStation   string `json:"dest_station"`
	OriginStation string `json:"origin_station"`
	Naznach       string `json:"naznach"`
}

// UpdateStationNaznach валидирует терминал по реестру, пишет справочник и
// горячо перезагружает DirectoryCache (фильтр перестановок и Stage 2 видят
// правку сразу; пересчёт уже стоящих вагонов — «Обновить справочники» / тик).
func (s *RearrangeService) UpdateStationNaznach(ctx context.Context, req StationNaznachUpdate) error {
	if req.DestStation == "" || req.OriginStation == "" {
		return fmt.Errorf("%w: не указана пара станций", ErrBadRearrange)
	}
	dir := s.proc.intake.dir
	if req.Naznach != "" {
		if _, ok := dir.PortByNameS(req.Naznach); !ok {
			return fmt.Errorf("%w: неизвестный терминал %q", ErrBadRearrange, req.Naznach)
		}
	}
	if err := dir.UpdateNaznachStation(ctx, req.DestStation, req.OriginStation, req.Naznach); err != nil {
		return err
	}
	return nil
}

// ── Применение (запись в снимок, батчем) ────────────────────────────────────

// RearrApplyRequest — перестановка: выбранные вагоны → новый терминал, либо
// «по назначению» (by_gruzpol): каждому вагону его родной gruzpol_s.
type RearrApplyRequest struct {
	VagonIDs   []string `json:"vagon_ids"`
	NewNaznach string   `json:"new_naznach"`
	ByGruzpol  bool     `json:"by_gruzpol"`
}

// RedirectApplyRequest — переадресация: kind = own (свой терминал, target=NameS) |
// ext (внешний порт, target=имя) | cancel (отмена: naznach → родной gruzpol_s).
type RedirectApplyRequest struct {
	VagonIDs []string `json:"vagon_ids"`
	Kind     string   `json:"kind"`
	Target   string   `json:"target"`
}

// RearrApplyResult — итог операции.
type RearrApplyResult struct {
	Updated          int `json:"updated"`           // вагонов изменено
	Selected         int `json:"selected"`          // вагонов было выбрано
	ForecastComputed int `json:"forecast_computed"` // пересчитан ход (Stage 3)
	ProgComputed     int `json:"prog_computed"`     // пересчитан прогноз порта (Stage 4)
}

// ApplyRearrangement — перестановка терминала выбранным вагонам. Меняются только
// вагоны, у которых пара станций разрешена справочником (страховка уровня gtport);
// уже стоящие на целевом терминале пропускаются (Updated считает реальные правки).
// by_gruzpol («По назначению») — каждому выбранному его родной gruzpol_s.
func (s *RearrangeService) ApplyRearrangement(ctx context.Context, req RearrApplyRequest) (RearrApplyResult, error) {
	if len(req.VagonIDs) == 0 {
		return RearrApplyResult{}, fmt.Errorf("%w: не выбраны вагоны", ErrBadRearrange)
	}
	dir := s.proc.intake.dir
	journalTarget := req.NewNaznach
	if req.ByGruzpol {
		journalTarget = "по назначению"
	} else if _, ok := dir.PortByNameS(req.NewNaznach); !ok {
		return RearrApplyResult{}, fmt.Errorf("%w: неизвестный терминал %q", ErrBadRearrange, req.NewNaznach)
	}

	ids := toSet(req.VagonIDs)
	now := clock.Now()
	res, err := s.mutateSnapshot(ctx, "rearrangement", map[string]any{"new_naznach": journalTarget, "selected": len(req.VagonIDs)},
		func(all []domain.Dislocation) int {
			n := 0
			for i := range all {
				r := &all[i]
				if _, sel := ids[r.ID]; !sel {
					continue
				}
				if _, ok := dir.GetNaznach(r.StanNazn, r.StationNach); !ok {
					continue // пара станций не разрешена справочником
				}
				target := req.NewNaznach
				if req.ByGruzpol {
					target = r.GruzpolS // родной терминал КАЖДОГО вагона
				}
				if target == "" || r.Naznach == target {
					continue
				}
				r.Naznach = target
				r.UpdatedAt = now
				n++
			}
			return n
		})
	res.Selected = len(req.VagonIDs)
	return res, err
}

// ApplyRedirection — переадресация выбранных вагонов (обычно поезд целиком).
func (s *RearrangeService) ApplyRedirection(ctx context.Context, req RedirectApplyRequest) (RearrApplyResult, error) {
	if len(req.VagonIDs) == 0 {
		return RearrApplyResult{}, fmt.Errorf("%w: не выбраны вагоны", ErrBadRearrange)
	}
	dir := s.proc.intake.dir

	var mutate func(r *domain.Dislocation)
	switch req.Kind {
	case domain.PereadrOwn:
		if _, ok := dir.PortByNameS(req.Target); !ok {
			return RearrApplyResult{}, fmt.Errorf("%w: неизвестный терминал %q", ErrBadRearrange, req.Target)
		}
		mutate = func(r *domain.Dislocation) {
			r.Naznach = req.Target
			r.PereadrType = domain.PereadrOwn
			r.PereadrPort = ""
		}
	case domain.PereadrExt:
		if req.Target == "" {
			return RearrApplyResult{}, fmt.Errorf("%w: не указано имя внешнего порта", ErrBadRearrange)
		}
		mutate = func(r *domain.Dislocation) {
			r.Naznach = domain.NaznachExternalPort
			r.PereadrType = domain.PereadrExt
			r.PereadrPort = req.Target
		}
	case "cancel":
		mutate = func(r *domain.Dislocation) {
			r.Naznach = r.GruzpolS // возврат родному грузополучателю
			r.PereadrType = ""
			r.PereadrPort = ""
		}
	default:
		return RearrApplyResult{}, fmt.Errorf("%w: неизвестный вид переадресации %q", ErrBadRearrange, req.Kind)
	}

	ids := toSet(req.VagonIDs)
	now := clock.Now()
	res, err := s.mutateSnapshot(ctx, "redirection", map[string]any{"kind": req.Kind, "target": req.Target, "selected": len(req.VagonIDs)},
		func(all []domain.Dislocation) int {
			n := 0
			for i := range all {
				r := &all[i]
				if _, sel := ids[r.ID]; !sel {
					continue
				}
				mutate(r)
				r.UpdatedAt = now
				n++
			}
			return n
		})
	res.Selected = len(req.VagonIDs)
	return res, err
}

// ErrBadRearrange — ошибка валидации запроса перестановки/переадресации (→ 400).
var ErrBadRearrange = fmt.Errorf("некорректный запрос перестановки")

// mutateSnapshot — общий каркас записи: одна операция = мьютекс конвейера →
// правка копии RAM-снимка → ОДИН пересчёт Stage 3–4 → атомарная подмена →
// перечитка RAM → одно событие журнала (rearrangement). Ничего не изменилось →
// снимок не трогаем.
func (s *RearrangeService) mutateSnapshot(ctx context.Context, source string, detail map[string]any, mutate func([]domain.Dislocation) int) (RearrApplyResult, error) {
	p := s.proc
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.actual == nil {
		return RearrApplyResult{}, fmt.Errorf("%w: снимок дислокации не загружен", ErrNotReady)
	}
	all := p.actual.All()
	n := mutate(all)
	if n == 0 {
		return RearrApplyResult{}, nil
	}

	var cutoff int
	if ds, ok := p.intake.cfg.DataSource("lk"); ok {
		cutoff = ds.Config.DateCutoffHour
	}
	forecastN := applyForecast(all, p.intake.dir, cutoff)
	progN := applyStage4(all, p.intake.dir, p.intake.cfg, cutoff)

	if err := p.repo.ReplaceActual(ctx, all); err != nil {
		return RearrApplyResult{}, fmt.Errorf("замена снимка: %w", err)
	}
	if err := p.actual.Load(ctx); err != nil {
		return RearrApplyResult{}, fmt.Errorf("перечитывание актуальной мапы: %w", err)
	}

	if p.journal != nil {
		detail["count"] = n
		p.journal.RecordRearrangement(ctx, source, n, detail)
	}
	return RearrApplyResult{Updated: n, ForecastComputed: forecastN, ProgComputed: progN}, nil
}

func toSet(ids []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			m[id] = struct{}{}
		}
	}
	return m
}
