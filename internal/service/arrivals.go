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
type ArrivalsService struct {
	repo port.HistoryRepository
	dir  *DirectoryCache
}

func NewArrivalsService(repo port.HistoryRepository, dir *DirectoryCache) *ArrivalsService {
	return &ArrivalsService{repo: repo, dir: dir}
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
type ArrivalsUpdateRequest struct {
	VagonIDs []string `json:"vagon_ids"`
	// «Отменить прибытие»: сброс статуса/факта/отклонения — вагон снова «в пути».
	// Меняет ТОЛЬКО историю (как в gtport), снимок дислокации не трогается.
	ClearArrival bool `json:"clear_arrival,omitempty"`
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
	if !req.ClearArrival && req.IndexPp == "" && req.PlanJd == nil && req.DatePrib == nil &&
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
	updates := make(map[string]map[string]any, len(rows))
	for i := range rows {
		updates[rows[i].ID] = arrivalUpdateFields(&rows[i], req, now)
	}
	if err := s.repo.UpdateFieldsBatch(ctx, updates); err != nil {
		return ArrivalsUpdateResult{}, err
	}
	return ArrivalsUpdateResult{Updated: len(rows), Selected: len(req.VagonIDs)}, nil
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
	if req.ClearArrival {
		fields["status"] = nil
		fields["date_prib"] = nil
		fields["date_prib_d"] = nil
		fields["otkl"] = ""
		fields["delay"] = nil
		fields["date_dostav"] = nil
		return fields
	}
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
		fields["date_vigr_d"] = dateOnly(req.DateVigr)
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
