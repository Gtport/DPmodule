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

// Integration-тест FillAttribution (гейт DPMODULE_TEST_PG_DSN): дозаполнение
// бизнес-атрибуции строк vagon_history по trip_key. Фиксирует guard gruzotpr = ''
// (заполненные строки, в т.ч. вручную, не перетираются), адресацию по trip_key
// (генерируемая колонка БД обязана совпасть с domain.TripKeyOf) и идемпотентность.
func TestHistoryRepository_FillAttribution(t *testing.T) {
	dsn := os.Getenv("DPMODULE_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DPMODULE_TEST_PG_DSN не задан — пропускаю integration-тест")
	}

	db, err := gormrepo.Open(config.Postgres{DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2}, nil, 0)
	require.NoError(t, err)

	const vagBase = "9989920" // тестовые «номера» — в живых данных не встречаются
	t.Cleanup(func() {
		db.Exec("DELETE FROM vagon_history WHERE vagon LIKE ?", vagBase+"%")
	})

	day := func(d int) *domain.LocalTime {
		lt := domain.LocalTime(time.Date(2032, 4, d, 0, 0, 0, 0, time.UTC))
		return &lt
	}
	repo := gormrepo.NewHistoryRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.Insert(ctx, []domain.VagonHistory{
		// 0: рейс без атрибуции — дозаполняется.
		{ID: "t-fa-0", Vagon: vagBase + "0", DateNachD: day(1)},
		// 1: атрибуция уже есть — guard не даёт перетереть.
		{ID: "t-fa-1", Vagon: vagBase + "1", DateNachD: day(1), Gruzotpr: "СВОЙ", Client: "СВОЙ КЛ"},
	}))

	key0, ok := domain.TripKeyOf(vagBase+"0", day(1))
	require.True(t, ok)
	key1, ok := domain.TripKeyOf(vagBase+"1", day(1))
	require.True(t, ok)

	attrs := []domain.HistoryAttribution{
		{TripKey: key0, Gruzotpr: "ОТПР", Client: "КЛ", Sms1: "Улак", Sms2: "Улак Г", Sms3: "УЛАК", Color: "#FFC000"},
		{TripKey: key1, Gruzotpr: "ЧУЖОЙ", Client: "ЧУЖОЙ КЛ"},
		{TripKey: key0 + 55555, Gruzotpr: "НЕТ ТАКОГО"}, // рейса нет — просто 0 строк
	}
	filled, err := repo.FillAttribution(ctx, attrs)
	require.NoError(t, err)
	assert.Equal(t, 1, filled)

	rows, err := repo.RowsByIDs(ctx, []string{"t-fa-0", "t-fa-1"})
	require.NoError(t, err)
	byID := map[string]domain.VagonHistory{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	assert.Equal(t, "ОТПР", byID["t-fa-0"].Gruzotpr)
	assert.Equal(t, "Улак Г", byID["t-fa-0"].Sms2)
	assert.Equal(t, "#FFC000", byID["t-fa-0"].Color)
	assert.Equal(t, "СВОЙ", byID["t-fa-1"].Gruzotpr) // guard: не перетёрта
	assert.Equal(t, "СВОЙ КЛ", byID["t-fa-1"].Client)

	// Идемпотентность: повторный вызов ничего не находит (gruzotpr уже непуст).
	filled, err = repo.FillAttribution(ctx, attrs)
	require.NoError(t, err)
	assert.Equal(t, 0, filled)
}
