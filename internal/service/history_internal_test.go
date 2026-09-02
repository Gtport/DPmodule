package service

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

type histStubRepo struct {
	existing map[string]struct{}
	trips    map[int64]string // trip_key → id строки (как уникальный индекс в БД)
	inserted []domain.VagonHistory
	updates  map[string]map[string]any
	rows     map[string]domain.VagonHistory // для RowsByIDs
	batch    map[string]map[string]any      // записи UpdateFieldsBatch

	notUnloaded map[string]int // ответ NotUnloadedCounts («Оперативка»)
}

func newHistStub(existing ...string) *histStubRepo {
	e := map[string]struct{}{}
	for _, id := range existing {
		e[id] = struct{}{}
	}
	return &histStubRepo{existing: e, updates: map[string]map[string]any{}, trips: map[int64]string{}}
}
func (r *histStubRepo) ExistingIDs(_ context.Context, ids []string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	for _, id := range ids {
		if _, ok := r.existing[id]; ok {
			out[id] = struct{}{}
		}
	}
	return out, nil
}

// ExistingTrips — как в бою: рейс опознаётся по trip_key (вагон + дата начала),
// а не по id строки. Ключи берутся из колонок строки, поэтому в заглушке они
// живут отдельной картой: id может быть каким угодно, в том числе временным.
func (r *histStubRepo) ExistingTrips(_ context.Context, keys []int64) (map[int64]string, error) {
	byKey := map[int64]string{}
	for id := range r.existing {
		if k, ok := tripKeyFromTestID(id); ok {
			byKey[k] = id
		}
	}
	for k, id := range r.trips {
		byKey[k] = id
	}
	out := map[int64]string{}
	for _, k := range keys {
		if id, ok := byKey[k]; ok {
			out[k] = id
		}
	}
	return out, nil
}

func (r *histStubRepo) Insert(_ context.Context, rows []domain.VagonHistory) error {
	r.inserted = append(r.inserted, rows...)
	return nil
}
func (r *histStubRepo) UpdateFields(_ context.Context, id string, f map[string]any) error {
	r.updates[id] = f
	return nil
}

func (r *histStubRepo) ArrivedRows(_ context.Context, _, _ domain.LocalTime, _ []string) ([]domain.VagonHistory, error) {
	return nil, nil
}

func TestBuildHistoryRow(t *testing.T) {
	now := *ltm(2026, 7, 2, 6, 0)

	t.Run("в пути (2) — без вех прибытия/выгрузки", func(t *testing.T) {
		r := &domain.Dislocation{ID: "A", Vagon: "1", Status: ip(2), Invoice: "i", InvoiceMain: "im", CargoS: "уголь"}
		h := buildHistoryRow(r, now, 18)
		assert.Equal(t, "A", h.ID)
		assert.Equal(t, "im", h.InvoiceMain)
		assert.Nil(t, h.DatePrib)
		assert.Nil(t, h.DateVigr)
	})

	t.Run("прибыл (10) — поля прибытия из потока, не из date_kon", func(t *testing.T) {
		r := &domain.Dislocation{ID: "A", Vagon: "1", Status: ip(10),
			DatePrib:   ltm(2026, 7, 2, 10, 0),  // момент прибытия от провайдера
			DateKon:    ltm(2026, 7, 3, 21, 40), // время последней операции — НЕ должно попасть
			DateDostav: ld(2026, 7, 1)}
		h := buildHistoryRow(r, now, 18)
		require.NotNil(t, h.DatePrib)
		assert.Equal(t, "2026-07-02T10:00:00", h.DatePrib.String())
		require.NotNil(t, h.DatePribD)
		assert.Equal(t, "2026-07-02T00:00:00", h.DatePribD.String())
		require.NotNil(t, h.Delay)
		assert.Equal(t, 1, *h.Delay) // прибыл 2-го, срок 1-го → 1 сутки
		assert.Empty(t, h.Otkl)      // без плана
	})

	// Инвариант хранения: date_prib в истории — ЖД-штамп. Поток отдаёт сырой МСК,
	// поэтому час ≥ отсечки уводит вехy в следующие ЖД-сутки (как было у date_op_jd).
	t.Run("прибыл (10) — час ≥ отсечки → ЖД-сутки вперёд", func(t *testing.T) {
		r := &domain.Dislocation{ID: "A", Vagon: "1", Status: ip(10),
			DatePrib: ltm(2026, 7, 2, 19, 26)}
		h := buildHistoryRow(r, now, 18)
		require.NotNil(t, h.DatePrib)
		assert.Equal(t, "2026-07-03T19:26:00", h.DatePrib.String())
		assert.Equal(t, "2026-07-03T00:00:00", h.DatePribD.String())
	})

	// Страховка: статус 10 без даты прибытия невозможен по построению
	// (computeStatus), но веху терять нельзя — падаем на прежний date_kon.
	t.Run("прибыл (10) без даты потока — фолбэк на date_kon", func(t *testing.T) {
		r := &domain.Dislocation{ID: "A", Vagon: "1", Status: ip(10),
			DateKon: ltm(2026, 7, 2, 10, 0)}
		h := buildHistoryRow(r, now, 18)
		require.NotNil(t, h.DatePrib)
		assert.Equal(t, "2026-07-02T10:00:00", h.DatePrib.String())
	})

	t.Run("выгружен в порту (12) — поля выгрузки", func(t *testing.T) {
		r := &domain.Dislocation{ID: "A", Vagon: "1", Status: ip(12),
			TimeOp: ltm(2026, 7, 2, 9, 0), DateOpJd: ltm(2026, 7, 2, 9, 0), Naznach: "ГУТ-2"}
		h := buildHistoryRow(r, now, 18)
		require.NotNil(t, h.DateVigr)
		require.NotNil(t, h.DateVigrD)
		assert.Equal(t, "ГУТ-2", h.PlaceVigr)
	})
}

func TestHistoryUpdateFields(t *testing.T) {
	t.Run("накладная изменилась", func(t *testing.T) {
		f := historyUpdateFields(&domain.Dislocation{Invoice: "a", Status: ip(2)},
			&domain.Dislocation{Invoice: "b", Status: ip(2)}, 18)
		assert.Equal(t, "b", f["invoice"])
		_, hasStatus := f["status"]
		assert.False(t, hasStatus)
	})

	t.Run("смена статуса 2→5 (без index_main)", func(t *testing.T) {
		f := historyUpdateFields(&domain.Dislocation{Status: ip(2)}, &domain.Dislocation{Status: ip(5)}, 18)
		assert.Equal(t, 5, f["status"])
		_, hasIdx := f["index_main"]
		assert.False(t, hasIdx)
	})

	t.Run("статус 0→другой → index_main", func(t *testing.T) {
		f := historyUpdateFields(&domain.Dislocation{Status: ip(0)},
			&domain.Dislocation{Status: ip(2), IndexMain: "IDX"}, 18)
		assert.Equal(t, "IDX", f["index_main"])
	})

	t.Run("переход в 12 → выгрузка", func(t *testing.T) {
		f := historyUpdateFields(&domain.Dislocation{Status: ip(2)},
			&domain.Dislocation{Status: ip(12), TimeOp: ltm(2026, 7, 2, 9, 0), DateOpJd: ltm(2026, 7, 2, 9, 0), Naznach: "АЭ"}, 18)
		assert.NotNil(t, f["date_vigr"])
		assert.NotNil(t, f["date_vigr_d"])
		assert.Equal(t, "АЭ", f["place_vigr"])
	})

	t.Run("переход в 10 → прибытие", func(t *testing.T) {
		f := historyUpdateFields(&domain.Dislocation{Status: ip(9)},
			&domain.Dislocation{Status: ip(10), DateKon: ltm(2026, 7, 2, 10, 0), DateDostav: ld(2026, 7, 3), Naznach: "УТ-1"}, 18)
		assert.NotNil(t, f["date_prib"])
		assert.NotNil(t, f["date_prib_d"])
		assert.Equal(t, 0, *(f["delay"].(*int))) // прибыл раньше срока
		assert.Equal(t, "УТ-1", f["naznach"])
	})

	t.Run("нет изменений → пусто", func(t *testing.T) {
		f := historyUpdateFields(&domain.Dislocation{Status: ip(2), Invoice: "a"},
			&domain.Dislocation{Status: ip(2), Invoice: "a"}, 18)
		assert.Empty(t, f)
	})
}

func TestCalculateOtkl(t *testing.T) {
	assert.Equal(t, "+02:00", calculateOtkl(ltm(2026, 7, 2, 10, 0), ltm(2026, 7, 2, 8, 0)))
	// факт час ≥18 → сдвиг на сутки назад: 07-02 19:00 → 07-01 19:00; план 07-01 20:00 → −01:00
	assert.Equal(t, "-01:00", calculateOtkl(ltm(2026, 7, 2, 19, 0), ltm(2026, 7, 1, 20, 0)))
	assert.Equal(t, "", calculateOtkl(nil, ltm(2026, 7, 1, 8, 0)))
}

func TestCalculateHistoryDelay(t *testing.T) {
	assert.Equal(t, 2, *calculateHistoryDelay(ld(2026, 7, 3), ld(2026, 7, 1)))
	assert.Equal(t, 0, *calculateHistoryDelay(ld(2026, 7, 1), ld(2026, 7, 3))) // раньше срока
	assert.Nil(t, calculateHistoryDelay(nil, ld(2026, 7, 1)))
}

func TestApplyHistory(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{items: []domain.Dislocation{{Vagon: "1", Status: ip(2)}}})
	require.NoError(t, actual.Load(ctx))
	const idA = "1/985702/01.07.2026"
	repo := newHistStub(idA) // рейс A уже в истории, B — новый

	kept := []domain.Dislocation{
		{ID: idA, Vagon: "1", DateNach: ld(2026, 7, 1), Status: ip(5), Invoice: "x"},     // переход 2→5
		{ID: "2/985702/01.07.2026", Vagon: "2", DateNach: ld(2026, 7, 1), Status: ip(2)}, // новый рейс
	}
	st, err := applyHistory(ctx, kept, actual, repo, 18)
	require.NoError(t, err)

	assert.Equal(t, 1, st.Inserted)
	assert.Equal(t, 1, st.Updated)
	require.Len(t, repo.inserted, 1)
	assert.Equal(t, "2/985702/01.07.2026", repo.inserted[0].ID)
	assert.Equal(t, 5, repo.updates[idA]["status"])
}

// Регрессия 03.08.2026: у вагона появилась станция отправления (раньше её код
// терялся), из-за чего id рейса стал другим — а trip_key (вагон + дата начала)
// остался прежним. Рейс обязан опознаться как существующий и обновиться по
// СТАРОМУ id: иначе вставка падает на uq_vagon_history_trip_key и обрывает всю
// пересборку снимка.
func TestApplyHistory_TripFoundWhenIDChanged(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{items: []domain.Dislocation{{Vagon: "44463065", Status: ip(2)}}})
	require.NoError(t, actual.Load(ctx))

	repo := newHistStub()
	// В истории рейс лежит под временным id — станции отправления тогда не знали.
	key, ok := historyTripKey(&domain.Dislocation{Vagon: "44463065", DateNach: ld(2026, 7, 28)})
	require.True(t, ok)
	repo.trips[key] = "temp_1785714108003612443"

	kept := []domain.Dislocation{{
		ID: "44463065/033004/28.07.2026", Vagon: "44463065",
		DateNach: ld(2026, 7, 28), Status: ip(5), Invoice: "x",
	}}
	st, err := applyHistory(ctx, kept, actual, repo, 18)
	require.NoError(t, err)

	assert.Equal(t, 0, st.Inserted, "рейс уже есть — вставлять нельзя")
	assert.Equal(t, 1, st.Updated)
	assert.Equal(t, 5, repo.updates["temp_1785714108003612443"]["status"], "обновлять надо строку с её настоящим id")
	assert.Empty(t, repo.inserted)
}

func (r *histStubRepo) RowsByIDs(_ context.Context, ids []string) ([]domain.VagonHistory, error) {
	var out []domain.VagonHistory
	for _, id := range ids {
		if row, ok := r.rows[id]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *histStubRepo) TripsForGU2B(context.Context, []string) ([]domain.GU2BTrip, error) {
	return nil, nil
}

func (r *histStubRepo) TripsForPamyatki(_ context.Context, _ []string) ([]domain.PamyatkaTrip, error) {
	return nil, nil
}

func (r *histStubRepo) UpdateFieldsBatch(_ context.Context, updates map[string]map[string]any) error {
	if r.batch == nil {
		r.batch = map[string]map[string]any{}
	}
	for id, f := range updates {
		r.batch[id] = f
	}
	return nil
}

func (r *histStubRepo) DailyTerminalCounts(_ context.Context, _, _ domain.LocalTime) (map[string]int, map[string]int, map[string]int, error) {
	return nil, nil, nil, nil
}

func (r *histStubRepo) DailyCargoUnloaded(_ context.Context, _, _ domain.LocalTime) (map[string]int, error) {
	return nil, nil
}

func (r *histStubRepo) PerestanovkaRows(_ context.Context, _, _ domain.LocalTime, _ bool) ([]domain.VagonHistory, error) {
	return nil, nil
}

func (r *histStubRepo) LoadingDaily(_ context.Context, _, _ domain.LocalTime) ([]domain.LoadingDailyRow, error) {
	return nil, nil
}

func (r *histStubRepo) SearchRows(_ context.Context, _ domain.HistorySearchFilter, _ string, _ bool, _, _ int) ([]domain.VagonHistory, int, error) {
	return nil, 0, nil
}

func (r *histStubRepo) IterateSearch(_ context.Context, _ domain.HistorySearchFilter, _ string, _ bool, _ func(domain.VagonHistory) error) error {
	return nil
}

func (r *histStubRepo) DistinctStationsNach(_ context.Context) ([]string, error) { return nil, nil }

// TestApplyUnloadOnLeave — авто-веха выгрузки при выбытии статуса-10 из батча
// (случай АЭ 143/144: выгружен и уехал между снимками, перехода 10→12 не было).
func TestApplyUnloadOnLeave(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 21, 19, 30, 0, 0, time.UTC))
	defer restore()

	st10, st2 := 10, 2
	actual := &ActualCache{byVagon: map[string]domain.Dislocation{
		"111": {ID: "A", Vagon: "111", Status: &st10, Naznach: "АЭ"},    // исчез → веха
		"222": {ID: "B", Vagon: "222", Status: &st10, Naznach: "ГУТ-2"}, // остался в батче
		"333": {ID: "C", Vagon: "333", Status: &st2},                    // исчез, но в пути — не наш случай
		"444": {ID: "D", Vagon: "444", Status: &st10, Naznach: "АЭ"},    // исчез, выгрузка уже внесена вручную
	}}
	manual := domain.NewLocalTime(time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC))
	repo := newHistStub()
	repo.rows = map[string]domain.VagonHistory{
		"A": {ID: "A", Vagon: "111"},
		"D": {ID: "D", Vagon: "444", DateVigr: manual},
	}

	kept := []domain.Dislocation{{ID: "B2", Vagon: "222", Status: &st10}}
	n, err := applyUnloadOnLeave(context.Background(), kept, actual, repo, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "веха только для выбывшего без выгрузки")

	f := repo.batch["A"]
	require.NotNil(t, f, "выбывший 111 получил веху")
	assert.Equal(t, 12, f["status"])
	assert.Equal(t, "АЭ", f["place_vigr"])
	assert.Equal(t, "2026-07-21T19:30:00", f["date_vigr"].(domain.LocalTime).String())
	// час 19 ≥ 18 → ЖД-сутки следующего дня
	assert.Equal(t, "2026-07-22T00:00:00", f["date_vigr_d"].(*domain.LocalTime).String())

	_, hasB := repo.batch["B"]
	_, hasC := repo.batch["C"]
	_, hasD := repo.batch["D"]
	assert.False(t, hasB, "оставшийся в батче не трогается")
	assert.False(t, hasC, "исчезнувший в пути — путь записи-8, не выгрузка")
	assert.False(t, hasD, "ручная выгрузка не перетирается")
}

// Выбытие порожнего под погрузку (статус 10) — погрузка и отъезд, а не выгрузка:
// ложная авто-веха выгрузки не пишется (решение владельца 04.08.2026).
func TestApplyUnloadOnLeave_PorozhInboundSkipped(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 21, 19, 30, 0, 0, time.UTC))
	defer restore()

	st10 := 10
	actual := &ActualCache{byVagon: map[string]domain.Dislocation{
		"111": {ID: "A", Vagon: "111", Status: &st10, Naznach: "АЭ", PorozhPriznak: "1"},              // порожний под погрузку → без вехи
		"222": {ID: "B", Vagon: "222", Status: &st10, Naznach: "АЭ", PorozhPriznak: "1", Ves: fp(70)}, // опустевший (вес есть) → веха
	}}
	repo := newHistStub()
	repo.rows = map[string]domain.VagonHistory{
		"A": {ID: "A", Vagon: "111"},
		"B": {ID: "B", Vagon: "222"},
	}

	porozhInbound := func(r *domain.Dislocation) bool {
		return r.PorozhPriznak == "1" && (r.Ves == nil || *r.Ves == 0)
	}
	n, err := applyUnloadOnLeave(context.Background(), nil, actual, repo, porozhInbound)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	_, hasA := repo.batch["A"]
	assert.False(t, hasA, "порожний под погрузку вехи выгрузки не получает")
	require.NotNil(t, repo.batch["B"], "опустевший с весом получает веху как раньше")
	assert.Equal(t, 12, repo.batch["B"]["status"])
}

// Расщеплённый рейс: строка истории лежит под СТАРЫМ id (станции отправления
// тогда не знали — id временный), а снимок держит полный. Веха выгрузки ищется
// по trip_key, как в applyHistory, — поиск по id снимка молча терял бы её
// (тот же класс ошибки, что 23505 от 06.08.2026, только тихий).
func TestApplyUnloadOnLeave_RowFoundByTripKey(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC))
	defer restore()

	st10 := 10
	actual := &ActualCache{byVagon: map[string]domain.Dislocation{
		"63499578": {ID: "63499578/872504/12.07.2026", Vagon: "63499578",
			DateNach: ld(2026, 7, 12), Status: &st10, Naznach: "АЭ"},
	}}
	repo := newHistStub()
	key, ok := historyTripKey(&domain.Dislocation{Vagon: "63499578", DateNach: ld(2026, 7, 12)})
	require.True(t, ok)
	repo.trips[key] = "temp_1785714108003612443"
	repo.rows = map[string]domain.VagonHistory{
		"temp_1785714108003612443": {ID: "temp_1785714108003612443", Vagon: "63499578"},
	}

	n, err := applyUnloadOnLeave(context.Background(), nil, actual, repo, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	f := repo.batch["temp_1785714108003612443"]
	require.NotNil(t, f, "веха ушла в строку с её настоящим id")
	assert.Equal(t, 12, f["status"])
	assert.Equal(t, "АЭ", f["place_vigr"])
	_, wrongRow := repo.batch["63499578/872504/12.07.2026"]
	assert.False(t, wrongRow, "по id снимка ничего не пишется")
}

// tripKeyFromTestID собирает trip_key из id вида «вагон/станция/ДД.ММ.ГГГГ» —
// той же формулой, что генерируемая колонка в БД.
func tripKeyFromTestID(id string) (int64, bool) {
	parts := strings.Split(id, "/")
	if len(parts) != 3 {
		return 0, false
	}
	v, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	d, err := time.Parse("02.01.2006", parts[2])
	if err != nil {
		return 0, false
	}
	days := int64(d.Sub(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)).Hours() / 24)
	return v*100000 + days, true
}

func (r *histStubRepo) FillAttribution(context.Context, []domain.HistoryAttribution) (int, error) {
	return 0, nil
}
