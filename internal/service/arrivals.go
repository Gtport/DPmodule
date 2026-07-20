package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// ArrivalsService — «История прибывших» (домашняя страница, перенос gtport
// HistoryTable). Читает бизнес-историю vagon_history (веха прибытия — статус 10:
// date_prib/plan_*/otkl проставлены Stage 2) и группирует как эталон:
// группа = index_pp + date_prib → подгруппы по index_main/naznach/gruzpol_s/sms_1
// → вагоны. Станция на фронте — это набор её терминалов (реестр ports, не хардкод):
// фильтр по naznach, как в gtport (Мыс = АЭ+ГУТ-2, Находка = УТ-1).
//
// Кандидаты в прибывшие (наше отличие от gtport): вагоны статуса 9 (на станции
// назначения, АСУ не дала date_prib) оператор подтверждает или отклоняет вручную;
// подтверждение пишет факт в СНИМОК (статус 10, дальше держится sticky-10) и веху
// в историю; отклонение — пометка «скрыт до новых данных» на записи-кандидате.
type ArrivalsService struct {
	repo port.HistoryRepository
	dir  *DirectoryCache
	proc *LKProcessor // снимок/мьютекс/кандидаты для Confirm/Dismiss (nil в тестах чтения)
}

func NewArrivalsService(repo port.HistoryRepository, dir *DirectoryCache, proc *LKProcessor) *ArrivalsService {
	return &ArrivalsService{repo: repo, dir: dir, proc: proc}
}

// ArrivalVagonDTO — вагон подгруппы (разворот по клику).
type ArrivalVagonDTO struct {
	ID        string `json:"id"`
	Vagon     string `json:"vagon"`
	Shipments string `json:"shipments,omitempty"` // судовая партия
	Status    *int   `json:"status,omitempty"`
}

// ArrivalSubgroupDTO — подгруппа прибывшего поезда (одно назначение/получатель).
type ArrivalSubgroupDTO struct {
	Key         string            `json:"key"`
	IndexMain   string            `json:"index_main"`
	StationNach string            `json:"station_nach"`
	Naznach     string            `json:"naznach"`
	GruzpolS    string            `json:"gruzpol_s"`
	Sms1        string            `json:"sms_1,omitempty"`
	VagonCount  int               `json:"vagon_count"`
	Display     string            `json:"display"` // «(N)-783-Челутай АЭ» / «… ГУТ-2 → АЭ»
	Vagons      []ArrivalVagonDTO `json:"vagons"`
}

// ArrivalGroupDTO — прибывший поезд (группа index_pp + date_prib).
type ArrivalGroupDTO struct {
	Key        string               `json:"key"`
	IndexPp    string               `json:"index_pp"`
	StanNazn   string               `json:"stan_nazn"`
	DatePribD  *domain.LocalTime    `json:"date_prib_d"`
	DatePrib   *domain.LocalTime    `json:"date_prib"`
	PlanMsk    *domain.LocalTime    `json:"plan_msk"`
	PlanJd     *domain.LocalTime    `json:"plan_jd"`
	Otkl       string               `json:"otkl"`
	VagonCount int                  `json:"vagon_count"`
	SubGroups  []ArrivalSubgroupDTO `json:"sub_groups"`
}

// ArrivalsDTO — ответ ручки: группы за период + реестр терминалов (для колонок).
type ArrivalsDTO struct {
	From    string            `json:"from"`
	To      string            `json:"to"`
	Groups  []ArrivalGroupDTO `json:"groups"`
	Targets []TargetDTO       `json:"targets"`
	Total   int               `json:"total"`
}

// Groups — прибывшие за период [from; to] (yyyy-MM-dd; пусто — вчера/сегодня по
// МСК), с фильтром по терминалам naznach (пусто — все).
func (s *ArrivalsService) Groups(ctx context.Context, fromS, toS string, naznach []string) (ArrivalsDTO, error) {
	from, to, err := arrivalsRange(fromS, toS)
	if err != nil {
		return ArrivalsDTO{}, err
	}
	rows, err := s.repo.ArrivedRows(ctx, from, to, naznach)
	if err != nil {
		return ArrivalsDTO{}, err
	}
	groups := groupArrivals(rows)
	return ArrivalsDTO{
		From: from.String()[:10], To: to.String()[:10],
		Groups: groups, Targets: terminalTargets(s.dir), Total: len(groups),
	}, nil
}

// Terminals — реестр целей (терминал + его станция) для раскладки домашней
// страницы по станциям (не хардкод — ports).
func (s *ArrivalsService) Terminals() []TargetDTO {
	return terminalTargets(s.dir)
}

// ── Правки истории прибывших (перенос gtport update-by-ids) ─────────────────

// ErrArrivalsAccess — правка запрещена правилом дат (обратиться к администратору).
var ErrArrivalsAccess = fmt.Errorf("правка запрещена")

// ArrivalsUpdateRequest — правка ВЫБРАННЫХ вагонов истории (все операции — по id
// вагонов, решение владельца). Заполняются только применяемые поля.
// «Отменить прибытие» — отдельная операция CancelArrival (снимок + история).
type ArrivalsUpdateRequest struct {
	VagonIDs []string `json:"vagon_ids"`
	// «Изменить прибытие»: индекс поезда, план (ЖД) и факт; otkl/plan_msk/
	// date_prib_d пересчитываются на сервере.
	IndexPp  string            `json:"index_pp,omitempty"`
	PlanJd   *domain.LocalTime `json:"plan_jd,omitempty"`
	DatePrib *domain.LocalTime `json:"date_prib,omitempty"`
	// «Выгрузить»: факт выгрузки, место, смерзаемость %.
	DateVigr  *domain.LocalTime `json:"date_vigr,omitempty"`
	PlaceVigr *string           `json:"place_vigr,omitempty"`
	Frost     *int              `json:"frost,omitempty"`
	// «Изменить назначение»: перераспределение по терминалам после прибытия
	// (значение валидируется по реестру портов).
	Naznach string `json:"naznach,omitempty"`
}

// ArrivalsUpdateResult — честный итог: сколько строк реально обновлено.
type ArrivalsUpdateResult struct {
	Updated  int `json:"updated"`
	Selected int `json:"selected"`
}

// UpdateVagons применяет правку к выбранным вагонам истории. Доступ (эталон
// gtport): administrator — без ограничений; остальным можно править только
// прибытия за СЕГОДНЯ/ВЧЕРА (МСК). Пересчёты — по каждому вагону (otkl зависит
// от его plan_msk/date_prib). Атомарно: один батч = одна транзакция.
func (s *ArrivalsService) UpdateVagons(ctx context.Context, req ArrivalsUpdateRequest) (ArrivalsUpdateResult, error) {
	if len(req.VagonIDs) == 0 {
		return ArrivalsUpdateResult{}, fmt.Errorf("не выбраны вагоны")
	}
	if req.IndexPp == "" && req.PlanJd == nil && req.DatePrib == nil &&
		req.DateVigr == nil && req.PlaceVigr == nil && req.Frost == nil && req.Naznach == "" {
		return ArrivalsUpdateResult{}, fmt.Errorf("не указано ни одного изменения")
	}
	if req.Naznach != "" {
		if _, ok := s.dir.PortByNameS(req.Naznach); !ok {
			return ArrivalsUpdateResult{}, fmt.Errorf("неизвестный терминал %q", req.Naznach)
		}
	}
	rows, err := s.repo.RowsByIDs(ctx, req.VagonIDs)
	if err != nil {
		return ArrivalsUpdateResult{}, err
	}
	if err := checkArrivalsEditAccess(ctx, rows); err != nil {
		return ArrivalsUpdateResult{}, err
	}

	now := clock.Now()

	// «Изменить назначение» — СКВОЗНАЯ операция (решение владельца): перестановка
	// прибывшего отражается и в СНИМКЕ (для вагонов текущего снимка — иначе
	// «Не выгружено» Оперативки и экраны снимка не видят перестановку; дальше
	// живёт carry-over), и в истории. Вагоны старых рейсов — только история.
	if req.Naznach != "" && s.proc != nil {
		ids := map[string]struct{}{}
		for _, id := range req.VagonIDs {
			ids[id] = struct{}{}
		}
		if _, _, _, err := s.proc.MutateSnapshot(ctx, "arrival_rearrange",
			nil, // журнал — единой записью arrivals_edit ниже (без дублей)
			func(all []domain.Dislocation) int {
				n := 0
				for i := range all {
					r := &all[i]
					if _, sel := ids[r.ID]; !sel || r.Naznach == req.Naznach {
						continue
					}
					r.Naznach = req.Naznach
					r.UpdatedAt = now
					n++
				}
				return n
			}); err != nil {
			return ArrivalsUpdateResult{}, fmt.Errorf("перестановка в снимке: %w", err)
		}
	}

	updates := make(map[string]map[string]any, len(rows))
	for i := range rows {
		updates[rows[i].ID] = arrivalUpdateFields(&rows[i], req, now)
	}
	if err := s.repo.UpdateFieldsBatch(ctx, updates); err != nil {
		return ArrivalsUpdateResult{}, err
	}
	s.journalEdit(ctx, req, len(rows))
	return ArrivalsUpdateResult{Updated: len(rows), Selected: len(req.VagonIDs)}, nil
}

// journalEdit — запись операторского действия с прибывшими в единый журнал
// (кто/что/сколько): код действия по заполненным полям запроса.
func (s *ArrivalsService) journalEdit(ctx context.Context, req ArrivalsUpdateRequest, count int) {
	if s.proc == nil || s.proc.journal == nil {
		return
	}
	var actions []string
	extra := map[string]any{"selected": len(req.VagonIDs)}
	if req.IndexPp != "" || req.PlanJd != nil || req.DatePrib != nil {
		actions = append(actions, "edit_arrival")
		if req.IndexPp != "" {
			extra["index_pp"] = req.IndexPp
		}
	}
	if req.DateVigr != nil || req.PlaceVigr != nil || req.Frost != nil {
		actions = append(actions, "unload")
		if req.PlaceVigr != nil {
			extra["place_vigr"] = *req.PlaceVigr
		}
	}
	if req.Naznach != "" {
		actions = append(actions, "set_naznach")
		extra["naznach"] = req.Naznach
	}
	if len(actions) == 0 {
		return
	}
	s.proc.journal.RecordArrivalsEdit(ctx, strings.Join(actions, "+"), count, extra)
}

// checkArrivalsEditAccess — правило дат (эталон gtport, строже: проверяем КАЖДЫЙ
// вагон, не только первый): не-администратору можно править лишь строки с датой
// прибытия сегодня/вчера (или без неё). Без claims (auth выключен) — разрешаем.
func checkArrivalsEditAccess(ctx context.Context, rows []domain.VagonHistory) error {
	cl := auth.ClaimsFromContext(ctx)
	if cl == nil || cl.HasRole(auth.RoleAdministrator) {
		return nil
	}
	today := clock.Now().Time().Truncate(24 * time.Hour)
	yesterday := today.AddDate(0, 0, -1)
	for i := range rows {
		d := rows[i].DatePribD
		if d == nil {
			continue
		}
		day := d.Time().Truncate(24 * time.Hour)
		if !day.Equal(today) && !day.Equal(yesterday) {
			return fmt.Errorf("%w: править можно только прибытия за сегодня/вчера "+
				"(вагон %s прибыл %s) — обратитесь к администратору",
				ErrArrivalsAccess, rows[i].Vagon, d.String()[:10])
		}
	}
	return nil
}

// arrivalUpdateFields — колонки правки одного вагона. Пересчёты как в Stage 2:
// date_prib_d = дата факта; plan_msk = plan_jd с правилом «час ≥ 18 → −сутки»
// (операционный календарь, не таймзона); otkl = факт − план.
func arrivalUpdateFields(r *domain.VagonHistory, req ArrivalsUpdateRequest, now domain.LocalTime) map[string]any {
	fields := map[string]any{"updated_at": &now}
	if req.IndexPp != "" {
		fields["index_pp"] = req.IndexPp
	}
	planMsk := r.PlanMsk
	if req.PlanJd != nil {
		planMsk = planMskFromJd(req.PlanJd)
		fields["plan_jd"] = req.PlanJd
		fields["plan_msk"] = planMsk
	}
	datePrib := r.DatePrib
	if req.DatePrib != nil {
		datePrib = req.DatePrib
		fields["date_prib"] = req.DatePrib
		fields["date_prib_d"] = dateOnly(req.DatePrib)
	}
	if req.PlanJd != nil || req.DatePrib != nil {
		fields["otkl"] = calculateOtkl(datePrib, planMsk)
	}
	if req.DateVigr != nil {
		fields["date_vigr"] = req.DateVigr
		// ЖД-сутки выгрузки — как у автоматики (dateOnly от ЖД-времени):
		// вечерняя выгрузка (час ≥ 18) относится к следующим операционным суткам.
		fields["date_vigr_d"] = dateOnly(jdFromFact(req.DateVigr))
	}
	if req.PlaceVigr != nil {
		fields["place_vigr"] = *req.PlaceVigr
	}
	if req.Frost != nil {
		fields["frost"] = req.Frost
	}
	if req.Naznach != "" {
		fields["naznach"] = req.Naznach
	}
	return fields
}

// planMskFromJd — МСК-календарь нитки из ЖД-времени: «час ≥ 18 → предыдущие
// операционные сутки» (бизнес-правило, эталон парсера плана applyMskRule).
func planMskFromJd(jd *domain.LocalTime) *domain.LocalTime {
	t := jd.Time()
	if t.Hour() >= 18 {
		t = t.AddDate(0, 0, -1)
	}
	return domain.NewLocalTime(t)
}

// jdFromFact — ЖД-время из фактического МСК: «час ≥ 18 → следующие операционные
// сутки» (обратное правило; для ЖД-суток ручных вех — как date_op_jd автоматики).
func jdFromFact(fact *domain.LocalTime) *domain.LocalTime {
	t := fact.Time()
	if t.Hour() >= 18 {
		t = t.AddDate(0, 0, 1)
	}
	return domain.NewLocalTime(t)
}

// ── Кандидаты в прибывшие (статус 9 — ручное подтверждение оператором) ──────

// CandidateGroupDTO — поезд-кандидат: вагоны статуса 9 одного индекса.
type CandidateGroupDTO struct {
	Key         string               `json:"key"`
	Index       string               `json:"index"`
	StanNazn    string               `json:"stan_nazn"`
	StationNach string               `json:"station_nach"`
	TimeOp      *domain.LocalTime    `json:"time_op"` // последняя операция (момент постановки)
	VagonCount  int                  `json:"vagon_count"`
	SubGroups   []ArrivalSubgroupDTO `json:"sub_groups"`
}

// Candidates — живые кандидаты из снимка (статус 9) минус отклонённые оператором,
// с фильтром по терминалам naznach; группы по текущему индексу поезда.
func (s *ArrivalsService) Candidates(ctx context.Context, naznach []string) ([]CandidateGroupDTO, error) {
	dismissed, err := s.proc.status9.DismissedVagons(ctx)
	if err != nil {
		return nil, err
	}
	nz := map[string]struct{}{}
	for _, n := range naznach {
		nz[n] = struct{}{}
	}

	type subKey struct{ im, nzn, gp string }
	var order []string
	groups := map[string]*CandidateGroupDTO{}
	subs := map[string]map[subKey]*ArrivalSubgroupDTO{}
	subOrder := map[string][]subKey{}

	for _, r := range s.proc.actual.All() {
		if r.Status == nil || *r.Status != 9 {
			continue
		}
		if len(nz) > 0 {
			if _, ok := nz[r.Naznach]; !ok {
				continue
			}
		}
		if _, off := dismissed[r.Vagon]; off {
			continue
		}
		gk := r.Index + "|" + r.StanNazn
		g, ok := groups[gk]
		if !ok {
			g = &CandidateGroupDTO{
				Key: gk, Index: r.Index, StanNazn: r.StanNazn, StationNach: r.StationNach,
				TimeOp: r.TimeOp,
			}
			groups[gk] = g
			subs[gk] = map[subKey]*ArrivalSubgroupDTO{}
			order = append(order, gk)
		}
		g.VagonCount++
		if r.TimeOp != nil && (g.TimeOp == nil || g.TimeOp.Time().Before(r.TimeOp.Time())) {
			g.TimeOp = r.TimeOp // самая свежая операция поезда
		}

		sk := subKey{r.IndexMain, r.Naznach, r.GruzpolS}
		sg, ok := subs[gk][sk]
		if !ok {
			sg = &ArrivalSubgroupDTO{
				Key:       r.IndexMain + "|" + r.Naznach + "|" + r.GruzpolS,
				IndexMain: r.IndexMain, StationNach: r.StationNach,
				Naznach: r.Naznach, GruzpolS: r.GruzpolS,
			}
			subs[gk][sk] = sg
			subOrder[gk] = append(subOrder[gk], sk)
		}
		sg.VagonCount++
		sg.Vagons = append(sg.Vagons, ArrivalVagonDTO{ID: r.ID, Vagon: r.Vagon, Status: r.Status})
	}

	out := make([]CandidateGroupDTO, 0, len(order))
	for _, gk := range order {
		g := groups[gk]
		for _, sk := range subOrder[gk] {
			sg := subs[gk][sk]
			sg.Display = arrivalDisplay(sg)
			g.SubGroups = append(g.SubGroups, *sg)
		}
		sort.SliceStable(g.SubGroups, func(i, j int) bool { return g.SubGroups[i].Key < g.SubGroups[j].Key })
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// ConfirmArrivalRequest — подтверждение прибытия кандидатов: выбранные вагоны +
// фактическое время (дефолт на фронте — время операции поезда). Index — правка
// индекса поезда оператором (пусто — оставить индекс из снимка).
type ConfirmArrivalRequest struct {
	VagonIDs []string          `json:"vagon_ids"`
	DatePrib *domain.LocalTime `json:"date_prib"`
	Index    string            `json:"index,omitempty"`
}

// ConfirmArrival — подтверждение прибытия: в СНИМОК проставляются date_prib,
// статус 10 и date_kon (дальше факт держится sticky-10 при пересборках, запись-
// кандидат снимается reconcile'ом), пересчёт Stage 3–4 и подмена — одним батчем
// (каркас MutateSnapshot); веха прибытия пишется в vagon_history здесь же
// (следующие пересборки перехода 9→10 уже не увидят).
func (s *ArrivalsService) ConfirmArrival(ctx context.Context, req ConfirmArrivalRequest) (ArrivalsUpdateResult, error) {
	if len(req.VagonIDs) == 0 {
		return ArrivalsUpdateResult{}, fmt.Errorf("не выбраны вагоны")
	}
	if req.DatePrib == nil || req.DatePrib.IsZero() {
		return ArrivalsUpdateResult{}, fmt.Errorf("не указано время прибытия")
	}
	ids := map[string]struct{}{}
	for _, id := range req.VagonIDs {
		ids[id] = struct{}{}
	}

	now := clock.Now()
	var confirmed []domain.Dislocation
	n, _, _, err := s.proc.MutateSnapshot(ctx, "arrival_confirm",
		map[string]any{"selected": len(req.VagonIDs), "date_prib": req.DatePrib.String()},
		func(all []domain.Dislocation) int {
			cnt := 0
			for i := range all {
				r := &all[i]
				if _, sel := ids[r.ID]; !sel {
					continue
				}
				if r.Status == nil || *r.Status != 9 {
					continue // подтверждаются только кандидаты
				}
				st := 10
				r.Status = &st
				r.DatePrib = req.DatePrib
				r.DateKon = r.DateOpJd // как computeDateKon для статуса 10
				if req.Index != "" {
					r.Index = req.Index // правка индекса поезда оператором
				}
				r.UpdatedAt = now
				confirmed = append(confirmed, *r)
				cnt++
			}
			return cnt
		})
	if err != nil {
		return ArrivalsUpdateResult{}, err
	}

	// Веха прибытия в историю — те же поля, что пишет Stage 2 на переходе в 10.
	updates := make(map[string]map[string]any, len(confirmed))
	for i := range confirmed {
		r := &confirmed[i]
		fields := map[string]any{
			"status":      10,
			"date_prib":   r.DatePrib,
			"date_prib_d": dateOnly(r.DatePrib),
			"delay":       calculateHistoryDelay(dateOnly(r.DatePrib), r.DateDostav),
			"otkl":        calculateOtkl(r.DatePrib, r.PlanMsk),
			"plan_msk":    r.PlanMsk,
			"plan_jd":     r.PlanJd,
			"naznach":     r.Naznach,
			"updated_at":  &now,
		}
		if r.Index != "" {
			fields["index_pp"] = r.Index // фактический поезд прибытия (решение владельца)
		}
		updates[r.ID] = fields
	}
	if err := s.repo.UpdateFieldsBatch(ctx, updates); err != nil {
		return ArrivalsUpdateResult{}, fmt.Errorf("веха прибытия в историю: %w", err)
	}
	return ArrivalsUpdateResult{Updated: n, Selected: len(req.VagonIDs)}, nil
}

// CancelArrival — «Отменить прибытие» (переосмысление gtport по решению
// владельца, симметрия с ConfirmArrival): в СНИМКЕ вагон возвращается из 10 в 9
// (сброс date_prib; date_kon — по правилу не-10 из time_op), снова становясь
// кандидатом; веха прибытия в истории очищается. С потоком не спорим: если АСУ
// снова принесёт date_prib, ближайший пул вернёт вагон в прибывшие.
//
// Ограничения: отменять можно только статус 10 (выгрузка-12 доказывает прибытие
// — запрет), и только вагоны текущего снимка; права — как у остальных правок
// (не-админам лишь прибытия за сегодня/вчера).
func (s *ArrivalsService) CancelArrival(ctx context.Context, vagonIDs []string) (ArrivalsUpdateResult, error) {
	if len(vagonIDs) == 0 {
		return ArrivalsUpdateResult{}, fmt.Errorf("не выбраны вагоны")
	}
	ids := map[string]struct{}{}
	for _, id := range vagonIDs {
		ids[id] = struct{}{}
	}

	// Предпроверка по снимку: выгруженные и отсутствующие — понятная ошибка ДО правки.
	inSnapshot := 0
	for _, r := range s.proc.actual.All() {
		if _, sel := ids[r.ID]; !sel {
			continue
		}
		inSnapshot++
		if r.Status != nil && *r.Status >= 12 {
			return ArrivalsUpdateResult{}, fmt.Errorf(
				"вагон %s уже выгружен — отмена прибытия недоступна (выгрузка доказывает прибытие)", r.Vagon)
		}
	}
	if inSnapshot == 0 {
		return ArrivalsUpdateResult{}, fmt.Errorf(
			"выбранных вагонов нет в текущем снимке — отменить можно только свежие прибытия")
	}

	// Права по датам — по строкам истории (как у остальных правок).
	rows, err := s.repo.RowsByIDs(ctx, vagonIDs)
	if err != nil {
		return ArrivalsUpdateResult{}, err
	}
	if err := checkArrivalsEditAccess(ctx, rows); err != nil {
		return ArrivalsUpdateResult{}, err
	}

	now := clock.Now()
	var cancelled []string
	n, _, _, err := s.proc.MutateSnapshot(ctx, "arrival_cancel",
		map[string]any{"selected": len(vagonIDs)},
		func(all []domain.Dislocation) int {
			cnt := 0
			for i := range all {
				r := &all[i]
				if _, sel := ids[r.ID]; !sel {
					continue
				}
				if r.Status == nil || *r.Status != 10 {
					continue // отменяется только подтверждённое прибытие
				}
				st := 9
				r.Status = &st
				r.DatePrib = nil
				r.DateKon = r.TimeOp // computeDateKon для не-10
				r.UpdatedAt = now
				cancelled = append(cancelled, r.ID)
				cnt++
			}
			return cnt
		})
	if err != nil {
		return ArrivalsUpdateResult{}, err
	}

	// Очистка вехи прибытия в истории (сброс полей перехода-10).
	updates := make(map[string]map[string]any, len(cancelled))
	for _, id := range cancelled {
		updates[id] = map[string]any{
			"status": 9, "date_prib": nil, "date_prib_d": nil,
			"otkl": "", "delay": nil, "updated_at": &now,
		}
	}
	if err := s.repo.UpdateFieldsBatch(ctx, updates); err != nil {
		return ArrivalsUpdateResult{}, fmt.Errorf("очистка вехи прибытия в истории: %w", err)
	}
	return ArrivalsUpdateResult{Updated: n, Selected: len(vagonIDs)}, nil
}

// DismissCandidates — «скрыть до новых данных»: пометка на записях-кандидатах;
// вагоны остаются в статусе 9, при появлении date_prib из АСУ станут 10 сами.
func (s *ArrivalsService) DismissCandidates(ctx context.Context, vagonIDs []string) (ArrivalsUpdateResult, error) {
	if len(vagonIDs) == 0 {
		return ArrivalsUpdateResult{}, fmt.Errorf("не выбраны вагоны")
	}
	ids := map[string]struct{}{}
	for _, id := range vagonIDs {
		ids[id] = struct{}{}
	}
	var vagons []string
	for _, r := range s.proc.actual.All() {
		if _, sel := ids[r.ID]; sel && r.Status != nil && *r.Status == 9 && r.Vagon != "" {
			vagons = append(vagons, r.Vagon)
		}
	}
	n, err := s.proc.status9.SetDismissed(ctx, vagons, clock.Now())
	if err != nil {
		return ArrivalsUpdateResult{}, err
	}
	if s.proc.journal != nil {
		s.proc.journal.RecordArrivalsEdit(ctx, "dismiss_candidate", n,
			map[string]any{"selected": len(vagonIDs)})
	}
	return ArrivalsUpdateResult{Updated: n, Selected: len(vagonIDs)}, nil
}

// arrivalsRange — границы периода из строк yyyy-MM-dd; дефолт «вчера — сегодня»
// (эталон gtport «СЕГОДНЯ/ВЧЕРА») от московского clock.Now().
func arrivalsRange(fromS, toS string) (domain.LocalTime, domain.LocalTime, error) {
	today := clock.Now().Time().Truncate(24 * time.Hour)
	from, to := today.AddDate(0, 0, -1), today
	if fromS != "" {
		t, err := time.Parse("2006-01-02", fromS)
		if err != nil {
			return domain.LocalTime{}, domain.LocalTime{}, fmt.Errorf("некорректная дата from %q (ожидается yyyy-MM-dd)", fromS)
		}
		from = t
	}
	if toS != "" {
		t, err := time.Parse("2006-01-02", toS)
		if err != nil {
			return domain.LocalTime{}, domain.LocalTime{}, fmt.Errorf("некорректная дата to %q (ожидается yyyy-MM-dd)", toS)
		}
		to = t
	}
	if to.Before(from) {
		from, to = to, from
	}
	return domain.LocalTime(from), domain.LocalTime(to), nil
}

// groupArrivals — группировка строк истории в поезда (эталон gtport):
// группа = index_pp + date_prib, подгруппа = index_main|naznach|gruzpol_s|sms_1.
// Строки приходят отсортированными (date_prib, index_pp, vagon) — порядок
// групп/вагонов сохраняется стабильным.
func groupArrivals(rows []domain.VagonHistory) []ArrivalGroupDTO {
	type subKey struct{ im, nz, gp, sms string }
	var order []string
	groups := map[string]*ArrivalGroupDTO{}
	subs := map[string]map[subKey]*ArrivalSubgroupDTO{}
	subOrder := map[string][]subKey{}

	for i := range rows {
		r := &rows[i]
		// Метка поезда: index_pp (нитка плана); для строк без матча с планом —
		// родительский индекс (иначе группа безымянная: старые записи истории
		// создавались до фиксации index_pp на прибытии).
		label := r.IndexPp
		if label == "" {
			label = r.IndexMain
		}
		gk := label + "|" + ltKey(r.DatePrib)
		g, ok := groups[gk]
		if !ok {
			g = &ArrivalGroupDTO{
				Key: gk, IndexPp: label, StanNazn: r.StanNazn,
				DatePribD: r.DatePribD, DatePrib: r.DatePrib,
				PlanMsk: r.PlanMsk, PlanJd: r.PlanJd, Otkl: r.Otkl,
			}
			groups[gk] = g
			subs[gk] = map[subKey]*ArrivalSubgroupDTO{}
			order = append(order, gk)
		}
		g.VagonCount++

		sk := subKey{r.IndexMain, r.Naznach, r.GruzpolS, r.Sms1}
		sg, ok := subs[gk][sk]
		if !ok {
			sg = &ArrivalSubgroupDTO{
				Key:       r.IndexMain + "|" + r.Naznach + "|" + r.GruzpolS + "|" + r.Sms1,
				IndexMain: r.IndexMain, StationNach: r.StationNach,
				Naznach: r.Naznach, GruzpolS: r.GruzpolS, Sms1: r.Sms1,
			}
			subs[gk][sk] = sg
			subOrder[gk] = append(subOrder[gk], sk)
		}
		sg.VagonCount++
		sg.Vagons = append(sg.Vagons, ArrivalVagonDTO{
			ID: r.ID, Vagon: r.Vagon, Shipments: r.Shipments, Status: r.Status,
		})
	}

	out := make([]ArrivalGroupDTO, 0, len(order))
	for _, gk := range order {
		g := groups[gk]
		for _, sk := range subOrder[gk] {
			sg := subs[gk][sk]
			sg.Display = arrivalDisplay(sg)
			g.SubGroups = append(g.SubGroups, *sg)
		}
		sort.SliceStable(g.SubGroups, func(i, j int) bool { return g.SubGroups[i].Key < g.SubGroups[j].Key })
		out = append(out, *g)
	}
	return out
}

// arrivalDisplay — строка состава подгруппы в формате gtport:
// «(59)-783-Челутай АЭ»; переставленный чужой груз — «(8)-782-Челутай ГУТ-2 → АЭ»
// (грузополучатель → фактическое назначение).
func arrivalDisplay(sg *ArrivalSubgroupDTO) string {
	dest := sg.Naznach
	if sg.GruzpolS != "" && sg.Naznach != "" && sg.GruzpolS != sg.Naznach {
		dest = sg.GruzpolS + " → " + sg.Naznach
	} else if dest == "" {
		dest = sg.GruzpolS
	}
	parts := []string{fmt.Sprintf("(%d)", sg.VagonCount)}
	if mid := indexMiddle(sg.IndexMain); mid != "" {
		parts = append(parts, mid)
	}
	if sg.StationNach != "" {
		parts = append(parts, sg.StationNach)
	}
	res := strings.Join(parts, "-")
	if dest != "" {
		res += " " + dest
	}
	return res
}

// indexMiddle — средняя часть индекса поезда «9379-783-9857» → «783».
func indexMiddle(index string) string {
	parts := strings.Split(index, "-")
	if len(parts) == 3 {
		return parts[1]
	}
	return ""
}

// ltKey — стабильный ключ времени для группировки (nil-безопасно).
func ltKey(lt *domain.LocalTime) string {
	if lt == nil {
		return ""
	}
	return lt.String()
}
