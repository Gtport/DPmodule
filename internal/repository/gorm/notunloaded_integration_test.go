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

// Integration-тест NotUnloadedCounts (гейт DPMODULE_TEST_PG_DSN): «не выгружено»
// карточки «Оперативка» по истории. Сторожит фильтры SQL: только статус 10,
// без вехи выгрузки (place_vigr пуст), не «недоехавший», гружёный (ves > 0 —
// порожние под погрузку не считаются), прибытие не раньше порога. Свои строки
// убирает.
func TestHistoryRepository_NotUnloadedCounts(t *testing.T) {
	dsn := os.Getenv("DPMODULE_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DPMODULE_TEST_PG_DSN не задан — пропускаю integration-тест")
	}

	db, err := gormrepo.Open(config.Postgres{DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2}, nil, 0)
	require.NoError(t, err)

	const vagBase = "9989910" // тестовые «номера» — в живых данных не встречаются
	t.Cleanup(func() {
		db.Exec("DELETE FROM vagon_history WHERE vagon LIKE ?", vagBase+"%")
	})

	day := func(d int) *domain.LocalTime {
		lt := domain.LocalTime(time.Date(2031, 8, d, 0, 0, 0, 0, time.UTC))
		return &lt
	}
	ves := func(v float64) *float64 { return &v }
	st := func(v int) *int { return &v }
	row := func(n, pribD int, naznach, placeVigr string, status *int, w *float64, notArrived bool) domain.VagonHistory {
		return domain.VagonHistory{
			ID: "test-nuc-" + string(rune('a'+n)), Vagon: vagBase + string(rune('0'+n)),
			DateNachD: day(n + 1), DatePribD: day(pribD), Naznach: naznach,
			PlaceVigr: placeVigr, Status: status, Ves: w, NotArrived: notArrived,
		}
	}

	repo := gormrepo.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Insert(ctx, []domain.VagonHistory{
		row(0, 20, "АЭ", "", st(10), ves(70), false),    // считается
		row(1, 20, "АЭ", "", st(10), ves(69), false),    // считается
		row(2, 20, "АЭ", "АЭ", st(12), ves(70), false),  // выгружен — нет
		row(3, 20, "АЭ", "", st(10), nil, false),        // порожний под погрузку — нет
		row(4, 20, "АЭ", "", st(10), ves(70), true),     // «недоехавший» — нет
		row(5, 5, "АЭ", "", st(10), ves(70), false),     // прибыл раньше порога — нет
		row(6, 20, "ГУТ-2", "", st(10), ves(70), false), // другой терминал
		row(7, 20, "АЭ", "", st(2), ves(70), false),     // ещё в пути — нет
	}))

	got, err := repo.NotUnloadedCounts(ctx, *day(10))
	require.NoError(t, err)

	assert.Equal(t, 2, got["АЭ"], "АЭ: два гружёных прибывших без выгрузки")
	assert.Equal(t, 1, got["ГУТ-2"])
	assert.NotContains(t, got, "", "пустой терминал не попадает в счёт")
}
