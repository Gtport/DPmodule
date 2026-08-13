package gormrepo_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/config"
	"github.com/Gtport/DPmodule/internal/domain"
	gormrepo "github.com/Gtport/DPmodule/internal/repository/gorm"
)

// Integration-тест поисковых выборок экрана «Работа с историческими данными»
// (гейт DPMODULE_TEST_PG_DSN). Фиксирует: каждый фильтр по отдельности, «не
// выгруж.» (в схеме DPmodule place_vigr NOT NULL DEFAULT '' — NULL, терявшийся
// в gtport, невозможен by design; COALESCE в запросе — страховка), включительные
// границы диапазонов дат, total/страницы пагинации, направление сортировки с
// NULLS LAST и стабильность порядка при равных значениях, курсорный обход.
func TestHistoryRepository_Search(t *testing.T) {
	dsn := os.Getenv("DPMODULE_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("DPMODULE_TEST_PG_DSN не задан — пропускаю integration-тест")
	}

	db, err := gormrepo.Open(config.Postgres{DSN: dsn, MaxOpenConns: 5, MaxIdleConns: 2}, nil, 0)
	require.NoError(t, err)

	const vagBase = "9989910" // тестовые «номера» — в живых данных не встречаются
	const station = "СТ-ПОИСК"
	t.Cleanup(func() {
		db.Exec("DELETE FROM vagon_history WHERE vagon LIKE ?", vagBase+"%")
	})

	day := func(d int) *domain.LocalTime {
		lt := domain.LocalTime(time.Date(2032, 3, d, 0, 0, 0, 0, time.UTC))
		return &lt
	}
	dint := func(v int) *int { return &v }
	repo := gormrepo.NewHistoryRepository(db)
	ctx := context.Background()

	// Сцена: 5 рейсов одной тестовой станции погрузки.
	rows := []domain.VagonHistory{
		// 0: выгружен на АЭ, погрузка 01.03, прибытие 05.03, выгрузка 06.03,
		// просрочка доставки 3 суток.
		{ID: "t-hs-0", Vagon: vagBase + "0", StationNach: station, GruzpolS: "АЭ",
			Naznach: "АЭ", PlaceVigr: "АЭ", Invoice: "ЭЛ100001",
			DateNachD: day(1), DatePribD: day(5), DateVigrD: day(6), Delay: dint(3)},
		// 1: не выгружен — place_vigr пустая строка; погрузка 02.03.
		{ID: "t-hs-1", Vagon: vagBase + "1", StationNach: station, GruzpolS: "АЭ",
			Naznach: "ГУТ-2", PlaceVigr: "", Invoice: "ЭЛ100002",
			DateNachD: day(2), DatePribD: day(6)},
		// 2: не выгружен — place_vigr не заполнялся (в базе DEFAULT ''); погрузка 03.03.
		{ID: "t-hs-2", Vagon: vagBase + "2", StationNach: station, GruzpolS: "ГУТ-2",
			Naznach: "ГУТ-2", Invoice: "ЭЛ100003", DateNachD: day(3)},
		// 3: та же дата погрузки, что у 2 (стабильность сортировки), выгружен
		// на ГУТ-2, прибыл В СРОК (delay 0 — в «просрочку» не попадает).
		{ID: "t-hs-3", Vagon: vagBase + "3", StationNach: station, GruzpolS: "ГУТ-2",
			Naznach: "ГУТ-2", PlaceVigr: "ГУТ-2", Invoice: "ЭЛ100004",
			DateNachD: day(3), DatePribD: day(7), DateVigrD: day(8), Delay: dint(0)},
		// 4: дата погрузки NULL — при сортировке по date_nach_d всегда внизу.
		{ID: "t-hs-4", Vagon: vagBase + "4", StationNach: station, GruzpolS: "УТ-1",
			Naznach: "УТ-1", PlaceVigr: "УТ-1", Invoice: "ЭЛ100005"},
	}
	require.NoError(t, repo.Insert(ctx, rows))

	// Свои строки узнаём по станции погрузки.
	mine := domain.HistorySearchFilter{StationNach: []string{station}}

	t.Run("все свои + сортировка date_nach_d desc, NULL внизу", func(t *testing.T) {
		got, total, err := repo.SearchRows(ctx, mine, "date_nach_d", true, 100, 0)
		require.NoError(t, err)
		assert.Equal(t, 5, total)
		require.Len(t, got, 5)
		// desc: 03.03 (вагоны 2,3 — при равной дате порядок по vagon), 02.03, 01.03, NULL.
		assert.Equal(t, "t-hs-2", got[0].ID)
		assert.Equal(t, "t-hs-3", got[1].ID)
		assert.Equal(t, "t-hs-1", got[2].ID)
		assert.Equal(t, "t-hs-0", got[3].ID)
		assert.Equal(t, "t-hs-4", got[4].ID, "NULL date_nach_d — в конце и при desc")
	})

	t.Run("пагинация: страницы не пересекаются, total одинаковый", func(t *testing.T) {
		p1, total1, err := repo.SearchRows(ctx, mine, "date_nach_d", true, 2, 0)
		require.NoError(t, err)
		p2, total2, err := repo.SearchRows(ctx, mine, "date_nach_d", true, 2, 2)
		require.NoError(t, err)
		assert.Equal(t, 5, total1)
		assert.Equal(t, total1, total2)
		require.Len(t, p1, 2)
		require.Len(t, p2, 2)
		assert.NotEqual(t, p1[1].ID, p2[0].ID)
		assert.Equal(t, "t-hs-1", p2[0].ID, "продолжение того же порядка")
	})

	t.Run("не выгруж.: пустое место выгрузки", func(t *testing.T) {
		f := mine
		f.NotUnloaded = true
		got, total, err := repo.SearchRows(ctx, f, "vagon", false, 100, 0)
		require.NoError(t, err)
		assert.Equal(t, 2, total)
		require.Len(t, got, 2)
		assert.Equal(t, "t-hs-1", got[0].ID)
		assert.Equal(t, "t-hs-2", got[1].ID)
	})

	t.Run("границы дат включительные", func(t *testing.T) {
		f := mine
		f.DateNachDFrom, f.DateNachDTo = day(2), day(3)
		_, total, err := repo.SearchRows(ctx, f, "vagon", false, 100, 0)
		require.NoError(t, err)
		assert.Equal(t, 3, total, "02.03 и обе строки 03.03; NULL-дата не попадает")

		f = mine
		f.DateVigrDFrom, f.DateVigrDTo = day(6), day(6)
		got, total, err := repo.SearchRows(ctx, f, "vagon", false, 100, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Equal(t, "t-hs-0", got[0].ID, "выгрузка ровно в граничный день")
	})

	t.Run("списковые фильтры: терминалы, вагоны, накладные", func(t *testing.T) {
		f := mine
		f.GruzpolS = []string{"ГУТ-2"}
		_, total, err := repo.SearchRows(ctx, f, "vagon", false, 100, 0)
		require.NoError(t, err)
		assert.Equal(t, 2, total)

		f = mine
		f.PlaceVigr = []string{"АЭ", "УТ-1"}
		_, total, err = repo.SearchRows(ctx, f, "vagon", false, 100, 0)
		require.NoError(t, err)
		assert.Equal(t, 2, total)

		f = mine
		f.Vagons = []string{vagBase + "1", vagBase + "4"}
		f.Invoices = []string{"ЭЛ100002"}
		got, total, err := repo.SearchRows(ctx, f, "vagon", false, 100, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, total, "фильтры соединяются по И")
		assert.Equal(t, "t-hs-1", got[0].ID)
	})

	t.Run("курсорный обход в порядке сортировки", func(t *testing.T) {
		var ids []string
		err := repo.IterateSearch(ctx, mine, "date_nach_d", true, func(h domain.VagonHistory) error {
			ids = append(ids, h.ID)
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"t-hs-2", "t-hs-3", "t-hs-1", "t-hs-0", "t-hs-4"}, ids)

		// Ошибка колбэка прерывает обход.
		n := 0
		err = repo.IterateSearch(ctx, mine, "vagon", false, func(domain.VagonHistory) error {
			n++
			return fmt.Errorf("стоп")
		})
		require.Error(t, err)
		assert.Equal(t, 1, n)
	})

	t.Run("только просроченные: delay > 0, NULL и 0 отсекаются", func(t *testing.T) {
		f := mine
		f.OnlyOverdue = true
		got, total, err := repo.SearchRows(ctx, f, "vagon", false, 100, 0)
		require.NoError(t, err)
		assert.Equal(t, 1, total, "delay NULL (без прибытия) и delay 0 (в срок) не попадают")
		require.Len(t, got, 1)
		assert.Equal(t, "t-hs-0", got[0].ID)
	})

	t.Run("словарь станций погрузки содержит тестовую", func(t *testing.T) {
		stations, err := repo.DistinctStationsNach(ctx)
		require.NoError(t, err)
		assert.Contains(t, stations, station)
	})
}
