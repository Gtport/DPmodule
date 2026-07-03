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

// s9StubRepo — in-memory port.Status9Repository с семантикой DoNothing по vagon.
type s9StubRepo struct {
	vagons   map[string]struct{}
	inserted []string
	deleted  []string
}

func (r *s9StubRepo) Vagons(context.Context) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(r.vagons))
	for k := range r.vagons {
		out[k] = struct{}{}
	}
	return out, nil
}
func (r *s9StubRepo) InsertNew(_ context.Context, items []domain.Dislocation) (int, error) {
	n := 0
	for _, it := range items {
		if _, ok := r.vagons[it.Vagon]; ok {
			continue
		}
		r.vagons[it.Vagon] = struct{}{}
		r.inserted = append(r.inserted, it.Vagon)
		n++
	}
	return n, nil
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

func ip(v int) *int { return &v }

func TestApplyStatus9Live(t *testing.T) {
	ctx := context.Background()

	// Актуальный снимок: V1 ехал (2), V2 уже кандидат (9 в снимке).
	actual := NewActualCache(s9StubDisl{items: []domain.Dislocation{
		{Vagon: "V1", Status: ip(2)},
		{Vagon: "V2", Status: ip(9)},
	}})
	require.NoError(t, actual.Load(ctx))

	// В таблице кандидатов уже лежит V3 (с прошлого цикла).
	repo := &s9StubRepo{vagons: map[string]struct{}{"V3": {}}}

	// Новый батч (после Stage 1, со статусами):
	batch := []domain.Dislocation{
		{Vagon: "V1", Status: ip(9)}, // был 2 → стал 9: первое появление → insert
		{Vagon: "V2", Status: ip(9)}, // был 9 → остался 9: не первое → skip
		{Vagon: "V3", Status: ip(2)}, // кандидат вернулся в поток (≠9) → delete
		{Vagon: "V4", Status: ip(2)}, // обычный, не в таблице → ничего
		{Vagon: "V5", Status: ip(9)}, // новый вагон сразу 9 (нет в актуальной) → insert
	}

	st, err := applyStatus9Live(ctx, batch, actual, repo)
	require.NoError(t, err)

	assert.Equal(t, 2, st.Inserted) // V1, V5
	assert.Equal(t, 1, st.Removed)  // V3
	assert.ElementsMatch(t, []string{"V1", "V5"}, repo.inserted)
	assert.ElementsMatch(t, []string{"V3"}, repo.deleted)
	// итоговое множество таблицы: V1, V2(нет — не вставляли), V5 ... V2 не вставлялся,
	// V3 удалён. Осталось: V1, V5.
	assert.Contains(t, repo.vagons, "V1")
	assert.Contains(t, repo.vagons, "V5")
	assert.NotContains(t, repo.vagons, "V3")
	assert.NotContains(t, repo.vagons, "V2") // повторный 9 не попал в таблицу
}

// Пустой вагон в батче пропускается.
func TestApplyStatus9Live_SkipEmptyVagon(t *testing.T) {
	ctx := context.Background()
	actual := NewActualCache(s9StubDisl{})
	require.NoError(t, actual.Load(ctx))
	repo := &s9StubRepo{vagons: map[string]struct{}{}}

	st, err := applyStatus9Live(ctx, []domain.Dislocation{{Vagon: "", Status: ip(9)}}, actual, repo)
	require.NoError(t, err)
	assert.Equal(t, 0, st.Inserted)
}
