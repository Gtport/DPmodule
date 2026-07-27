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
// DPMODULE_TEST_PG_DSN (иначе Skip). Требует применённой миграции
// 000047_vagon_delay. Полный цикл эпизода: открытие → чтение открытых →
// эскалация/закрытие → эпизод ушёл из открытых. Свои строки убирает за собой.
func TestVagonDelayRepository_Lifecycle(t *testing.T) {
	dsn := os.Getenv("DPMODULE_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DPMODULE_TEST_PG_DSN не задан — пропускаю integration-тест")
	}

	db, err := gormrepo.Open(config.Postgres{DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2})
	require.NoError(t, err)

	const vag = "test-delay-0001" // тестовый «номер» — в живых данных не встречается
	t.Cleanup(func() {
		db.Exec("DELETE FROM vagon_delay WHERE vagon = ?", vag)
	})

	repo := gormrepo.NewVagonDelayRepository(db)
	ctx := context.Background()
	from := domain.NewLocalTime(time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC))
	now := domain.NewLocalTime(time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC))

	// Открытие эпизода.
	require.NoError(t, repo.Insert(ctx, domain.VagonDelay{
		Vagon: vag, Kind: domain.DelayKindProstoi, GroupKey: "IDX|770001|op",
		Index: "1234-567-8901", IndexMain: "1234-567-8901",
		StationCode: "770001", StationName: "ТЕСТОВАЯ", Doroga: "ДВ",
		DateNachD: domain.NewLocalTime(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)),
		DateFrom:  from, CreatedAt: now, UpdatedAt: now,
	}))

	// Эпизод виден среди открытых, поля доехали.
	open, err := repo.Open(ctx)
	require.NoError(t, err)
	var mine *domain.VagonDelay
	for i := range open {
		if open[i].Vagon == vag {
			mine = &open[i]
			break
		}
	}
	require.NotNil(t, mine, "открытый эпизод тестового вагона не найден")
	assert.Equal(t, domain.DelayKindProstoi, mine.Kind)
	assert.Equal(t, "1234-567-8901", mine.Index)
	assert.Equal(t, "1234-567-8901", mine.IndexMain)
	assert.Equal(t, "770001", mine.StationCode)
	require.NotNil(t, mine.DateFrom)
	assert.Equal(t, time.Time(*from), time.Time(*mine.DateFrom))
	assert.Nil(t, mine.DateTo)

	// Эскалация 4 → 5 и закрытие.
	to := domain.NewLocalTime(time.Date(2026, 7, 22, 9, 30, 0, 0, time.UTC))
	require.NoError(t, repo.Update(ctx, mine.ID, map[string]any{
		"kind": domain.DelayKindBros, "date_to": to, "hours": 49.5, "updated_at": now,
	}))

	// Закрытый эпизод из открытых ушёл.
	open2, err := repo.Open(ctx)
	require.NoError(t, err)
	for _, d := range open2 {
		assert.NotEqual(t, vag, d.Vagon, "закрытый эпизод не должен быть среди открытых")
	}

	// Автоочистка: закрытый эпизод старше cutoff удаляется, открытый — не трогается.
	require.NoError(t, repo.Insert(ctx, domain.VagonDelay{
		Vagon: vag, Kind: domain.DelayKindProstoi, StationCode: "880002",
		DateFrom: to, CreatedAt: now, UpdatedAt: now, // второй эпизод, открытый
	}))
	purged, err := repo.PurgeClosedOlderThan(ctx, *domain.NewLocalTime(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, purged, 1) // как минимум наш закрытый
	var left int64
	require.NoError(t, db.Raw("SELECT count(*) FROM vagon_delay WHERE vagon = ?", vag).Scan(&left).Error)
	assert.Equal(t, int64(1), left, "открытый эпизод должен пережить автоочистку")
}
