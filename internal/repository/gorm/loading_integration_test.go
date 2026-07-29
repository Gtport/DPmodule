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

// Integration-тест LoadingDaily против реальной БД (гейт DPMODULE_TEST_PG_DSN).
// Проверяет перенос отчёта «Погрузка»: группировку по дате × терминалу × sms_1
// × клиенту × группе груза, суммы кол-ва/веса, исключение перегрузов
// (peregruz непустой — TARGET.md §3.17) и границы периода. Свои строки убирает.
func TestHistoryRepository_LoadingDaily(t *testing.T) {
	dsn := os.Getenv("DPMODULE_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DPMODULE_TEST_PG_DSN не задан — пропускаю integration-тест")
	}

	db, err := gormrepo.Open(config.Postgres{DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2})
	require.NoError(t, err)

	const vagBase = "9989900" // тестовые «номера» — в живых данных не встречаются
	t.Cleanup(func() {
		db.Exec("DELETE FROM vagon_history WHERE vagon LIKE ?", vagBase+"%")
	})

	day := func(d int) *domain.LocalTime {
		lt := domain.LocalTime(time.Date(2031, 7, d, 0, 0, 0, 0, time.UTC))
		return &lt
	}
	ves := func(v float64) *float64 { return &v }
	row := func(n int, d int, term, sms, client, group, peregruz string, w float64) domain.VagonHistory {
		return domain.VagonHistory{
			ID: "test-load-" + string(rune('a'+n)), Vagon: vagBase + string(rune('0'+n)),
			DateNachD: day(d), GruzpolS: term, Sms1: sms, StationNach: "СТ",
			Client: client, CargoGroup: group, Peregruz: peregruz, Ves: ves(w),
		}
	}

	repo := gormrepo.NewHistoryRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Insert(ctx, []domain.VagonHistory{
		row(0, 1, "АЭ", "РАЗРЕЗ", "КЛИЕНТ1", "УГОЛЬ", "", 69.5), // сливается со следующей
		row(1, 1, "АЭ", "РАЗРЕЗ", "КЛИЕНТ1", "УГОЛЬ", "", 70.0),
		row(2, 1, "ГУТ-2", "ЕВРАЗ", "КЛИЕНТ2", "МЕТАЛЛ", "", 60),      // другая группа груза
		row(3, 1, "АЭ", "РАЗРЕЗ", "КЛИЕНТ1", "УГОЛЬ", "12345678", 65), // перегруз — исключён
		row(4, 2, "АЭ", "РАЗРЕЗ", "КЛИЕНТ1", "УГОЛЬ", "", 66),         // другой день
		row(5, 9, "АЭ", "РАЗРЕЗ", "КЛИЕНТ1", "УГОЛЬ", "", 67),         // вне периода
	}))

	got, err := repo.LoadingDaily(ctx, *day(1), *day(2))
	require.NoError(t, err)

	// В выборку попадают и живые данные базы — оставляем только свои строки.
	mine := got[:0:0]
	for _, r := range got {
		if r.StationNach == "СТ" {
			mine = append(mine, r)
		}
	}
	require.Len(t, mine, 3)

	assert.Equal(t, "АЭ", mine[0].GruzpolS)
	assert.Equal(t, 2, mine[0].VagonCount) // перегруз не посчитан
	assert.InDelta(t, 139.5, mine[0].TotalWeight, 0.001)
	assert.Equal(t, "УГОЛЬ", mine[0].CargoGroup)

	assert.Equal(t, "ГУТ-2", mine[1].GruzpolS)
	assert.Equal(t, "МЕТАЛЛ", mine[1].CargoGroup)
	assert.Equal(t, 1, mine[1].VagonCount)

	assert.Equal(t, "2031-07-02", mine[2].Day.String()[:10]) // второй день отдельной строкой
	assert.Equal(t, 1, mine[2].VagonCount)
}
