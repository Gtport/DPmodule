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

// Golden-тест экрана «Поезда в движении» (перенос gtport
// OperatorToolsDislocation.tsx + service/dislocation_extended.go).
// Фиксирует правила gtport:
//   - группировка по нормализованному индексу (index_pp, если есть, иначе index)
//     и станции операции: отцепка того же индекса на другой станции — отдельная
//     строка-поезд;
//   - подгруппа = index_main + station_nach + gruzpol_s + naznach + cargo_group;
//   - простой поезда — МАКСИМУМ по вагонам (в gtport комментарий «суммарный» врал);
//   - вагоны внутри подгруппы по npp_vag, вес поезда — сумма;
//   - сортировка поездов: rasst ↑ (без rasst — в конец), затем индекс, станция.
// И согласованные отходы от gtport (решение владельца 31.07.2026):
//   - статус поезда — агрегат по вагонам (большинство), а не «первый попавшийся
//     из недетерминированного обхода мапы»; broshen = есть вагон-5, arrived =
//     все вагоны на станции назначения (статусы 9/10/12);
//   - поля поезда (план/прогноз/операция) — первое непустое по порядку вагонов
//     natural-листа (детерминированно);
//   - собственник вагона — owner (в gtport колонку заполнял rod_vag_uch),
//     марка груза — freight_exact_name (просьба владельца).
func TestTrainsInMotion(t *testing.T) {
	lt := func(d, h int) *domain.LocalTime {
		return domain.NewLocalTime(time.Date(2026, 7, d, h, 0, 0, 0, time.UTC))
	}
	ip := func(v int) *int { return &v }
	fp := func(v float64) *float64 { return &v }

	mk := func(id, vagon, index, stationOper string, status int) domain.Dislocation {
		return domain.Dislocation{
			ID: id, Vagon: vagon, Index: index, StationOper: stationOper,
			IndexMain: "9370-101-0001", StationNach: "ЕРУНАКОВО",
			GruzpolS: "АЭ", Naznach: "АЭ", CargoGroup: "УГОЛЬ", CargoS: "УГОЛЬ КАМЕННЫЙ",
			StanNazn: "МЫС АСТАФЬЕВА", Status: ip(status),
		}
	}

	// Поезд X: плановый индекс перекрывает фактический (index_pp главнее),
	// три вагона, две подгруппы (у x3 другой получатель и группа груза).
	x1 := mk("X1", "62000001", "9370-101-9857", "БИКИН", 2)
	x1.IndexPp = "9379-786-9857"
	x1.NppVag, x1.Ves = ip(2), fp(69.5)
	x1.ProstDn, x1.ProstCh = ip(0), ip(5)
	x1.Invoice, x1.Owner = "ЭЛ318859", "ООО ОПЕРАТОР"
	x1.FreightExactName, x1.RodVagUch = "УГОЛЬ КАМЕННЫЙ МАРКИ Д", "ПВ"
	x1.DateNach, x1.DateDostav = lt(20, 10), lt(30, 0)
	x1.RasstStanNazn = ip(500)

	x2 := mk("X2", "62000002", "9370-101-9857", "БИКИН", 4)
	x2.IndexPp = "9379-786-9857"
	x2.NppVag, x2.Ves = ip(1), fp(70.0)
	x2.ProstDn, x2.ProstCh = ip(2), ip(3) // 51 ч — максимум по поезду
	x2.PlanJd, x2.PlanMsk = lt(31, 18), lt(31, 15)
	x2.ProgJd, x2.ProgMsk = lt(31, 20), lt(31, 17)
	x2.RaschJd, x2.RaschMsk = lt(31, 12), lt(31, 10)
	x2.TimeOp, x2.OperS = lt(29, 8), "ПРИБ"
	x2.RasstStanNazn = ip(500)

	x3 := mk("X3", "62000003", "9370-101-9857", "БИКИН", 2)
	x3.IndexPp = "9379-786-9857"
	x3.NppVag, x3.Ves = ip(3), fp(60.0)
	x3.GruzpolS, x3.Naznach, x3.CargoGroup = "УТ-1", "УТ-1", "МЕТАЛЛ"
	x3.RasstStanNazn = ip(500)

	// Отцепка Y: тот же нормализованный индекс, но другая станция операции —
	// отдельная строка; единственный вагон брошен (статус 5).
	y1 := mk("Y1", "62000004", "9370-101-9857", "ХАБАРОВСК-2", 5)
	y1.IndexPp = "9379-786-9857"
	y1.NppVag = ip(4)
	y1.RasstStanNazn = ip(700)

	// Поезд Z: на станции назначения (10 и 12) — arrived, rasst 0, сверху.
	z1 := mk("Z1", "62000005", "8631-880-9847", "МЫС АСТАФЬЕВА", 10)
	z1.NppVag, z1.RasstStanNazn = ip(1), ip(0)
	z2 := mk("Z2", "62000006", "8631-880-9847", "МЫС АСТАФЬЕВА", 12)
	z2.NppVag, z2.RasstStanNazn = ip(2), ip(0)

	// Вагон без индекса (Б/И) и без расстояния — своя группа, в конце списка.
	b1 := mk("B1", "62000007", "Б/И", "ЕРУНАКОВО", 0)
	b1.RasstStanNazn = nil

	repo := &fakeDislRepo{current: []domain.Dislocation{x1, x2, x3, y1, z1, z2, b1}}
	actual := service.NewActualCache(repo)
	require.NoError(t, actual.Load(context.Background()))

	svc := service.NewTrainsService(actual, nil)
	out := svc.Trains(context.Background())

	require.Len(t, out.Trains, 4)
	assert.Equal(t, 4, out.Total)

	// Сортировка: Z (0 км) → X (500) → Y (700) → Б/И (без rasst — в конец).
	z, x, y, b := out.Trains[0], out.Trains[1], out.Trains[2], out.Trains[3]
	assert.Equal(t, "8631-880-9847", z.Index)
	assert.Equal(t, "9379-786-9857", x.Index) // index_pp главнее index
	assert.Equal(t, "ХАБАРОВСК-2", y.StationOper)
	assert.Equal(t, "Б/И", b.Index)

	// Z: все вагоны на назначении → arrived; статус-большинство при равенстве —
	// меньший код (10 из пары 10/12).
	assert.True(t, z.Arrived)
	require.NotNil(t, z.Status)
	assert.Equal(t, 10, *z.Status)

	// X: большинство 2 из {2,4,2}; не arrived, не брошен; простой — максимум
	// (2 дн 3 ч от x2); вес — сумма; поля поезда — от вагона №1 (x2).
	require.NotNil(t, x.Status)
	assert.Equal(t, 2, *x.Status)
	assert.False(t, x.Arrived)
	assert.False(t, x.Broshen)
	assert.Equal(t, 3, x.VagonCount)
	assert.InDelta(t, 199.5, x.Ves, 0.01)
	require.NotNil(t, x.ProstDn)
	require.NotNil(t, x.ProstCh)
	assert.Equal(t, 2, *x.ProstDn)
	assert.Equal(t, 3, *x.ProstCh)
	require.NotNil(t, x.PlanJd)
	require.NotNil(t, x.ProgJd)
	require.NotNil(t, x.RaschJd)
	assert.Equal(t, "ПРИБ", x.OperS)
	assert.True(t, x.HasPlan)

	// X: две подгруппы (у x3 другой получатель/группа груза); вагоны по npp_vag.
	require.Len(t, x.SubGroups, 2)
	sgCoal := x.SubGroups[0]
	if sgCoal.CargoGroup != "УГОЛЬ" {
		sgCoal = x.SubGroups[1]
	}
	require.Len(t, sgCoal.Vagons, 2)
	assert.Equal(t, "62000002", sgCoal.Vagons[0].Vagon) // npp 1
	assert.Equal(t, "62000001", sgCoal.Vagons[1].Vagon) // npp 2
	assert.InDelta(t, 139.5, sgCoal.Ves, 0.01)

	// Вагон несёт накладную, собственника owner, марку груза и род вагона.
	v := sgCoal.Vagons[1]
	assert.Equal(t, "ЭЛ318859", v.Invoice)
	assert.Equal(t, "ООО ОПЕРАТОР", v.Owner)
	assert.Equal(t, "УГОЛЬ КАМЕННЫЙ МАРКИ Д", v.Freight)
	assert.Equal(t, "ПВ", v.RodVag)
	require.NotNil(t, v.DateDostav)

	// Y: единственный вагон брошен — статус 5 и флаг broshen.
	assert.True(t, y.Broshen)
	require.NotNil(t, y.Status)
	assert.Equal(t, 5, *y.Status)
	assert.False(t, y.Arrived)
}
