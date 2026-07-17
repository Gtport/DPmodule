package service

import (
	"context"
	"fmt"
	"sort"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// RearrangeService — экран «Перестановки/Переадресация» (перенос gtport
// RearrangementService, §HARDCODE: без хардкода станций и терминалов).
//
// Перестановки: распределение вагонов, идущих на наши станции с «управляемых»
// станций погрузки (пара станций разрешена справочником naznach_station,
// enabled), по терминалам (naznach). Переадресация: увод поезда целиком на
// другой свой терминал или внешний порт (pereadr_* вместо info_1/info_2).
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
	ID      string `json:"id"`
	Vagon   string `json:"vagon"`
	NppVag  *int   `json:"npp_vag"`
	Invoice string `json:"invoice"`
	Naznach string `json:"naznach"`
}

// RearrSubGroupDTO — второй уровень группировки.
type RearrSubGroupDTO struct {
	Key        string          `json:"key"`
	Label      string          `json:"label"`
	Naznach    string          `json:"naznach"`
	VagonCount int             `json:"vagon_count"`
	Vagons     []RearrVagonDTO `json:"vagons"`
}

// RearrGroupDTO — первый уровень группировки (обе вкладки и оба режима).
type RearrGroupDTO struct {
	Key        string             `json:"key"`
	Title      string             `json:"title"`    // левая часть заголовка
	Subtitle   string             `json:"subtitle"` // правая часть (станция/статус и т.п.)
	Naznach    string             `json:"naznach"`  // терминал группы (переадресация)
	Available  bool               `json:"available"`
	VagonCount int                `json:"vagon_count"`
	SubGroups  []RearrSubGroupDTO `json:"sub_groups"`
}

// RearrGroupsDTO — ответ ручек группировок.
type RearrGroupsDTO struct {
	GroupBy string          `json:"group_by"`
	Groups  []RearrGroupDTO `json:"groups"`
	Targets []string        `json:"targets"` // включённые терминалы (цели перестановки)
	Total   int             `json:"total"`
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
		Targets: dir.EnabledTerminals(), Total: len(groups),
	}
}

// groupParentIndex — режим «по родительскому индексу»: index_main + станция
// погрузки → подгруппы по станции операции / индексу / naznach (эталон gtport).
func groupParentIndex(records []domain.Dislocation) []RearrGroupDTO {
	type subKey struct{ oper, index, naznach string }
	groups := map[string]*RearrGroupDTO{}
	subs := map[string]map[subKey]*RearrSubGroupDTO{}

	for _, r := range records {
		gk := r.IndexMain + "|" + r.StationNach
		g, ok := groups[gk]
		if !ok {
			g = &RearrGroupDTO{
				Key: gk, Title: orDash(r.IndexMain), Subtitle: r.StationNach,
				Naznach: r.GruzpolS, Available: true,
			}
			groups[gk] = g
			subs[gk] = map[subKey]*RearrSubGroupDTO{}
		}
		g.VagonCount++

		sk := subKey{r.StationOper, r.Index, r.Naznach}
		sg, ok := subs[gk][sk]
		if !ok {
			sg = &RearrSubGroupDTO{
				Key:     r.StationOper + "|" + r.Index + "|" + r.Naznach,
				Label:   fmt.Sprintf("%s / %s / %s", orDash(r.StationOper), orDash(r.Index), orDash(r.Naznach)),
				Naznach: r.Naznach,
			}
			subs[gk][sk] = sg
		}
		sg.VagonCount++
		sg.Vagons = append(sg.Vagons, RearrVagonDTO{
			ID: r.ID, Vagon: r.Vagon, NppVag: r.NppVag, Invoice: r.Invoice, Naznach: r.Naznach,
		})
	}
	return assembleGroups(groups, subs)
}

// groupCollective — режим «по сборному поезду»: индекс + станция операции →
// подгруппы по родительскому индексу / станции погрузки / получателю / naznach.
func groupCollective(records []domain.Dislocation) []RearrGroupDTO {
	type subKey struct{ im, sn, gp, nz string }
	groups := map[string]*RearrGroupDTO{}
	subs := map[string]map[subKey]*RearrSubGroupDTO{}

	for _, r := range records {
		st := ""
		if r.Status != nil {
			st = fmt.Sprintf("статус %d", *r.Status)
		}
		gk := r.Index + "|" + r.StationOper + "|" + st
		g, ok := groups[gk]
		if !ok {
			g = &RearrGroupDTO{
				Key: gk, Title: orDash(r.Index), Subtitle: dotJoin(r.StationOper, st),
				Available: true,
			}
			groups[gk] = g
			subs[gk] = map[subKey]*RearrSubGroupDTO{}
		}
		g.VagonCount++

		sk := subKey{r.IndexMain, r.StationNach, r.GruzpolS, r.Naznach}
		sg, ok := subs[gk][sk]
		if !ok {
			sg = &RearrSubGroupDTO{
				Key:     r.IndexMain + "|" + r.StationNach + "|" + r.GruzpolS + "|" + r.Naznach,
				Label:   fmt.Sprintf("%s / %s / %s / %s", orDash(r.IndexMain), orDash(r.StationNach), orDash(r.GruzpolS), orDash(r.Naznach)),
				Naznach: r.Naznach,
			}
			subs[gk][sk] = sg
		}
		sg.VagonCount++
		sg.Vagons = append(sg.Vagons, RearrVagonDTO{
			ID: r.ID, Vagon: r.Vagon, NppVag: r.NppVag, Invoice: r.Invoice, Naznach: r.Naznach,
		})
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

	for _, r := range s.proc.actual.All() {
		gk := r.IndexMain + "|" + r.PereadrPort + "|" + r.StanNazn + "|" + r.Naznach
		g, ok := groups[gk]
		if !ok {
			g = &RearrGroupDTO{
				Key: gk, Title: orDash(r.IndexMain),
				Subtitle: dotJoin(r.StanNazn, r.PereadrPort),
				Naznach:  r.Naznach,
			}
			groups[gk] = g
			subs[gk] = map[subKey]*RearrSubGroupDTO{}
		}
		g.VagonCount++

		sk := subKey{r.StationOper, r.GruzpolS, r.Naznach}
		sg, ok := subs[gk][sk]
		if !ok {
			sg = &RearrSubGroupDTO{
				Key:     r.StationOper + "|" + r.GruzpolS + "|" + r.Naznach,
				Label:   fmt.Sprintf("%s / %s / %s", orDash(r.StationOper), orDash(r.GruzpolS), orDash(r.Naznach)),
				Naznach: r.Naznach,
			}
			subs[gk][sk] = sg
		}
		sg.VagonCount++
		sg.Vagons = append(sg.Vagons, RearrVagonDTO{
			ID: r.ID, Vagon: r.Vagon, NppVag: r.NppVag, Invoice: r.Invoice, Naznach: r.Naznach,
		})
	}

	out := assembleGroups(groups, subs)
	for i := range out {
		out[i].Available = redirectAvailable(&out[i], minVagons)
	}
	return RearrGroupsDTO{
		GroupBy: "redirection", Groups: out,
		Targets: s.proc.intake.dir.EnabledTerminals(), Total: len(out),
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

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// dotJoin — склейка непустых частей через « · » (заголовки групп).
func dotJoin(a, b string) string {
	switch {
	case a == "" || a == b:
		return b
	case b == "":
		return a
	default:
		return a + " · " + b
	}
}

// ── Применение (запись в снимок, батчем) ────────────────────────────────────

// RearrApplyRequest — перестановка: выбранные вагоны → новый терминал.
type RearrApplyRequest struct {
	VagonIDs   []string `json:"vagon_ids"`
	NewNaznach string   `json:"new_naznach"`
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
	ForecastComputed int `json:"forecast_computed"` // пересчитан ход (Stage 3)
	ProgComputed     int `json:"prog_computed"`     // пересчитан прогноз порта (Stage 4)
}

// ApplyRearrangement — перестановка терминала выбранным вагонам. Меняются только
// вагоны, у которых пара станций разрешена справочником (страховка уровня gtport).
func (s *RearrangeService) ApplyRearrangement(ctx context.Context, req RearrApplyRequest) (RearrApplyResult, error) {
	if len(req.VagonIDs) == 0 {
		return RearrApplyResult{}, fmt.Errorf("%w: не выбраны вагоны", ErrBadRearrange)
	}
	dir := s.proc.intake.dir
	if _, ok := dir.PortByNameS(req.NewNaznach); !ok {
		return RearrApplyResult{}, fmt.Errorf("%w: неизвестный терминал %q", ErrBadRearrange, req.NewNaznach)
	}

	ids := toSet(req.VagonIDs)
	now := clock.Now()
	return s.mutateSnapshot(ctx, "rearrangement", map[string]any{"new_naznach": req.NewNaznach},
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
				if r.Naznach == req.NewNaznach {
					continue
				}
				r.Naznach = req.NewNaznach
				r.UpdatedAt = now
				n++
			}
			return n
		})
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
	return s.mutateSnapshot(ctx, "redirection", map[string]any{"kind": req.Kind, "target": req.Target},
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
