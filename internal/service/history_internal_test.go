package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

type histStubRepo struct {
	existing map[string]struct{}
	inserted []domain.VagonHistory
	updates  map[string]map[string]any
	rows     map[string]domain.VagonHistory // для RowsByIDs
	batch    map[string]map[string]any      // записи UpdateFieldsBatch
}

func newHistStub(existing ...string) *histStubRepo {
	e := map[string]struct{}{}
	for _, id := range existing {
		e[id] = struct{}{}
	}
	return &histStubRepo{existing: e, updates: map[string]map[string]any{}}
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
		h := buildHistoryRow(r, now)
		assert.Equal(t, "A", h.ID)
		assert.Equal(t, "im", h.InvoiceMain)
		assert.Nil(t, h.DatePrib)
		assert.Nil(t, h.DateVigr)
	})

	t.Run("прибыл (10) — поля прибытия", func(t *testing.T) {
		r := &domain.Dislocation{ID: "A", Vagon: "1", Status: ip(10),
			DateKon: ltm(2026, 7, 2, 10, 0), DateDostav: ld(2026, 7, 1)}
		h := buildHistoryRow(r, now)
		require.NotNil(t, h.DatePrib)
		require.NotNil(t, h.DatePribD)
		require.NotNil(t, h.Delay)
		assert.Equal(t, 1, *h.Delay) // прибыл 2-го, срок 1-го → 1 сутки
		assert.Empty(t, h.Otkl)      // без плана
	})

	t.Run("выгружен в порту (12) — поля выгрузки", func(t *testing.T) {
		r := &domain.Dislocation{ID: "A", Vagon: "1", Status: ip(12),
			TimeOp: ltm(2026, 7, 2, 9, 0), DateOpJd: ltm(2026, 7, 2, 9, 0), Naznach: "ГУТ-2"}
		h := buildHistoryRow(r, now)
		require.NotNil(t, h.DateVigr)
		require.NotNil(t, h.DateVigrD)
		assert.Equal(t, "ГУТ-2", h.PlaceVigr)
	})
}

func TestHistoryUpdateFields(t *testing.T) {
	t.Run("накладная изменилась", func(t *testing.T) {
		f := historyUpdateFields(&domain.Dislocation{Invoice: "a", Status: ip(2)},
			&domain.Dislocation{Invoice: "b", Status: ip(2)})
		assert.Equal(t, "b", f["invoice"])
		_, hasStatus := f["status"]
		assert.False(t, hasStatus)
	})

	t.Run("смена статуса 2→5 (без index_main)", func(t *testing.T) {
		f := historyUpdateFields(&domain.Dislocation{Status: ip(2)}, &domain.Dislocation{Status: ip(5)})
		assert.Equal(t, 5, f["status"])
		_, hasIdx := f["index_main"]
		assert.False(t, hasIdx)
	})

	t.Run("статус 0→другой → index_main", func(t *testing.T) {
		f := historyUpdateFields(&domain.Dislocation{Status: ip(0)},
			&domain.Dislocation{Status: ip(2), IndexMain: "IDX"})
		assert.Equal(t, "IDX", f["index_main"])
	})

	t.Run("переход в 12 → выгрузка", func(t *testing.T) {
		f := historyUpdateFields(&domain.Dislocation{Status: ip(2)},
			&domain.Dislocation{Status: ip(12), TimeOp: ltm(2026, 7, 2, 9, 0), DateOpJd: ltm(2026, 7, 2, 9, 0), Naznach: "АЭ"})
		assert.NotNil(t, f["date_vigr"])
		assert.NotNil(t, f["date_vigr_d"])
		assert.Equal(t, "АЭ", f["place_vigr"])
	})

	t.Run("переход в 10 → прибытие", func(t *testing.T) {
		f := historyUpdateFields(&domain.Dislocation{Status: ip(9)},
			&domain.Dislocation{Status: ip(10), DateKon: ltm(2026, 7, 2, 10, 0), DateDostav: ld(2026, 7, 3), Naznach: "УТ-1"})
		assert.NotNil(t, f["date_prib"])
		assert.NotNil(t, f["date_prib_d"])
		assert.Equal(t, 0, *(f["delay"].(*int))) // прибыл раньше срока
		assert.Equal(t, "УТ-1", f["naznach"])
	})

	t.Run("нет изменений → пусто", func(t *testing.T) {
		f := historyUpdateFields(&domain.Dislocation{Status: ip(2), Invoice: "a"},
			&domain.Dislocation{Status: ip(2), Invoice: "a"})
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
	repo := newHistStub("A") // рейс A уже в истории, B — новый

	kept := []domain.Dislocation{
		{ID: "A", Vagon: "1", Status: ip(5), Invoice: "x"}, // переход 2→5
		{ID: "B", Vagon: "2", Status: ip(2)},               // новый рейс
	}
	st, err := applyHistory(ctx, kept, actual, repo)
	require.NoError(t, err)

	assert.Equal(t, 1, st.Inserted)
	assert.Equal(t, 1, st.Updated)
	require.Len(t, repo.inserted, 1)
	assert.Equal(t, "B", repo.inserted[0].ID)
	assert.Equal(t, 5, repo.updates["A"]["status"])
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

func (r *histStubRepo) UpdateFieldsBatch(_ context.Context, updates map[string]map[string]any) error {
	if r.batch == nil {
		r.batch = map[string]map[string]any{}
	}
	for id, f := range updates {
		r.batch[id] = f
	}
	return nil
}

func (r *histStubRepo) DailyTerminalCounts(_ context.Context, _, _ domain.LocalTime) (map[string]int, map[string]int, error) {
	return nil, nil, nil
}

func (r *histStubRepo) DailyCargoUnloaded(_ context.Context, _, _ domain.LocalTime) (map[string]int, error) {
	return nil, nil
}

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
	n, err := applyUnloadOnLeave(context.Background(), kept, actual, repo)
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
