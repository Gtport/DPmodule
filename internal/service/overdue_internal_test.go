package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

func ovLT(y int, m time.Month, d int) *domain.LocalTime {
	t := domain.LocalTime(time.Date(y, m, d, 12, 0, 0, 0, time.UTC))
	return &t
}

func ovInt(v int) *int { return &v }

// overdueCache — ActualCache с готовым содержимым (без БД).
func overdueCache(rows ...domain.Dislocation) *ActualCache {
	m := map[string]domain.Dislocation{}
	for _, r := range rows {
		m[r.Vagon] = r
	}
	return &ActualCache{byVagon: m}
}

func TestOverdueGroups(t *testing.T) {
	s10, s12, s2, s6 := 10, 12, 2, 6
	cache := overdueCache(
		// Без просрочки (delay nil и delay 0) — не попадают.
		domain.Dislocation{Vagon: "1000", Invoice: "Н1"},
		domain.Dislocation{Vagon: "1001", Invoice: "Н1", Delay: ovInt(0)},
		// Прибывший и выгруженный — просрочка зафиксирована в истории, не показываем.
		domain.Dislocation{Vagon: "1002", Invoice: "Н1", Delay: ovInt(5), Status: &s10},
		domain.Dislocation{Vagon: "1003", Invoice: "Н1", Delay: ovInt(5), Status: &s12},
		// Накладная Н1: два вагона в пути, разные просрочки и нормативы.
		domain.Dislocation{Vagon: "2002", ID: "2002/1/01.08.2026", Invoice: "Н1", InvoiceMain: "Н1",
			StationNach: "ЕРУНАКОВО", Gruzotpr: "ОТПР", StanNazn: "НАХОДКА", GruzpolS: "АЭ",
			CargoS: "УГОЛЬ Г", Delay: ovInt(3), Status: &s2,
			DateDostav: ovLT(2026, 8, 8), DateNach: ovLT(2026, 7, 20)},
		domain.Dislocation{Vagon: "2001", ID: "2001/1/01.08.2026", Invoice: "Н1", InvoiceMain: "Н1",
			StationNach: "ЕРУНАКОВО", Gruzotpr: "ОТПР", StanNazn: "НАХОДКА", GruzpolS: "АЭ",
			CargoS: "УГОЛЬ Г", Delay: ovInt(1), Status: &s2, DateDostav: ovLT(2026, 8, 10)},
		// Пустая invoice — фолбэк на invoice_main.
		domain.Dislocation{Vagon: "3001", InvoiceMain: "Н2", Delay: ovInt(7), Status: &s6},
		// Совсем без накладной — одна группа с пустым ключом.
		domain.Dislocation{Vagon: "4001", Delay: ovInt(2), Status: &s2},
		domain.Dislocation{Vagon: "4002", Delay: ovInt(4), Status: &s2},
	)

	svc := NewOverdueService(cache, nil)
	groups := svc.Groups()
	require.Len(t, groups, 3)

	// Сортировка групп — по максимальной просрочке.
	assert.Equal(t, "Н2", groups[0].Key)
	assert.Equal(t, 7, groups[0].MaxDelay)
	assert.Equal(t, "", groups[1].Key) // «без накладной», max delay 4
	assert.Equal(t, 4, groups[1].MaxDelay)

	g := groups[2]
	assert.Equal(t, "Н1", g.Key)
	assert.Equal(t, 2, g.VagonCount)
	assert.Equal(t, 3, g.MaxDelay)
	assert.Equal(t, "ЕРУНАКОВО", g.StationNach)
	assert.Equal(t, "ОТПР", g.Gruzotpr)
	assert.Equal(t, "НАХОДКА", g.StanNazn)
	// Ранний норматив группы.
	require.NotNil(t, g.DateDostav)
	assert.Equal(t, time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC), time.Time(*g.DateDostav))
	// Вагоны внутри группы — по номеру.
	require.Len(t, g.Vagons, 2)
	assert.Equal(t, "2001", g.Vagons[0].Vagon)
	assert.Equal(t, "2002", g.Vagons[1].Vagon)
	assert.Equal(t, 3, g.Vagons[1].Delay)
	assert.Equal(t, "2002/1/01.08.2026", g.Vagons[1].ID)
}

func TestOverdueGroupsEmpty(t *testing.T) {
	svc := NewOverdueService(overdueCache(), nil)
	assert.Empty(t, svc.Groups())
}
