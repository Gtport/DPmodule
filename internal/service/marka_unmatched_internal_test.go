package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

func unmLT(y int, m time.Month, d int) *domain.LocalTime {
	t := domain.LocalTime(time.Date(y, m, d, 12, 0, 0, 0, time.UTC))
	return &t
}

// unmatchedCache — ActualCache с готовым содержимым (без БД).
func unmatchedCache(rows ...domain.Dislocation) *ActualCache {
	m := map[string]domain.Dislocation{}
	for _, r := range rows {
		m[r.Vagon] = r
	}
	return &ActualCache{byVagon: m}
}

func TestUnmatchedGroups(t *testing.T) {
	dir := markaDir(t, markaFixture, cargoFixture, nil)
	zero := 0.0
	cache := unmatchedCache(
		// Сматченный — в список не попадает.
		domain.Dislocation{Vagon: "1000", Gruzotpr: "ОТПР", GruzotprOkpo: "1", CodeStationNach: "2", CargoGroup: "УГОЛЬ"},
		// Порожний под погрузку — матч невозможен в принципе, не показываем.
		domain.Dislocation{Vagon: "1001", PorozhPriznak: "1", Ves: &zero},
		// Полный ключ, но комбинации в marka нет: ОКПО и группа в словаре есть, станции нет.
		domain.Dislocation{Vagon: "2001", ID: "2001/3/01.08.2026", GruzotprOkpo: "1", CodeStationNach: "3",
			CargoGroup: "УГОЛЬ", CargoS: "УГОЛЬ Г", StationNach: "ЧЕЛУТАЙ", DateNach: unmLT(2026, 8, 1)},
		domain.Dislocation{Vagon: "2002", ID: "2002/3/01.08.2026", GruzotprOkpo: "1", CodeStationNach: "3",
			CargoGroup: "УГОЛЬ", CargoS: "УГОЛЬ Г", StationNach: "ЧЕЛУТАЙ", DateNach: unmLT(2026, 8, 1)},
		// Кривое ОКПО.
		domain.Dislocation{Vagon: "3001", GruzotprOkpo: "", CodeStationNach: "2", CargoGroup: "УГОЛЬ"},
		// Код груза вне словаря cargo — группа пустая.
		domain.Dislocation{Vagon: "4001", GruzotprOkpo: "1", CodeStationNach: "2", CodeCargo: "999999"},
	)

	svc := NewMarkaUnmatchedService(cache, dir, nil, nil)
	groups := svc.Groups()
	require.Len(t, groups, 3)

	// Крупная группа первой.
	g := groups[0]
	assert.Equal(t, "1|3|УГОЛЬ", g.Key)
	assert.Equal(t, 2, g.VagonCount)
	assert.Equal(t, ReasonNoMarka, g.Reason)
	assert.True(t, g.OkpoInMarka)
	assert.False(t, g.StationInMarka)
	assert.True(t, g.GroupInMarka)
	assert.True(t, g.CanAddToMarka)
	assert.Equal(t, "ЧЕЛУТАЙ", g.StationNach)
	require.NotNil(t, g.Suggest)
	assert.Equal(t, "ОТПР", g.Suggest.Gruzotpr)
	assert.Equal(t, "КЛ", g.Suggest.Client)
	require.Len(t, g.Vagons, 2)
	assert.Equal(t, "2001/3/01.08.2026", g.Vagons[0].ID)

	byReason := map[string]UnmatchedGroupDTO{}
	for _, g := range groups {
		byReason[g.Reason] = g
	}
	bad := byReason[ReasonBadOkpo]
	assert.Equal(t, 1, bad.VagonCount)
	assert.False(t, bad.CanAddToMarka)
	assert.Nil(t, bad.Suggest)

	nc := byReason[ReasonNoCargoGroup]
	assert.Equal(t, 1, nc.VagonCount)
	assert.False(t, nc.CanAddToMarka) // без группы груза комбинацию в marka не завести
	require.NotNil(t, nc.Suggest)     // ОКПО известное — подсказка есть
}

func TestApplyManualAttribution(t *testing.T) {
	dir := markaDir(t, markaFixture, cargoFixture, nil)
	all := []domain.Dislocation{
		// Целевая комбинация: заполняется, sms_2 пересчитывается.
		{Vagon: "2001", GruzotprOkpo: "1", CodeStationNach: "3", CargoGroup: "УГОЛЬ",
			CargoSms: "Г", DateNach: unmLT(2026, 8, 1)},
		// Уже атрибутирован — не трогаем.
		{Vagon: "2002", Gruzotpr: "ДРУГОЙ", GruzotprOkpo: "1", CodeStationNach: "3", CargoGroup: "УГОЛЬ"},
		// Чужая комбинация (другая станция) — не трогаем.
		{Vagon: "2003", GruzotprOkpo: "1", CodeStationNach: "4", CargoGroup: "УГОЛЬ"},
	}
	req := MarkaAssignRequest{
		Okpo: "1", StationKod: "3", CargoGroup: "УГОЛЬ",
		Gruzotpr: "НОВЫЙ ОТПР", Client: "НОВЫЙ КЛ", Sms1: "Чел",
	}
	n, attrs := applyManualAttribution(all, req, dir)
	assert.Equal(t, 1, n)

	assert.Equal(t, "НОВЫЙ ОТПР", all[0].Gruzotpr)
	assert.Equal(t, "НОВЫЙ КЛ", all[0].Client)
	assert.Equal(t, "Чел Г", all[0].Sms2) // sms_1 + cargo_sms
	assert.Equal(t, "ДРУГОЙ", all[1].Gruzotpr)
	assert.Empty(t, all[2].Gruzotpr)

	require.Len(t, attrs, 1)
	key, ok := historyTripKey(&all[0])
	require.True(t, ok)
	assert.Equal(t, key, attrs[0].TripKey)
	assert.Equal(t, "НОВЫЙ ОТПР", attrs[0].Gruzotpr)
	assert.Equal(t, "Чел Г", attrs[0].Sms2)
}

func TestAttributionRows(t *testing.T) {
	all := []domain.Dislocation{
		// Атрибутирован, ключ рейса есть — попадает.
		{Vagon: "2001", Gruzotpr: "ОТПР", Sms1: "Улак", Sms2: "Улак Г", DateNach: unmLT(2026, 8, 1)},
		// Без атрибуции — пропускается.
		{Vagon: "2002", DateNach: unmLT(2026, 8, 1)},
		// Без даты начала рейса ключ не построить — пропускается.
		{Vagon: "2003", Gruzotpr: "ОТПР"},
	}
	rows := attributionRows(all)
	require.Len(t, rows, 1)
	key, ok := historyTripKey(&all[0])
	require.True(t, ok)
	assert.Equal(t, key, rows[0].TripKey)
	assert.Equal(t, "ОТПР", rows[0].Gruzotpr)
	assert.Equal(t, "Улак Г", rows[0].Sms2)
}
