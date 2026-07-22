package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// CargoWorkService — «Грузовая работа»: суточный учётный лист терминала
// (перенос gtport cargowork_repository.go).
//
// Разделение слоёв — главное отличие от эталона. В gtport сборка авто-данных
// была вшита в «создание записи» (isNewRecord), поэтому аналитика замерзала
// навсегда: пришли новые вехи истории — цифры суток уже не пересчитать. Здесь
// авто-слой пересобирается отдельной операцией (Recalc), а правки оператора
// живут своей жизнью и пересчётом не затираются.
type CargoWorkService struct {
	repo    port.CargoWorkRepository
	history port.HistoryRepository
	plans   port.PlanRepository
	dir     *DirectoryCache
	cfg     *ConfigCache
	journal *Journal
}

func NewCargoWorkService(repo port.CargoWorkRepository, history port.HistoryRepository,
	plans port.PlanRepository, dir *DirectoryCache, cfg *ConfigCache) *CargoWorkService {
	return &CargoWorkService{repo: repo, history: history, plans: plans, dir: dir, cfg: cfg}
}

// SetJournal подключает единый журнал событий (nil-safe).
func (s *CargoWorkService) SetJournal(j *Journal) { s.journal = j }

// ErrCargoWorkAccess — правка суток запрещена правилом дат.
var ErrCargoWorkAccess = fmt.Errorf("правка запрещена")

// checkCargoWorkAccess — доступ к ИЗМЕНЕНИЮ суток (решение владельца):
// administrator правит любые даты, остальные — только вчерашние ЖД-сутки
// (учётный лист закрывают на следующий день; в gtport было то же правило,
// только проверялось на клиенте разбором JWT).
//
// Чтение не ограничиваем: смотреть прошлые сутки может кто угодно.
func checkCargoWorkAccess(ctx context.Context, day time.Time) error {
	cl := auth.ClaimsFromContext(ctx)
	if cl == nil || cl.HasRole(auth.RoleAdministrator) {
		return nil
	}
	yesterday := clock.Now().Time().Truncate(24*time.Hour).AddDate(0, 0, -1)
	if !dayStart(day).Equal(yesterday) {
		return fmt.Errorf("%w: менять можно только учёт за вчера (%s) — "+
			"обратитесь к администратору", ErrCargoWorkAccess, yesterday.Format("2006-01-02"))
	}
	return nil
}

// CargoWorkLineDTO — колонка таблицы выгрузки: описание линии + её цифры.
type CargoWorkLineDTO struct {
	CargoKey string `json:"cargo_key"`
	Label    string `json:"label"`
	Pc       int    `json:"pc"` // способность, ваг/сут (0 — линия не настроена)

	Ost18           int    `json:"ost_18"`
	OstSt           int    `json:"ost_st"`
	Prib            int    `json:"prib"`
	UsefulFormation int    `json:"useful_formation"`
	TotalFormation  int    `json:"total_formation"`
	Downtime        string `json:"downtime"`

	Plan     int    `json:"plan"`
	VigrFact int    `json:"vigr_fact"`
	VigrStan int    `json:"vigr_stan"`
	Prim     string `json:"prim"`

	Ost       int `json:"ost"`
	Effectiv  int `json:"effectiv"`
	Perepokaz int `json:"perepokaz"`

	Analytics *CargoWorkAnalytics `json:"analytics,omitempty"`
}

// CargoWorkLoadDTO — строка таблицы погрузки (целиком ручная).
type CargoWorkLoadDTO struct {
	CargoKey string `json:"cargo_key"`
	Label    string `json:"label"`
	LoadFact int    `json:"load_fact"`
	Plan     int    `json:"plan"`
	Ost      int    `json:"ost"`
}

// CargoWorkDayDTO — учётный лист терминала за сутки.
type CargoWorkDayDTO struct {
	Date     string             `json:"date"` // yyyy-MM-dd, ЖД-сутки
	Terminal string             `json:"terminal"`
	Color    string             `json:"color"` // ports.color — шапка таблицы
	Lines    []CargoWorkLineDTO `json:"lines"`
	Load     []CargoWorkLoadDTO `json:"load"` // пусто → блока погрузки нет
}

// CargoWorkManual — правки оператора: только ручной слой, по линиям.
type CargoWorkManual struct {
	Lines map[string]CargoWorkManualLine `json:"lines"` // ключ — cargo_key
	Load  map[string]CargoWorkManualLoad `json:"load"`
}

type CargoWorkManualLine struct {
	Plan     *int    `json:"plan"`
	VigrFact *int    `json:"vigr_fact"`
	Prim     *string `json:"prim"`
}

type CargoWorkManualLoad struct {
	LoadFact *int `json:"load_fact"`
	Plan     *int `json:"plan"`
	Ost      *int `json:"ost"`
}

// Day — учётный лист за сутки. Листа ещё нет — собираем авто-слой на лету и
// показываем, но НЕ сохраняем (отход от gtport GetOrCreate, который писал в БД
// прямо на чтении: там открытие любой даты плодило пустые записи, а здесь это
// ещё и упиралось бы в права на правку). В БД лист попадает, когда его
// осознанно сохранили или пересчитали.
func (s *CargoWorkService) Day(ctx context.Context, day time.Time, terminal string) (CargoWorkDayDTO, error) {
	if _, ok := s.dir.PortByNameS(terminal); !ok {
		return CargoWorkDayDTO{}, fmt.Errorf("неизвестный терминал %q", terminal)
	}
	rows, loadRows, err := s.stored(ctx, day, terminal)
	if err != nil {
		return CargoWorkDayDTO{}, err
	}
	if len(rows) == 0 {
		if rows, err = s.rebuild(ctx, day, terminal, nil); err != nil {
			return CargoWorkDayDTO{}, err
		}
	}
	return s.assemble(ctx, day, terminal, rows, loadRows)
}

// Recalc пересобирает авто-слой суток, СОХРАНЯЯ ручные поля (план, факт
// выгрузки, комментарий) и пересчитывая производные. Кнопка «Пересчитать»:
// история дополняется (подтверждение кандидатов, sticky-10, «Обновить
// справочники»), и вчерашние цифры без этого остались бы устаревшими.
func (s *CargoWorkService) Recalc(ctx context.Context, day time.Time, terminal string) (CargoWorkDayDTO, error) {
	if _, ok := s.dir.PortByNameS(terminal); !ok {
		return CargoWorkDayDTO{}, fmt.Errorf("неизвестный терминал %q", terminal)
	}
	if err := checkCargoWorkAccess(ctx, day); err != nil {
		return CargoWorkDayDTO{}, err
	}
	existing, loadRows, err := s.stored(ctx, day, terminal)
	if err != nil {
		return CargoWorkDayDTO{}, err
	}
	rows, err := s.rebuild(ctx, day, terminal, existing)
	if err != nil {
		return CargoWorkDayDTO{}, err
	}
	if err := s.repo.UpsertRows(ctx, rows); err != nil {
		return CargoWorkDayDTO{}, fmt.Errorf("сохранение учёта: %w", err)
	}
	return s.assemble(ctx, day, terminal, rows, loadRows)
}

// Save применяет правки оператора: принимает ТОЛЬКО ручные поля, авто-слой
// берёт из сохранённой строки (фронт его прислать не может — незачем ему
// доверять), производные пересчитывает сервер.
func (s *CargoWorkService) Save(ctx context.Context, day time.Time, terminal string, manual CargoWorkManual) (CargoWorkDayDTO, error) {
	if _, ok := s.dir.PortByNameS(terminal); !ok {
		return CargoWorkDayDTO{}, fmt.Errorf("неизвестный терминал %q", terminal)
	}
	if err := checkCargoWorkAccess(ctx, day); err != nil {
		return CargoWorkDayDTO{}, err
	}
	rows, loadRows, err := s.stored(ctx, day, terminal)
	if err != nil {
		return CargoWorkDayDTO{}, err
	}
	if len(rows) == 0 {
		// Правка суток, которых ещё нет: сперва собираем авто-слой.
		if rows, err = s.rebuild(ctx, day, terminal, nil); err != nil {
			return CargoWorkDayDTO{}, err
		}
	}

	now := clock.Now()
	for i := range rows {
		m, ok := manual.Lines[rows[i].CargoKey]
		if !ok {
			continue
		}
		if m.Plan != nil {
			rows[i].Plan = *m.Plan
		}
		if m.VigrFact != nil {
			rows[i].VigrFact = *m.VigrFact
		}
		if m.Prim != nil {
			rows[i].Prim = *m.Prim
		}
		rows[i].Recalc()
		rows[i].UpdatedAt = &now
	}
	if err := s.repo.UpsertRows(ctx, rows); err != nil {
		return CargoWorkDayDTO{}, fmt.Errorf("сохранение учёта: %w", err)
	}

	if len(manual.Load) > 0 {
		loadRows = mergeCargoWorkLoad(loadRows, manual.Load, day, terminal, now)
		if err := s.repo.UpsertLoadRows(ctx, loadRows); err != nil {
			return CargoWorkDayDTO{}, fmt.Errorf("сохранение погрузки: %w", err)
		}
	}
	return s.assemble(ctx, day, terminal, rows, loadRows)
}

// Delete удаляет учёт суток по терминалу (выгрузку и погрузку разом).
func (s *CargoWorkService) Delete(ctx context.Context, day time.Time, terminal string) error {
	if _, ok := s.dir.PortByNameS(terminal); !ok {
		return fmt.Errorf("неизвестный терминал %q", terminal)
	}
	if err := checkCargoWorkAccess(ctx, day); err != nil {
		return err
	}
	return s.repo.DeleteDay(ctx, domain.LocalTime(dayStart(day)), terminal)
}

// stored — сохранённые строки суток.
func (s *CargoWorkService) stored(ctx context.Context, day time.Time, terminal string) ([]domain.CargoWorkRow, []domain.CargoWorkLoadRow, error) {
	d := domain.LocalTime(dayStart(day))
	rows, err := s.repo.Rows(ctx, d, d, terminal)
	if err != nil {
		return nil, nil, fmt.Errorf("чтение учёта: %w", err)
	}
	loadRows, err := s.repo.LoadRows(ctx, d, d, terminal)
	if err != nil {
		return nil, nil, fmt.Errorf("чтение погрузки: %w", err)
	}
	return rows, loadRows, nil
}

// rebuild — сборка авто-слоя суток по линиям терминала. Ручные поля берутся из
// existing (пересчёт их не трогает), производные считаются заново.
func (s *CargoWorkService) rebuild(ctx context.Context, day time.Time, terminal string, existing []domain.CargoWorkRow) ([]domain.CargoWorkRow, error) {
	lines, err := s.unloadLines(ctx, terminal)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("для терминала %q не заданы линии учёта выгрузки", terminal)
	}

	start := dayStart(day)
	d := domain.LocalTime(start)
	prev := domain.LocalTime(start.AddDate(0, 0, -1))

	// Прибытия суток: разом дают и счётчик prib, и состав поездов для аналитики.
	arrived, err := s.history.ArrivedRows(ctx, d, d, []string{terminal})
	if err != nil {
		return nil, fmt.Errorf("вехи прибытия: %w", err)
	}
	unloaded, err := s.history.DailyCargoUnloaded(ctx, d, d)
	if err != nil {
		return nil, fmt.Errorf("вехи выгрузки: %w", err)
	}
	prevRows, err := s.repo.Rows(ctx, prev, prev, terminal)
	if err != nil {
		return nil, fmt.Errorf("остатки предыдущих суток: %w", err)
	}
	ostSt, err := s.planRemainders(ctx, day, terminal)
	if err != nil {
		return nil, err
	}

	prevOst := map[string]int{}
	for _, r := range prevRows {
		prevOst[r.CargoKey] = r.Ost
	}
	manual := map[string]domain.CargoWorkRow{}
	for _, r := range existing {
		manual[r.CargoKey] = r
	}

	dayKey := start.Format("2006-01-02")
	cutoff := s.cutoffHour()
	now := clock.Now()

	out := make([]domain.CargoWorkRow, 0, len(lines))
	for _, ln := range lines {
		row := domain.CargoWorkRow{
			DateJd: d, Terminal: terminal, CargoKey: ln.CargoKey,
			Ost18:     prevOst[ln.CargoKey],
			OstSt:     ostSt[ln.CargoKey],
			VigrStan:  unloaded[dayKey+"|"+terminal+"|"+ln.CargoKey],
			CreatedAt: &now, UpdatedAt: &now,
		}
		if old, ok := manual[ln.CargoKey]; ok {
			row.ID = old.ID
			row.Plan, row.VigrFact, row.Prim = old.Plan, old.VigrFact, old.Prim
			row.CreatedAt = old.CreatedAt
		}

		trains, prib := cargoWorkTrains(arrived, ln.CargoKey)
		row.Prib = prib

		a := calcCargoWorkDay(start, row.Ost18, s.linePc(ln, terminal), cutoff, trains)
		row.UsefulFormation, row.TotalFormation, row.Downtime = a.UsefulFormation, a.TotalFormation, a.Downtime
		row.AnalyticsJSON = marshalCargoWork(a)
		row.TrainStructureJSON = marshalCargoWork(trains)

		row.Recalc()
		out = append(out, row)
	}
	return out, nil
}

// unloadLines — линии выгрузки терминала в порядке справочника.
func (s *CargoWorkService) unloadLines(ctx context.Context, terminal string) ([]domain.PortCargoLine, error) {
	all, err := s.repo.Lines(ctx)
	if err != nil {
		return nil, fmt.Errorf("справочник линий: %w", err)
	}
	out := make([]domain.PortCargoLine, 0, 4)
	for _, ln := range all {
		if ln.Terminal == terminal && ln.Kind == domain.CargoLineUnload {
			out = append(out, ln)
		}
	}
	return out, nil
}

// loadLines — линии погрузки терминала (пусто → блока погрузки у него нет).
func (s *CargoWorkService) loadLines(ctx context.Context, terminal string) ([]domain.PortCargoLine, error) {
	all, err := s.repo.Lines(ctx)
	if err != nil {
		return nil, fmt.Errorf("справочник линий: %w", err)
	}
	out := make([]domain.PortCargoLine, 0, 8)
	for _, ln := range all {
		if ln.Terminal == terminal && ln.Kind == domain.CargoLineLoad {
			out = append(out, ln)
		}
	}
	return out, nil
}

// linePc — способность линии: из справочника, иначе из ports по роду груза
// (та же цифра, что у интервалов Stage 4).
func (s *CargoWorkService) linePc(ln domain.PortCargoLine, terminal string) int {
	if ln.Pc != nil {
		return *ln.Pc
	}
	p, ok := s.dir.PortByNameS(terminal)
	if !ok {
		return 0
	}
	if ln.CargoKey == "" {
		if p.PcTotal != nil {
			return *p.PcTotal
		}
		return 0
	}
	return pcForRod(p, ln.CargoKey)
}

// cutoffHour — час начала ЖД-суток из настроек источника (0 → движок возьмёт 18).
func (s *CargoWorkService) cutoffHour() int {
	if s.cfg == nil {
		return 0
	}
	if ds, ok := s.cfg.DataSource("lk"); ok {
		return ds.Config.DateCutoffHour
	}
	return 0
}

// planRemainders — «Остаток на 18:00» по линиям терминала: берём план станции
// (ports.plan_code) за эти сутки и разбираем служебную строку плана по меткам
// колонок (port_cargo_line.plan_label). Нет плана/метки — остаток 0, это не
// ошибка: план мог быть не загружен.
func (s *CargoWorkService) planRemainders(ctx context.Context, day time.Time, terminal string) (map[string]int, error) {
	out := map[string]int{}
	p, ok := s.dir.PortByNameS(terminal)
	if !ok || p.PlanCode == "" || s.plans == nil {
		return out, nil
	}
	lines, err := s.unloadLines(ctx, terminal)
	if err != nil {
		return nil, err
	}
	byLabel := map[string]string{} // метка колонки → cargo_key
	for _, ln := range lines {
		if ln.PlanLabel != "" {
			byLabel[strings.TrimSpace(ln.PlanLabel)] = ln.CargoKey
		}
	}
	if len(byLabel) == 0 {
		return out, nil
	}

	nitki, err := s.planForDay(ctx, p.PlanCode, day)
	if err != nil || len(nitki) == 0 {
		return out, err
	}
	for _, n := range nitki {
		if !n.IsOstatok {
			continue
		}
		for _, cell := range n.Ports {
			if key, ok := byLabel[strings.TrimSpace(cell.Label)]; ok {
				out[key] = cell.Count
			}
		}
		break // служебная строка одна
	}
	return out, nil
}

// planForDay — нитки плана станции ЗА ЭТИ сутки; если плана на дату нет, берём
// самый свежий (диспетчеру лучше прошлый остаток, чем пустая клетка).
func (s *CargoWorkService) planForDay(ctx context.Context, planCode string, day time.Time) ([]domain.PlanNitka, error) {
	want := dayStart(day)
	plans, err := s.plans.ListPlans(ctx, planCode)
	if err != nil {
		return nil, fmt.Errorf("загрузки плана %s: %w", planCode, err)
	}
	for _, pl := range plans {
		if pl.PlanDate == nil {
			continue
		}
		if dayStart(pl.PlanDate.Time()).Equal(want) {
			_, nitki, err := s.plans.GetPlanByID(ctx, pl.ID)
			if err != nil {
				return nil, fmt.Errorf("план %d: %w", pl.ID, err)
			}
			return nitki, nil
		}
	}
	_, nitki, err := s.plans.GetLatestPlan(ctx, planCode)
	if err != nil {
		return nil, fmt.Errorf("свежий план %s: %w", planCode, err)
	}
	return nitki, nil
}

// assemble — сборка ответа: строки учёта + справочные подписи линий + цвет
// терминала (ports.color) + строки погрузки (даже если их ещё не заполняли).
func (s *CargoWorkService) assemble(ctx context.Context, day time.Time, terminal string,
	rows []domain.CargoWorkRow, loadRows []domain.CargoWorkLoadRow) (CargoWorkDayDTO, error) {

	lines, err := s.unloadLines(ctx, terminal)
	if err != nil {
		return CargoWorkDayDTO{}, err
	}
	byKey := map[string]domain.CargoWorkRow{}
	for _, r := range rows {
		byKey[r.CargoKey] = r
	}

	out := CargoWorkDayDTO{
		Date:     dayStart(day).Format("2006-01-02"),
		Terminal: terminal,
		Lines:    make([]CargoWorkLineDTO, 0, len(lines)),
		Load:     []CargoWorkLoadDTO{},
	}
	if p, ok := s.dir.PortByNameS(terminal); ok {
		out.Color = p.Color
	}

	for _, ln := range lines {
		r := byKey[ln.CargoKey]
		dto := CargoWorkLineDTO{
			CargoKey: ln.CargoKey, Label: ln.Label, Pc: s.linePc(ln, terminal),
			Ost18: r.Ost18, OstSt: r.OstSt, Prib: r.Prib,
			UsefulFormation: r.UsefulFormation, TotalFormation: r.TotalFormation,
			Downtime: r.Downtime,
			Plan:     r.Plan, VigrFact: r.VigrFact, VigrStan: r.VigrStan, Prim: r.Prim,
			Ost: r.Ost, Effectiv: r.Effectiv, Perepokaz: r.Perepokaz,
		}
		if r.AnalyticsJSON != "" {
			var a CargoWorkAnalytics
			if json.Unmarshal([]byte(r.AnalyticsJSON), &a) == nil {
				dto.Analytics = &a
			}
		}
		out.Lines = append(out.Lines, dto)
	}

	loads, err := s.loadLines(ctx, terminal)
	if err != nil {
		return CargoWorkDayDTO{}, err
	}
	byLoad := map[string]domain.CargoWorkLoadRow{}
	for _, r := range loadRows {
		byLoad[r.CargoKey] = r
	}
	for _, ln := range loads {
		r := byLoad[ln.CargoKey]
		out.Load = append(out.Load, CargoWorkLoadDTO{
			CargoKey: ln.CargoKey, Label: ln.Label,
			LoadFact: r.LoadFact, Plan: r.Plan, Ost: r.Ost,
		})
	}
	return out, nil
}

// cargoWorkTrains — поезда линии из вех прибытия: группировка по индексу
// поезда, время — самое раннее прибытие группы. Отбор по группе груза; пустой
// ключ линии означает «терминал без разбивки» — берём все вагоны.
func cargoWorkTrains(rows []domain.VagonHistory, cargoKey string) ([]CargoWorkTrain, int) {
	type agg struct {
		wagons  int
		arrival *domain.LocalTime
	}
	byIndex := map[string]*agg{}
	total := 0

	for i := range rows {
		r := &rows[i]
		if cargoKey != "" && r.CargoGroup != cargoKey {
			continue
		}
		total++
		if r.DatePrib == nil || r.DatePrib.IsZero() {
			continue // без времени прибытия поезд в расчёт суток не поставить
		}
		a, ok := byIndex[r.IndexPp]
		if !ok {
			a = &agg{}
			byIndex[r.IndexPp] = a
		}
		a.wagons++
		if a.arrival == nil || r.DatePrib.Time().Before(a.arrival.Time()) {
			a.arrival = r.DatePrib
		}
	}

	out := make([]CargoWorkTrain, 0, len(byIndex))
	for index, a := range byIndex {
		if a.arrival == nil {
			continue
		}
		out = append(out, CargoWorkTrain{Name: index, Wagons: a.wagons, Arrival: *a.arrival})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Arrival.Time().Equal(out[j].Arrival.Time()) {
			return out[i].Arrival.Time().Before(out[j].Arrival.Time())
		}
		return out[i].Name < out[j].Name
	})
	return out, total
}

// mergeCargoWorkLoad накладывает правки погрузки на сохранённые строки.
func mergeCargoWorkLoad(rows []domain.CargoWorkLoadRow, manual map[string]CargoWorkManualLoad,
	day time.Time, terminal string, now domain.LocalTime) []domain.CargoWorkLoadRow {

	byKey := map[string]*domain.CargoWorkLoadRow{}
	for i := range rows {
		byKey[rows[i].CargoKey] = &rows[i]
	}
	for key, m := range manual {
		r, ok := byKey[key]
		if !ok {
			rows = append(rows, domain.CargoWorkLoadRow{
				DateJd: domain.LocalTime(dayStart(day)), Terminal: terminal, CargoKey: key,
				CreatedAt: &now,
			})
			r = &rows[len(rows)-1]
			byKey[key] = r
		}
		if m.LoadFact != nil {
			r.LoadFact = *m.LoadFact
		}
		if m.Plan != nil {
			r.Plan = *m.Plan
		}
		if m.Ost != nil {
			r.Ost = *m.Ost
		}
		r.UpdatedAt = &now
	}
	return rows
}

// dayStart — дата без времени (ключ суток).
func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// marshalCargoWork — снимок в jsonb-колонку; ошибка маршалинга не должна ронять
// сутки (снимок вспомогательный, цифры уже посчитаны).
func marshalCargoWork(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
