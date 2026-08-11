package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Валидация запроса экрана «Работа с историческими данными» (без БД): разбор и
// свап дат, белый список сортировки, кламп страницы, потолки списков и
// несовместимость «не выгруж.» со списком мест выгрузки.

func TestBuildHistoryFilter(t *testing.T) {
	t.Run("даты разбираются, перепутанные границы свапаются", func(t *testing.T) {
		f, err := buildHistoryFilter(HistorySearchFilterDTO{
			DateNachDFrom: "2026-07-31", DateNachDTo: "2026-07-01",
		})
		require.NoError(t, err)
		require.NotNil(t, f.DateNachDFrom)
		require.NotNil(t, f.DateNachDTo)
		assert.Equal(t, "2026-07-01", f.DateNachDFrom.Time().Format("2006-01-02"))
		assert.Equal(t, "2026-07-31", f.DateNachDTo.Time().Format("2006-01-02"))
	})

	t.Run("кривая дата — ошибка валидации с именем поля", func(t *testing.T) {
		_, err := buildHistoryFilter(HistorySearchFilterDTO{DateVigrDFrom: "31.07.2026"})
		require.ErrorIs(t, err, ErrHistorySearchInvalid)
		assert.Contains(t, err.Error(), "дата выгрузки")
	})

	t.Run("списки нормализуются: трим, пустые вон", func(t *testing.T) {
		f, err := buildHistoryFilter(HistorySearchFilterDTO{
			Vagons: []string{" 62158654 ", "", "62158655"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"62158654", "62158655"}, f.Vagons)
	})

	t.Run("список длиннее потолка — ошибка", func(t *testing.T) {
		long := make([]string, historyListMax+1)
		for i := range long {
			long[i] = "62158654"
		}
		_, err := buildHistoryFilter(HistorySearchFilterDTO{Vagons: long})
		require.ErrorIs(t, err, ErrHistorySearchInvalid)
	})

	t.Run("не выгруж. + список мест выгрузки — конфликт", func(t *testing.T) {
		_, err := buildHistoryFilter(HistorySearchFilterDTO{
			NotUnloaded: true, PlaceVigr: []string{"АЭ"},
		})
		require.ErrorIs(t, err, ErrHistorySearchInvalid)
	})

	t.Run("only_overdue проходит в доменный фильтр", func(t *testing.T) {
		f, err := buildHistoryFilter(HistorySearchFilterDTO{OnlyOverdue: true})
		require.NoError(t, err)
		assert.True(t, f.OnlyOverdue)
	})
}

func TestHistorySortColumn(t *testing.T) {
	t.Run("дефолт — дата погрузки, новые сверху", func(t *testing.T) {
		col, desc, err := historySortColumn(HistorySortDTO{})
		require.NoError(t, err)
		assert.Equal(t, "date_nach_d", col)
		assert.True(t, desc)
	})

	t.Run("ключ API мапится в колонку SQL", func(t *testing.T) {
		col, desc, err := historySortColumn(HistorySortDTO{By: "freight", Dir: "asc"})
		require.NoError(t, err)
		assert.Equal(t, "freight_exact_name", col)
		assert.False(t, desc)
	})

	t.Run("вне белого списка — отказ (защита ORDER BY)", func(t *testing.T) {
		_, _, err := historySortColumn(HistorySortDTO{By: "vagon; DROP TABLE vagon_history"})
		require.ErrorIs(t, err, ErrHistorySearchInvalid)
	})

	t.Run("кривое направление — отказ", func(t *testing.T) {
		_, _, err := historySortColumn(HistorySortDTO{By: "vagon", Dir: "up"})
		require.ErrorIs(t, err, ErrHistorySearchInvalid)
	})
}

func TestHistoryPage(t *testing.T) {
	limit, offset, err := historyPage(HistoryPageDTO{})
	require.NoError(t, err)
	assert.Equal(t, historyLimitDefault, limit)
	assert.Equal(t, 0, offset)

	_, _, err = historyPage(HistoryPageDTO{Limit: historyLimitMax + 1})
	require.ErrorIs(t, err, ErrHistorySearchInvalid)

	_, _, err = historyPage(HistoryPageDTO{Limit: 100, Offset: -1})
	require.ErrorIs(t, err, ErrHistorySearchInvalid)
}
