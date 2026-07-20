package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// Ближайшие поезда: статусы 9/10/12 исключены, время план→прогноз→расчёт,
// плановые помечены, сортировка «ближайшие сверху», собственник в вагонах.
func TestNearestTrains(t *testing.T) {
	lt := func(h int) *domain.LocalTime {
		return domain.NewLocalTime(time.Date(2026, 7, 21, h, 0, 0, 0, time.UTC))
	}
	st := func(v int) *int { return &v }
	mk := func(id, idDisl, index, vagon, naznach string, status *int) domain.Dislocation {
		return domain.Dislocation{
			ID: id, IdDisl: idDisl, Index: index, Vagon: vagon,
			Naznach: naznach, GruzpolS: naznach, StanNazn: "МЫС АСТАФЬЕВА",
			StationOper: "БИКИН", Status: status, Owner: "ООО ОПЕРАТОР",
		}
	}

	// Поезд A — плановый (прогноз = план, 18:00), B — бесплановый с прогнозом
	// (09:00), C — прибывший (исключён), D — кандидат-9 (исключён), E — чужой
	// терминал, F — без прогноза (только расчётный ход) — в «Ближайшие» не попадает.
	a1 := mk("A1", "TA", "9379-786-9857", "111", "АЭ", st(2))
	a1.PlanMsk, a1.PlanJd = lt(18), lt(18)
	a1.ProgMsk, a1.ProgJd = lt(18), lt(18) // у плановых прогноз = план
	a2 := mk("A2", "TA", "9379-786-9857", "112", "АЭ", st(5)) // брошенный вагон в составе
	b1 := mk("B1", "TB", "8631-880-9847", "221", "ГУТ-2", st(2))
	b1.ProgMsk, b1.ProgJd = lt(9), lt(9)
	c1 := mk("C1", "TC", "9999-001-9857", "331", "АЭ", st(10))
	d1 := mk("D1", "TD", "9999-002-9857", "441", "АЭ", st(9))
	e1 := mk("E1", "TE", "7777-003-9847", "551", "УТ-1", st(2))
	e1.ProgMsk = lt(7)
	f1 := mk("F1", "TF", "6666-004-9857", "661", "АЭ", st(2))
	f1.RaschMsk = lt(5) // расчётный ход есть, прогноза нет → исключён

	repo := &fakeDislRepo{current: []domain.Dislocation{a1, a2, b1, c1, d1, e1, f1}}
	actual := service.NewActualCache(repo)
	require.NoError(t, actual.Load(context.Background()))

	svc := service.NewNearestService(actual, nil)
	trains := svc.Trains(context.Background(), []string{"АЭ", "ГУТ-2"}, 0)

	require.Len(t, trains, 2) // C (10), D (9), F (без прогноза) исключены; E — чужой терминал
	// Сортировка по прогнозу: B (09:00) → A (18:00, прогноз = план, зелёная метка).
	assert.Equal(t, "TB", trains[0].Key)
	assert.False(t, trains[0].HasPlan)
	assert.Equal(t, "TA", trains[1].Key)
	assert.True(t, trains[1].HasPlan)
	assert.True(t, trains[1].Broshen) // брошенный вагон подсвечивает поезд
	assert.Equal(t, 2, trains[1].VagonCount)
	require.Len(t, trains[1].SubGroups, 1)
	require.NotEmpty(t, trains[1].SubGroups[0].Vagons)
	assert.Equal(t, "ООО ОПЕРАТОР", trains[1].SubGroups[0].Vagons[0].Owner)
}
