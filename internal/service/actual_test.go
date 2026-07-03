package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// stubActualRepo — in-memory port.DislocationRepository для теста ActualCache.
type stubActualRepo struct{ items []domain.Dislocation }

func (s *stubActualRepo) LoadActual(context.Context) ([]domain.Dislocation, error) {
	return s.items, nil
}
func (s *stubActualRepo) ReplaceActual(context.Context, []domain.Dislocation) error { return nil }

func TestActualCache_LoadFindCount(t *testing.T) {
	repo := &stubActualRepo{items: []domain.Dislocation{
		{Vagon: "55535108", StanNazn: "МЫС АСТАФЬЕВА", GruzpolS: "ГУТ-2"},
		{Vagon: "52803384", StanNazn: "МЫС АСТАФЬЕВА", GruzpolS: "АЭ"},
		{Vagon: "", StanNazn: "X"}, // пустой вагон — пропускается
	}}
	c := service.NewActualCache(repo)
	require.NoError(t, c.Load(context.Background()))

	assert.Equal(t, 2, c.Count()) // пустой вагон не в мапе

	r, ok := c.FindVagonInActual("55535108")
	require.True(t, ok)
	assert.Equal(t, "ГУТ-2", r.GruzpolS)

	_, ok = c.FindVagonInActual("00000000")
	assert.False(t, ok)

	assert.Len(t, c.All(), 2)
}

func TestActualCache_DuplicateVagonLastWins(t *testing.T) {
	repo := &stubActualRepo{items: []domain.Dislocation{
		{Vagon: "1", GruzpolS: "первый"},
		{Vagon: "1", GruzpolS: "второй"},
	}}
	c := service.NewActualCache(repo)
	require.NoError(t, c.Load(context.Background()))

	assert.Equal(t, 1, c.Count())
	r, _ := c.FindVagonInActual("1")
	assert.Equal(t, "второй", r.GruzpolS)
}
