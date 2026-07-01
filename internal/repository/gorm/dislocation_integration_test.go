package gormrepo_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/config"
	"github.com/Gtport/DPmodule/internal/domain"
	gormrepo "github.com/Gtport/DPmodule/internal/repository/gorm"
)

// Integration-тест против реальной БД. Запускается только если задан
// DPMODULE_TEST_PG_DSN (иначе Skip). ВНИМАНИЕ: мутирует таблицу dislocation —
// гонять только на dev-базе (dpport), не на проде.
func TestDislocationRepository_ReplaceAndLoad(t *testing.T) {
	dsn := os.Getenv("DPMODULE_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DPMODULE_TEST_PG_DSN не задан — пропускаю integration-тест")
	}

	db, err := gormrepo.Open(config.Postgres{DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2})
	require.NoError(t, err)

	repo := gormrepo.NewDislocationRepository(db)
	ctx := context.Background()

	items := []domain.Dislocation{
		{
			ID: "test-1", Vagon: "10000001", AlternativeMove: 1, Status: ptrInt(2),
			DateNach: domain.NewLocalTime(time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)),
		},
		{ID: "test-2", Vagon: "10000002", StanNazn: "МЫС АСТАФЬЕВА"},
	}
	require.NoError(t, repo.ReplaceActual(ctx, items))

	got, err := repo.LoadActual(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)

	byID := map[string]domain.Dislocation{}
	for _, d := range got {
		byID[d.ID] = d
	}
	assert.Equal(t, "10000001", byID["test-1"].Vagon)
	assert.Equal(t, 1, byID["test-1"].AlternativeMove)
	require.NotNil(t, byID["test-1"].Status)
	assert.Equal(t, 2, *byID["test-1"].Status)
	require.NotNil(t, byID["test-1"].DateNach)
	assert.Equal(t, "2026-06-30T10:00:00", byID["test-1"].DateNach.String()) // время без Z
	assert.Equal(t, "МЫС АСТАФЬЕВА", byID["test-2"].StanNazn)

	// Повторная замена пустым набором → снимок пуст (свап идемпотентен).
	require.NoError(t, repo.ReplaceActual(ctx, nil))
	got, err = repo.LoadActual(ctx)
	require.NoError(t, err)
	assert.Len(t, got, 0)
}

func ptrInt(i int) *int { return &i }
