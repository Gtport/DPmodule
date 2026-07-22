package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// Тесты сборки суточного учётного листа. Проверяют то, ради чего слои были
// разделены: пересчёт авто-данных НЕ трогает то, что вбил диспетчер.

// ── стабы ───────────────────────────────────────────────────────────────────

type cwDir struct{ ports []domain.Ports }

func (cwDir) LoadStations(context.Context) ([]domain.Station, error)               { return nil, nil }
func (cwDir) LoadCargoOperations(context.Context) ([]domain.CargoOperation, error) { return nil, nil }
func (cwDir) LoadCargo(context.Context) ([]domain.Cargo, error)                    { return nil, nil }
func (cwDir) LoadMarka(context.Context) ([]domain.Marka, error)                    { return nil, nil }
func (d cwDir) LoadPorts(context.Context) ([]domain.Ports, error)                  { return d.ports, nil }
func (cwDir) LoadRouteSpeed(context.Context) ([]domain.RouteSpeed, error)          { return nil, nil }
func (cwDir) LoadNaznachStation(context.Context) ([]domain.NaznachStation, error)  { return nil, nil }
func (cwDir) UpdateNaznachStationNaznach(context.Context, string, string, string) error {
	return nil
}

// cwRepo — in-memory port.CargoWorkRepository.
type cwRepo struct {
	lines []domain.PortCargoLine
	rows  map[string]domain.CargoWorkRow // ключ date|terminal|cargo_key
	loads map[string]domain.CargoWorkLoadRow
}

func newCwRepo(lines []domain.PortCargoLine) *cwRepo {
	return &cwRepo{
		lines: lines,
		rows:  map[string]domain.CargoWorkRow{},
		loads: map[string]domain.CargoWorkLoadRow{},
	}
}

func cwRowKey(d domain.LocalTime, terminal, key string) string {
	return d.String()[:10] + "|" + terminal + "|" + key
}

func (r *cwRepo) Lines(context.Context) ([]domain.PortCargoLine, error) { return r.lines, nil }

func (r *cwRepo) Rows(_ context.Context, from, to domain.LocalTime, terminal string) ([]domain.CargoWorkRow, error) {
	var out []domain.CargoWorkRow
	for _, row := range r.rows {
		if terminal != "" && row.Terminal != terminal {
			continue
		}
		d := row.DateJd.Time()
		if d.Before(from.Time()) || d.After(to.Time()) {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func (r *cwRepo) LoadRows(_ context.Context, from, to domain.LocalTime, terminal string) ([]domain.CargoWorkLoadRow, error) {
	var out []domain.CargoWorkLoadRow
	for _, row := range r.loads {
		if terminal != "" && row.Terminal != terminal {
			continue
		}
		d := row.DateJd.Time()
		if d.Before(from.Time()) || d.After(to.Time()) {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func (r *cwRepo) UpsertRows(_ context.Context, rows []domain.CargoWorkRow) error {
	for _, row := range rows {
		r.rows[cwRowKey(row.DateJd, row.Terminal, row.CargoKey)] = row
	}
	return nil
}

func (r *cwRepo) UpsertLoadRows(_ context.Context, rows []domain.CargoWorkLoadRow) error {
	for _, row := range rows {
		r.loads[cwRowKey(row.DateJd, row.Terminal, row.CargoKey)] = row
	}
	return nil
}

func (r *cwRepo) DeleteDay(_ context.Context, day domain.LocalTime, terminal string) error {
	for k, row := range r.rows {
		if row.Terminal == terminal && row.DateJd.String()[:10] == day.String()[:10] {
			delete(r.rows, k)
		}
	}
	for k, row := range r.loads {
		if row.Terminal == terminal && row.DateJd.String()[:10] == day.String()[:10] {
			delete(r.loads, k)
		}
	}
	return nil
}

// cwHist — стаб истории: вехи прибытия и счётчики выгрузки.
type cwHist struct {
	arrived  []domain.VagonHistory
	unloaded map[string]int
}

func (cwHist) ExistingIDs(context.Context, []string) (map[string]struct{}, error) { return nil, nil }
func (cwHist) Insert(context.Context, []domain.VagonHistory) error                { return nil }
func (cwHist) UpdateFields(context.Context, string, map[string]any) error         { return nil }
func (cwHist) RowsByIDs(context.Context, []string) ([]domain.VagonHistory, error) { return nil, nil }
func (cwHist) UpdateFieldsBatch(context.Context, map[string]map[string]any) error { return nil }
func (cwHist) DailyTerminalCounts(context.Context, domain.LocalTime, domain.LocalTime) (map[string]int, map[string]int, error) {
	return nil, nil, nil
}
func (h cwHist) ArrivedRows(_ context.Context, _, _ domain.LocalTime, _ []string) ([]domain.VagonHistory, error) {
	return h.arrived, nil
}
func (h cwHist) DailyCargoUnloaded(context.Context, domain.LocalTime, domain.LocalTime) (map[string]int, error) {
	return h.unloaded, nil
}

// cwPlans — стаб плана подвода без загрузок (остаток на станции = 0).
type cwPlans struct{}

func (cwPlans) SavePlan(context.Context, domain.Plan, []domain.PlanNitka) (int64, error) {
	return 0, nil
}
func (cwPlans) ListPlans(context.Context, string) ([]domain.PlanSummary, error) { return nil, nil }
func (cwPlans) GetLatestPlan(context.Context, string) (domain.Plan, []domain.PlanNitka, error) {
	return domain.Plan{}, nil, nil
}
func (cwPlans) GetPlanByID(context.Context, int64) (domain.Plan, []domain.PlanNitka, error) {
	return domain.Plan{}, nil, nil
}
func (cwPlans) ListSF(context.Context) ([]domain.SFRecord, error) { return nil, nil }

// ── помощники ───────────────────────────────────────────────────────────────

func cwPtr(v int) *int { return &v }

func cwLt(y int, m time.Month, d, h, min int) *domain.LocalTime {
	lt := domain.LocalTime(time.Date(y, m, d, h, min, 0, 0, time.UTC))
	return &lt
}

// cwVagon — веха прибытия: поезд, группа груза, время (date_prib, ЖД-штамп).
func cwVagon(index, group string, h, min int) domain.VagonHistory {
	return domain.VagonHistory{
		IndexPp: index, Naznach: "ГУТ-2", CargoGroup: group,
		DatePrib: cwLt(2026, time.July, 20, h, min), DatePribD: cwLt(2026, time.July, 20, 0, 0),
	}
}

// cwService — сервис на стабах: терминал ГУТ-2 с разбивкой уголь/металл.
func cwService(t *testing.T, repo *cwRepo, hist cwHist) *CargoWorkService {
	t.Helper()
	ctx := context.Background()
	dir := NewDirectoryCache(cwDir{ports: []domain.Ports{
		{NameS: "ГУТ-2", StationCode: "985702", Color: "#DDEBF7",
			PcCoal: cwPtr(170), PcMetal: cwPtr(90), Enabled: true},
	}})
	require.NoError(t, dir.Load(ctx))
	return NewCargoWorkService(repo, hist, cwPlans{}, dir, nil)
}

func cwLines() []domain.PortCargoLine {
	return []domain.PortCargoLine{
		{Terminal: "ГУТ-2", Kind: domain.CargoLineUnload, CargoKey: "УГОЛЬ", Label: "Уголь", Pc: cwPtr(170), SortOrder: 10, Enabled: true},
		{Terminal: "ГУТ-2", Kind: domain.CargoLineUnload, CargoKey: "МЕТАЛЛ", Label: "Металл", Pc: cwPtr(90), SortOrder: 20, Enabled: true},
		{Terminal: "ГУТ-2", Kind: domain.CargoLineLoad, CargoKey: "GLIN", Label: "Глинозем", SortOrder: 10, Enabled: true},
	}
}

func cwDate() time.Time { return time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC) }

// ── тесты ───────────────────────────────────────────────────────────────────

// Сутки собираются по линиям справочника: у терминала с разбивкой — колонка на
// род груза, вагоны разносятся по cargo_group, погрузка идёт отдельным блоком.
func TestCargoWorkDay_BuildsLinesFromRegistry(t *testing.T) {
	repo := newCwRepo(cwLines())
	svc := cwService(t, repo, cwHist{
		arrived: []domain.VagonHistory{
			cwVagon("1234-567-8901", "УГОЛЬ", 19, 22),
			cwVagon("1234-567-8901", "УГОЛЬ", 19, 22),
			cwVagon("2234-567-8902", "МЕТАЛЛ", 8, 15),
		},
		unloaded: map[string]int{"2026-07-20|ГУТ-2|УГОЛЬ": 5},
	})

	got, err := svc.Day(context.Background(), cwDate(), "ГУТ-2")
	require.NoError(t, err)

	require.Len(t, got.Lines, 2, "колонки — линии выгрузки из справочника")
	require.Equal(t, "#DDEBF7", got.Color, "цвет терминала берётся из ports")
	require.Len(t, got.Load, 1, "погрузка — отдельные строки справочника")

	coal, metal := got.Lines[0], got.Lines[1]
	require.Equal(t, "УГОЛЬ", coal.CargoKey)
	require.Equal(t, 2, coal.Prib, "прибыло считается по группе груза")
	require.Equal(t, 5, coal.VigrStan, "выгрузка по станции — из вех истории")
	require.Equal(t, 1, metal.Prib)
	require.Equal(t, 0, metal.VigrStan)
}

// Пересчёт обновляет авто-слой и НЕ трогает ручной: это причина, по которой
// слои разделены (в gtport аналитика замерзала при создании записи).
func TestCargoWorkRecalc_KeepsManualFields(t *testing.T) {
	ctx := context.Background()
	repo := newCwRepo(cwLines())
	hist := cwHist{arrived: []domain.VagonHistory{cwVagon("1234-567-8901", "УГОЛЬ", 10, 0)}}
	svc := cwService(t, repo, hist)

	_, err := svc.Day(ctx, cwDate(), "ГУТ-2")
	require.NoError(t, err)

	// Диспетчер вбил план, факт и комментарий.
	_, err = svc.Save(ctx, cwDate(), "ГУТ-2", CargoWorkManual{
		Lines: map[string]CargoWorkManualLine{
			"УГОЛЬ": {Plan: cwPtr(100), VigrFact: cwPtr(80), Prim: cwStr("вручную")},
		},
	})
	require.NoError(t, err)

	// История дополнилась — пересчёт должен подхватить прибытие, но не стереть правки.
	hist.arrived = append(hist.arrived, cwVagon("1234-567-8901", "УГОЛЬ", 10, 0))
	svc = cwService(t, repo, hist)

	got, err := svc.Recalc(ctx, cwDate(), "ГУТ-2")
	require.NoError(t, err)

	coal := got.Lines[0]
	require.Equal(t, 2, coal.Prib, "авто-слой пересобран")
	require.Equal(t, 100, coal.Plan, "план оператора сохранён")
	require.Equal(t, 80, coal.VigrFact, "факт оператора сохранён")
	require.Equal(t, "вручную", coal.Prim, "комментарий оператора сохранён")
}

// Производные поля считает сервер, а не фронт: остаток, перепоказ и
// эффективность выводятся из авто- и ручного слоёв.
func TestCargoWorkSave_ServerComputesDerived(t *testing.T) {
	ctx := context.Background()
	repo := newCwRepo(cwLines())
	svc := cwService(t, repo, cwHist{
		arrived:  []domain.VagonHistory{cwVagon("П", "УГОЛЬ", 10, 0)},
		unloaded: map[string]int{"2026-07-20|ГУТ-2|УГОЛЬ": 12},
	})

	got, err := svc.Save(ctx, cwDate(), "ГУТ-2", CargoWorkManual{
		Lines: map[string]CargoWorkManualLine{"УГОЛЬ": {VigrFact: cwPtr(10)}},
	})
	require.NoError(t, err)

	coal := got.Lines[0]
	// ost = ost_18(0) + prib(1) − vigr_fact(10) = −9; перепоказ = 12 − 10 = 2.
	require.Equal(t, -9, coal.Ost, "остаток = ост18 + прибыло − выгрузка")
	require.Equal(t, 2, coal.Perepokaz, "перепоказ = станция − факт порта")
	require.Equal(t, coal.VigrFact*100/max1(coal.UsefulFormation), coal.Effectiv,
		"эффективность = факт / полезное образование")
}

// Остаток на начало суток берётся из остатка ПРЕДЫДУЩИХ суток той же линии
// (перенос carry-over gtport getPreviousOst).
func TestCargoWorkRebuild_CarriesPreviousRemainder(t *testing.T) {
	ctx := context.Background()
	repo := newCwRepo(cwLines())
	prev := domain.LocalTime(cwDate().AddDate(0, 0, -1))
	repo.rows[cwRowKey(prev, "ГУТ-2", "УГОЛЬ")] = domain.CargoWorkRow{
		DateJd: prev, Terminal: "ГУТ-2", CargoKey: "УГОЛЬ", Ost: 37,
	}
	svc := cwService(t, repo, cwHist{})

	got, err := svc.Day(ctx, cwDate(), "ГУТ-2")
	require.NoError(t, err)
	require.Equal(t, 37, got.Lines[0].Ost18, "остаток вчерашних суток переносится на сегодня")
}

// Терминал без разбивки (пустой cargo_key) считает ВСЕ вагоны одной строкой —
// так «у АЭ одна колонка, у ГУТ-2 три» перестаёт быть кодом.
func TestCargoWorkTrains_NoSplitTakesAll(t *testing.T) {
	rows := []domain.VagonHistory{
		cwVagon("A", "УГОЛЬ", 10, 0),
		cwVagon("A", "МЕТАЛЛ", 10, 0),
		cwVagon("B", "", 12, 0),
	}
	trains, prib := cargoWorkTrains(rows, "")
	require.Equal(t, 3, prib, "без разбивки считаются все вагоны")
	require.Len(t, trains, 2, "вагоны сгруппированы в поезда по индексу")
	require.Equal(t, "A", trains[0].Name)
	require.Equal(t, 2, trains[0].Wagons)
}

// Отбор по группе груза и время поезда — самое раннее прибытие его вагонов.
func TestCargoWorkTrains_FiltersAndTakesEarliest(t *testing.T) {
	rows := []domain.VagonHistory{
		cwVagon("A", "УГОЛЬ", 14, 0),
		cwVagon("A", "УГОЛЬ", 9, 30),
		cwVagon("A", "МЕТАЛЛ", 3, 0),
	}
	trains, prib := cargoWorkTrains(rows, "УГОЛЬ")
	require.Equal(t, 2, prib, "металл в угольную линию не попадает")
	require.Len(t, trains, 1)
	require.Equal(t, 2, trains[0].Wagons)
	require.Equal(t, 9, trains[0].Arrival.Time().Hour(), "время поезда — первый прибывший вагон")
}

// Права (решение владельца): оператор меняет только вчерашние сутки, админ —
// любые. Чтение не ограничено никому.
func TestCargoWorkAccess_OperatorOnlyYesterday(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC))
	defer restore()

	yesterday := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	old := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	operator := auth.WithClaims(context.Background(), &auth.Claims{Roles: []auth.Role{"dispatcher"}})
	admin := auth.WithClaims(context.Background(), &auth.Claims{Roles: []auth.Role{auth.RoleAdministrator}})

	require.NoError(t, checkCargoWorkAccess(operator, yesterday), "вчера оператору можно")
	require.ErrorIs(t, checkCargoWorkAccess(operator, old), ErrCargoWorkAccess,
		"старые сутки оператору нельзя")
	require.NoError(t, checkCargoWorkAccess(admin, old), "админу можно любые сутки")

	// Чтение старых суток оператором проходит — проверка только на изменении.
	repo := newCwRepo(cwLines())
	svc := cwService(t, repo, cwHist{})
	_, err := svc.Day(operator, old, "ГУТ-2")
	require.NoError(t, err, "смотреть прошлые сутки может кто угодно")

	_, err = svc.Save(operator, old, "ГУТ-2", CargoWorkManual{})
	require.ErrorIs(t, err, ErrCargoWorkAccess, "править прошлые сутки оператору нельзя")
}

// Терминал БЕЗ разбивки (пустой cargo_key) должен собирать выгрузку по всем
// группам груза — в истории у вагона стоит реальная группа («УГОЛЬ»), пустой
// ключ есть только у линии. Прямое обращение по пустому ключу давало ноль:
// у АЭ и УТ-1 «Выгрузка станция» всегда была 0, у ГУТ-2 (с разбивкой) — верной.
func TestCargoWorkUnloaded_NoSplitSumsAllGroups(t *testing.T) {
	counts := map[string]int{
		"2026-07-20|АЭ|УГОЛЬ":    125,
		"2026-07-20|АЭ|МЕТАЛЛ":   5,
		"2026-07-20|АЭ|":         2, // вагоны без группы груза
		"2026-07-20|УТ-1|УГОЛЬ":  161,
		"2026-07-20|ГУТ-2|УГОЛЬ": 63,
	}

	require.Equal(t, 132, cargoWorkUnloaded(counts, "2026-07-20", "АЭ", ""),
		"линия без разбивки собирает все группы своего терминала")
	require.Equal(t, 63, cargoWorkUnloaded(counts, "2026-07-20", "ГУТ-2", "УГОЛЬ"),
		"линия с разбивкой берёт только свою группу")
	require.Equal(t, 0, cargoWorkUnloaded(counts, "2026-07-20", "ГУТ-2", "ЧУГУН"),
		"группы не было — ноль")
	require.Equal(t, 0, cargoWorkUnloaded(counts, "2026-07-19", "АЭ", ""),
		"чужие сутки не подмешиваются")
}

// Терминал без разбивки: прибытие и выгрузка считаются по одному правилу —
// обе цифры собирают все группы груза (раньше расходились).
func TestCargoWorkDay_NoSplitTerminalCountsBoth(t *testing.T) {
	repo := newCwRepo([]domain.PortCargoLine{
		{Terminal: "ГУТ-2", Kind: domain.CargoLineUnload, CargoKey: "", Label: "Уголь",
			Pc: cwPtr(144), SortOrder: 10, Enabled: true},
	})
	svc := cwService(t, repo, cwHist{
		arrived: []domain.VagonHistory{
			cwVagon("A", "УГОЛЬ", 10, 0),
			cwVagon("A", "МЕТАЛЛ", 10, 0),
		},
		unloaded: map[string]int{
			"2026-07-20|ГУТ-2|УГОЛЬ":  7,
			"2026-07-20|ГУТ-2|МЕТАЛЛ": 3,
		},
	})

	got, err := svc.Day(context.Background(), cwDate(), "ГУТ-2")
	require.NoError(t, err)
	require.Len(t, got.Lines, 1)
	require.Equal(t, 2, got.Lines[0].Prib, "прибыло — все группы")
	require.Equal(t, 10, got.Lines[0].VigrStan, "выгружено — тоже все группы")
}

func cwStr(s string) *string { return &s }

func max1(v int) int {
	if v <= 0 {
		return 1
	}
	return v
}
