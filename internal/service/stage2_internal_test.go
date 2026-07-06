package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

// s9StubDisl — минимальный DislocationRepository для наполнения ActualCache.
type s9StubDisl struct{ items []domain.Dislocation }

func (s s9StubDisl) LoadActual(context.Context) ([]domain.Dislocation, error)  { return s.items, nil }
func (s s9StubDisl) ReplaceActual(context.Context, []domain.Dislocation) error { return nil }

// s9StubRepo — in-memory port.Status9Repository (vagon → статус в таблице).
type s9StubRepo struct {
	vagons   map[string]int // vagon → статус (8/9)
	inserted []string
	deleted  []string
	missing8 []string
}

func (r *s9StubRepo) VagonStatuses(context.Context) (map[string]int, error) {
	out := make(map[string]int, len(r.vagons))
	for k, v := range r.vagons {
		out[k] = v
	}
	return out, nil
}
func (r *s9StubRepo) InsertNew(_ context.Context, items []domain.Dislocation) (int, error) {
	n := 0
	for _, it := range items {
		if _, ok := r.vagons[it.Vagon]; ok {
			continue // DoNothing по vagon
		}
		r.vagons[it.Vagon] = derefStatus(it.Status)
		r.inserted = append(r.inserted, it.Vagon)
		n++
	}
	return n, nil
}
func (r *s9StubRepo) UpsertMissing(_ context.Context, items []domain.Dislocation) (int, error) {
	for _, it := range items {
		r.vagons[it.Vagon] = derefStatus(it.Status) // insert или update статуса (→8)
		r.missing8 = append(r.missing8, it.Vagon)
	}
	return len(items), nil
}
func (r *s9StubRepo) DeleteByVagons(_ context.Context, vagons []string) (int, error) {
	n := 0
	for _, v := range vagons {
		if _, ok := r.vagons[v]; ok {
			delete(r.vagons, v)
			r.deleted = append(r.deleted, v)
			n++
		}
	}
	return n, nil
}

func derefStatus(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// s6StubRepo — in-memory port.Status6Repository.
type s6StubRepo struct {
	stored   map[string]domain.Dislocation
	upserted []domain.Dislocation
}

func newS6Stub() *s6StubRepo { return &s6StubRepo{stored: map[string]domain.Dislocation{}} }

func (r *s6StubRepo) LoadAll(context.Context) ([]domain.Dislocation, error) {
	out := make([]domain.Dislocation, 0, len(r.stored))
	for _, d := range r.stored {
		out = append(out, d)
	}
	return out, nil
}
func (r *s6StubRepo) Upsert(_ context.Context, items []domain.Dislocation) (int, error) {
	for _, it := range items {
		r.stored[it.Vagon] = it
	}
	r.upserted = append(r.upserted, items...)
	return len(items), nil
}
func (r *s6StubRepo) DeleteByVagons(_ context.Context, vagons []string) (int, error) {
	n := 0
	for _, v := range vagons {
		if _, ok := r.stored[v]; ok {
			delete(r.stored, v)
			n++
		}
	}
	return n, nil
}

// s9cache/s6cache — кэши поверх stub-репозиториев (прогреты).
func s9cache(t *testing.T, repo *s9StubRepo) *Status9Cache {
	t.Helper()
	c := NewStatus9Cache(repo)
	require.NoError(t, c.Load(context.Background()))
	return c
}
func s6cache(t *testing.T, repo *s6StubRepo) *Status6Cache {
	t.Helper()
	c := NewStatus6Cache(repo)
	require.NoError(t, c.Load(context.Background()))
	return c
}

// Переход на статус 6 (был ≠6) → донор в status6. gruzpol_s/naznach обнулены ТОЛЬКО
// в снимке; в записи-доноре они реальные (нужны для передачи приёмнику, §3.17). Новый
// сразу 6 и «уже был 6» — не фиксируются.
func TestApplyStatus6Transition(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{items: []domain.Dislocation{
		{Vagon: "T1", Status: ip(2)}, // ехал гружёным
		{Vagon: "T2", Status: ip(6)}, // уже был порожним
	}})
	require.NoError(t, actual.Load(ctx))
	repo := newS6Stub()

	kept := []domain.Dislocation{
		{Vagon: "T1", Status: ip(6), GruzpolS: "ГУТ-2", Naznach: "ГУТ-2", CargoS: "УГОЛЬ"}, // 2→6: переход, донор
		{Vagon: "T2", Status: ip(6), GruzpolS: "УТ-1"},                                     // 6→6: не переход
		{Vagon: "T3", Status: ip(6), GruzpolS: "АЭ"},                                       // новый сразу 6: не фиксируем
		{Vagon: "T4", Status: ip(2), GruzpolS: "ГУТ-2"},                                    // не 6
	}

	n, err := applyStatus6Transition(ctx, kept, actual, s6cache(t, repo))
	require.NoError(t, err)

	assert.Equal(t, 1, n) // только T1
	require.Len(t, repo.upserted, 1)
	d := repo.upserted[0]
	assert.Equal(t, "T1", d.Vagon)
	assert.Equal(t, "ГУТ-2", d.GruzpolS) // в доноре реальные — для передачи приёмнику
	assert.Equal(t, "ГУТ-2", d.Naznach)
	assert.Equal(t, "УГОЛЬ", d.CargoS) // груз сохранён для передачи
	// в снимке T1 тоже обнулён
	assert.Equal(t, "0", kept[0].GruzpolS)
	assert.Equal(t, "0", kept[0].Naznach)
	// T2/T3/T4 в снимке не тронуты
	assert.Equal(t, "УТ-1", kept[1].GruzpolS)
	assert.Equal(t, "АЭ", kept[2].GruzpolS)
}

func ip(v int) *int { return &v }

// Живой статус 9: первое появление / повторный / возврат в поток.
func TestReconcile_Status9Live(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{items: []domain.Dislocation{
		{Vagon: "V1", Status: ip(2)},
		{Vagon: "V2", Status: ip(9)},
	}})
	require.NoError(t, actual.Load(ctx))
	repo := &s9StubRepo{vagons: map[string]int{"V3": 9}} // V3 — кандидат с прошлого цикла

	batch := []domain.Dislocation{
		{Vagon: "V1", Status: ip(9)}, // был 2 → 9: первое появление → insert
		{Vagon: "V2", Status: ip(9)}, // был 9 → 9: не первое → skip
		{Vagon: "V3", Status: ip(2)}, // кандидат вернулся (≠9) → delete
		{Vagon: "V4", Status: ip(2)}, // обычный, не в таблице → ничего
		{Vagon: "V5", Status: ip(9)}, // новый сразу 9 → insert
	}

	st, err := reconcileCandidates(ctx, batch, actual, s9cache(t, repo))
	require.NoError(t, err)

	assert.Equal(t, 2, st.Inserted) // V1, V5
	assert.Equal(t, 1, st.Removed)  // V3
	assert.Contains(t, repo.vagons, "V1")
	assert.Contains(t, repo.vagons, "V5")
	assert.NotContains(t, repo.vagons, "V3")
	assert.NotContains(t, repo.vagons, "V2")
}

// Пропавшие → статус 8; порожний (6) выбывает; статус-9 при пропаже → 8.
func TestReconcile_Missing8(t *testing.T) {
	ctx := context.Background()
	// актуальная: M1 ехал (2), M2 порожний в пути (6), M3 живой кандидат (9), P — останется в батче
	actual := NewActualCache(s9StubDisl{items: []domain.Dislocation{
		{Vagon: "M1", Status: ip(2)},
		{Vagon: "M2", Status: ip(6)},
		{Vagon: "M3", Status: ip(9)},
		{Vagon: "P", Status: ip(2)},
	}})
	require.NoError(t, actual.Load(ctx))
	repo := &s9StubRepo{vagons: map[string]int{"M3": 9}} // M3 уже в таблице как живой 9

	// В батче только P (остальные пропали).
	batch := []domain.Dislocation{{Vagon: "P", Status: ip(2)}}

	st, err := reconcileCandidates(ctx, batch, actual, s9cache(t, repo))
	require.NoError(t, err)

	assert.Equal(t, 2, st.Missing8) // M1 (новый 8) + M3 (перевод 9→8); M2 выбыл
	assert.Equal(t, 8, repo.vagons["M1"])
	assert.Equal(t, 8, repo.vagons["M3"]) // 9 → 8 при пропаже
	assert.NotContains(t, repo.vagons, "M2")
	assert.ElementsMatch(t, []string{"M1", "M3"}, repo.missing8)
}

// Вагон был защищённым 8, вернулся живым кандидатом 9 → снять 8, записать 9.
func TestReconcile_Return8AsLive9(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{}) // вагона нет в снимке (пропадал)
	require.NoError(t, actual.Load(ctx))
	repo := &s9StubRepo{vagons: map[string]int{"W": 8}} // W лежит как пропавший 8

	batch := []domain.Dislocation{{Vagon: "W", Status: ip(9)}} // вернулся, сразу на ст.назн

	st, err := reconcileCandidates(ctx, batch, actual, s9cache(t, repo))
	require.NoError(t, err)

	assert.Equal(t, 1, st.Removed)  // старый 8 снят
	assert.Equal(t, 1, st.Inserted) // записан как живой 9
	assert.Equal(t, 9, repo.vagons["W"])
	assert.Contains(t, repo.deleted, "W")
}

// Вагон был 8, вернулся в поток НЕ как кандидат (статус 2) → снять из таблицы.
func TestReconcile_Return8ToStream(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{})
	require.NoError(t, actual.Load(ctx))
	repo := &s9StubRepo{vagons: map[string]int{"W": 8}}

	batch := []domain.Dislocation{{Vagon: "W", Status: ip(2)}}

	st, err := reconcileCandidates(ctx, batch, actual, s9cache(t, repo))
	require.NoError(t, err)

	assert.Equal(t, 1, st.Removed)
	assert.NotContains(t, repo.vagons, "W")
}

// Пустой вагон в батче пропускается.
func TestReconcile_SkipEmptyVagon(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{})
	require.NoError(t, actual.Load(ctx))
	repo := &s9StubRepo{vagons: map[string]int{}}

	st, err := reconcileCandidates(ctx, []domain.Dislocation{{Vagon: "", Status: ip(9)}}, actual, s9cache(t, repo))
	require.NoError(t, err)
	assert.Equal(t, 0, st.Inserted)
}
