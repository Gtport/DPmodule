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

	st, err := reconcileCandidates(ctx, batch, actual, repo)
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

	st, err := reconcileCandidates(ctx, batch, actual, repo)
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

	st, err := reconcileCandidates(ctx, batch, actual, repo)
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

	st, err := reconcileCandidates(ctx, batch, actual, repo)
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

	st, err := reconcileCandidates(ctx, []domain.Dislocation{{Vagon: "", Status: ip(9)}}, actual, repo)
	require.NoError(t, err)
	assert.Equal(t, 0, st.Inserted)
}
