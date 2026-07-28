package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Группировка снимка для «Нового прогноза»: только подход (статус < 9) с
// prog_jd на известный терминал; поезд склеивается по index_pp; подгруппы —
// по (index_main, станция отправления, причал, назначение, группа груза);
// дата погрузки — максимум по вагонам.
func TestForecastTransitGroups(t *testing.T) {
	lt := func(d, h int) *domain.LocalTime {
		return domain.NewLocalTime(time.Date(2026, 7, d, h, 0, 0, 0, time.UTC))
	}
	st := func(v int) *int { return &v }
	mk := func(index, indexPp, naznach, cargo, stNach string, status *int, progJd *domain.LocalTime) domain.Dislocation {
		return domain.Dislocation{
			Index: index, IndexPp: indexPp, IndexMain: index,
			Naznach: naznach, GruzpolS: naznach, CargoGroup: cargo,
			StationNach: stNach, StationOper: "БИКИН",
			Status: status, ProgJd: progJd, Color: "#123456",
		}
	}
	known := map[string]bool{"АЭ": true, "ГУТ-2": true}

	a1 := mk("9379-795-9857", "", "АЭ", "УГОЛЬ", "ЧЕЛУТАЙ", st(2), lt(28, 6))
	a1.DateNach = lt(21, 0)
	a2 := mk("9379-795-9857", "", "АЭ", "УГОЛЬ", "ЧЕЛУТАЙ", st(2), lt(28, 6))
	a2.DateNach = lt(22, 0) // позже — подгруппа возьмёт максимум
	// Тот же поезд, другая группа груза → вторая подгруппа.
	a3 := mk("9379-795-9857", "", "ГУТ-2", "МЕТАЛЛ", "ЧЕЛУТАЙ", st(2), lt(28, 6))
	// Плановая нитка склеивает разные текущие индексы в один поезд.
	b1 := mk("8725-182-0000", "8725-182-9857", "АЭ", "УГОЛЬ", "ЛЕНИНСК", st(5), lt(29, 3))
	b2 := mk("8725-999-0000", "8725-182-9857", "АЭ", "УГОЛЬ", "ЛЕНИНСК", st(5), lt(29, 3))
	// Исключения: прибыл (10), кандидат (9), без прогноза, чужое назначение.
	c1 := mk("1111-111-1111", "", "АЭ", "УГОЛЬ", "ЧЕЛУТАЙ", st(10), lt(28, 6))
	c2 := mk("2222-222-2222", "", "АЭ", "УГОЛЬ", "ЧЕЛУТАЙ", st(9), lt(28, 6))
	c3 := mk("3333-333-3333", "", "АЭ", "УГОЛЬ", "ЧЕЛУТАЙ", st(2), nil)
	c4 := mk("4444-444-4444", "", "ВП", "УГОЛЬ", "ЧЕЛУТАЙ", st(2), lt(28, 6))

	groups := forecastTransitGroups(
		[]domain.Dislocation{a1, a2, a3, b1, b2, c1, c2, c3, c4}, known)

	require.Len(t, groups, 2)
	// Сортировка по prog_jd: поезд A (28.07) раньше B (29.07).
	g := groups[0]
	assert.Equal(t, "9379-795-9857", g.Index)
	require.Len(t, g.SubGroups, 2)
	assert.Equal(t, 2, g.SubGroups[0].VagonCount)
	assert.Equal(t, "УГОЛЬ", g.SubGroups[0].CargoGroup)
	assert.Equal(t, lt(22, 0), g.SubGroups[0].DateNach) // максимум дат погрузки
	assert.Equal(t, "ГУТ-2", g.SubGroups[1].Naznach)

	// Склейка по нитке: один поезд, но подгруппы остаются по index_main
	// (как в gtport — окончательное слияние строк делает клиент по ключу
	// индекс|станция отправления|дата погрузки).
	assert.Equal(t, "8725-182-9857", groups[1].Index)
	require.Len(t, groups[1].SubGroups, 2)
	assert.Equal(t, 1, groups[1].SubGroups[0].VagonCount)
	assert.Equal(t, 5, *groups[1].Status)
}

// Прибывшие: группа по (index_pp, date_prib), подгруппы как у едущих,
// чужие назначения отфильтрованы.
func TestForecastArrivedGroups(t *testing.T) {
	lt := func(d, h int) *domain.LocalTime {
		return domain.NewLocalTime(time.Date(2026, 7, d, h, 0, 0, 0, time.UTC))
	}
	mk := func(indexPp, naznach, cargo string, prib *domain.LocalTime) domain.VagonHistory {
		return domain.VagonHistory{
			IndexPp: indexPp, IndexMain: indexPp, Naznach: naznach,
			CargoGroup: cargo, StationNach: "ЧЕЛУТАЙ",
			DatePrib: prib, DateNachD: lt(20, 0), Color: "#abcdef",
		}
	}
	known := map[string]bool{"АЭ": true}

	rows := []domain.VagonHistory{
		mk("9722-550-9857", "АЭ", "УГОЛЬ", lt(28, 5)),
		mk("9722-550-9857", "АЭ", "УГОЛЬ", lt(28, 5)),
		mk("9722-550-9857", "АЭ", "УГОЛЬ", lt(28, 11)), // другое время прибытия — другая группа
		mk("5555-555-5555", "УТ-1", "УГОЛЬ", lt(28, 5)), // чужой терминал — вон
	}

	groups := forecastArrivedGroups(rows, known)
	require.Len(t, groups, 2)
	assert.Equal(t, 2, groups[0].SubGroups[0].VagonCount)
	assert.Equal(t, lt(28, 5), groups[0].DatePrib)
	assert.Equal(t, 1, groups[1].SubGroups[0].VagonCount)
}
