package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Группировка пропавших: поезд (index|stan_nazn) → подгруппа
// (index_main|naznach|gruzpol_s) → вагоны; позиция группы — самая свежая
// операция; days_missing — от самой свежей фиксации пропажи.
func TestGroupMissing(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	lt := func(d, h int) *domain.LocalTime {
		return domain.NewLocalTime(time.Date(2026, 7, 25+d, h, 0, 0, 0, time.UTC))
	}
	s8 := 8
	mk := func(id, vagon, index, stanNazn, indexMain, naznach, gruzpol string,
		timeOp *domain.LocalTime, upd *domain.LocalTime) domain.Dislocation {
		return domain.Dislocation{
			ID: id, Vagon: vagon, Index: index, StanNazn: stanNazn,
			IndexMain: indexMain, StationNach: "Челутай", Naznach: naznach,
			GruzpolS: gruzpol, StationOper: "СТ-" + vagon, OperS: "ОП-" + vagon,
			TimeOp: timeOp, Status: &s8, UpdatedAt: *upd,
		}
	}

	rows := []domain.Dislocation{
		// Поезд A, две подгруппы; вагон 222 — самая свежая операция группы.
		mk("A1", "111", "9379-783-9857", "Мыс А.", "9379-783-9857", "АЭ", "АЭ", lt(0, 8), lt(6, 10)),
		mk("A2", "222", "9379-783-9857", "Мыс А.", "9379-783-9857", "АЭ", "АЭ", lt(1, 9), lt(6, 10)),
		mk("A3", "333", "9379-783-9857", "Мыс А.", "9379-784-9857", "ГУТ-2", "АЭ", lt(0, 7), lt(5, 10)),
		// Поезд B — без индекса (одиночный вагон).
		mk("B1", "444", "", "Находка", "", "УТ-1", "УТ-1", lt(0, 6), lt(3, 10)),
	}

	groups := groupMissing(rows, now)
	require.Len(t, groups, 2)

	a := groups[0]
	assert.Equal(t, "9379-783-9857|Мыс А.", a.Key)
	assert.Equal(t, 3, a.VagonCount)
	assert.Equal(t, "СТ-222", a.StationOper, "позиция группы — вагон с самой свежей операцией")
	assert.Equal(t, "ОП-222", a.OperS)
	assert.Equal(t, lt(1, 9).String(), a.TimeOp.String())
	assert.Equal(t, lt(6, 10).String(), a.MissingSince.String(), "самая свежая фиксация пропажи")
	assert.Equal(t, 2, a.DaysMissing) // 31.07 10:00 → 02.08 12:00 = 2 полных суток

	require.Len(t, a.SubGroups, 2)
	sg := a.SubGroups[0]
	assert.Equal(t, "9379-783-9857|АЭ|АЭ", sg.Key)
	assert.Equal(t, 2, sg.VagonCount)
	assert.Equal(t, "(2)-783-Челутай АЭ", sg.Display)
	require.Len(t, sg.Vagons, 2)
	assert.Equal(t, "111", sg.Vagons[0].Vagon)
	sg2 := a.SubGroups[1]
	assert.Equal(t, "(1)-784-Челутай АЭ → ГУТ-2", sg2.Display, "перестановка: получатель → назначение")

	b := groups[1]
	assert.Equal(t, "|Находка", b.Key)
	assert.Equal(t, 1, b.VagonCount)
	assert.Equal(t, "(1)-Челутай УТ-1", b.SubGroups[0].Display, "без индекса — счёт, станция и назначение")
	assert.Equal(t, 5, b.DaysMissing) // 28.07 10:00 → 02.08 12:00
	assert.Equal(t, 5, b.SubGroups[0].Vagons[0].DaysMissing)
}
