package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

func now0() domain.LocalTime { return domain.LocalTime{} }

// Новый вагон (нет в актуальной): первичная установка index/invoice.
func TestCarryOver_NewVagon(t *testing.T) {
	r := domain.Dislocation{Vagon: "N1", Index: "8649-882-9857", Invoice: "ЭТ123"}
	initNewVagon(&r)
	assert.Equal(t, "ЭТ123", r.InvoiceMain)
	assert.Equal(t, "8649-882-9857", r.IndexMain)
	assert.Equal(t, "8649-882-9857", r.IndexLast)
}

// Непрбывший (актуальный ≠10): выборочный перенос из актуальной.
func TestCarryOver_SelectedFields(t *testing.T) {
	st2 := 2
	ex := domain.Dislocation{
		Vagon: "V", ID: "old-id", IndexMain: "PARENT", IndexLast: "PREV", IndexPp: "PP",
		Gruzpol: "НМТП", GruzpolS: "ГУТ-2", Naznach: "ГУТ-2", Gruzotpr: "ОТПР",
		CargoS: "УГОЛЬ", CargoGroup: "УГОЛЬ", Client: "КЛИЕНТ", Sms1: "s1", Color: "#fff",
		Param1: "p1", InvoiceMain: "СВОДНАЯ", Status: &st2,
		Latitude: "42.8", Longitude: "132.9",
	}
	nw := domain.Dislocation{Vagon: "V", Index: "NEWIDX", Invoice: "ЭТ999"}

	require.False(t, enrichFromActual(&nw, &ex, now0()))

	assert.Equal(t, "old-id", nw.ID)
	assert.Equal(t, "PARENT", nw.IndexMain) // родительский из актуальной
	assert.Equal(t, "NEWIDX", nw.IndexLast) // ex.Index был пуст → текущий
	assert.Equal(t, "ГУТ-2", nw.GruzpolS)
	assert.Equal(t, "ОТПР", nw.Gruzotpr) // груз из актуальной (Gruzotpr≠"")
	assert.Equal(t, "УГОЛЬ", nw.CargoGroup)
	assert.Equal(t, "СВОДНАЯ", nw.InvoiceMain) // стабильная накладная
	assert.Equal(t, "42.8", nw.Latitude)       // координаты из актуальной (в новой пусто)
}

// Заморозки на статусе 10 нет: прибывший обновляется свежими данными РЖД
// (выборочный перенос), переход 10→12 не блокируется, квирк Index=IndexPp упразднён.
func TestCarryOver_NoFreezeOn10(t *testing.T) {
	st10, st12 := 10, 12
	ex := domain.Dislocation{
		Vagon: "V", ID: "arr-id", IndexPp: "PLAN-NITKA", Status: &st10,
		GruzpolS: "АЭ", Naznach: "АЭ", Gruzotpr: "ОТПР", InvoiceMain: "СВОДНАЯ",
	}
	// Свежая выгрузка: вагон опустел на назначении, Stage 1 дал 12.
	nw := domain.Dislocation{Vagon: "V", Index: "NEW", Invoice: "ЭТ1", Status: &st12}

	sticky := enrichFromActual(&nw, &ex, now0())

	assert.False(t, sticky)
	require.NotNil(t, nw.Status)
	assert.Equal(t, 12, *nw.Status)           // переход 10→12 прошёл
	assert.Equal(t, "arr-id", nw.ID)          // выборочный перенос из актуальной
	assert.Equal(t, "NEW", nw.Index)          // фактический индекс, не плановая нитка
	assert.Equal(t, "PLAN-NITKA", nw.IndexPp) // план-поля сохранены
	assert.Equal(t, "АЭ", nw.GruzpolS)
	assert.Equal(t, "ОТПР", nw.Gruzotpr)
	assert.Equal(t, "СВОДНАЯ", nw.InvoiceMain)
}

// Sticky 10: на той же станции пропал только date_prib (Stage 1 дал 9) → держим
// факт прибытия: статус 10, date_prib из актуальной, date_kon = ЖД-сутки операции.
func TestCarryOver_Sticky10(t *testing.T) {
	st10, st9a, st9b := 10, 9, 9
	prib := domain.NewLocalTime(time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC))
	jd := domain.NewLocalTime(time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC))
	ex := domain.Dislocation{Vagon: "V", Status: &st10, CodeStationOper: "986005", DatePrib: prib}
	nw := domain.Dislocation{Vagon: "V", Status: &st9a, CodeStationOper: "986005", DateOpJd: jd}

	sticky := enrichFromActual(&nw, &ex, now0())
	assert.True(t, sticky)
	require.NotNil(t, nw.Status)
	assert.Equal(t, 10, *nw.Status)
	require.NotNil(t, nw.DatePrib)
	assert.Equal(t, *prib, *nw.DatePrib)
	assert.Equal(t, jd, nw.DateKon)

	// станция операции сменилась → удержания нет, статус честный
	nw2 := domain.Dislocation{Vagon: "V", Status: &st9b, CodeStationOper: "770000"}
	assert.False(t, enrichFromActual(&nw2, &ex, now0()))
	assert.Equal(t, 9, *nw2.Status)
	assert.Nil(t, nw2.DatePrib)
}

// Sticky статус 5: брошенный на той же станции операции остаётся 5.
func TestCarryOver_Sticky5(t *testing.T) {
	st5, st2 := 5, 2
	ex := domain.Dislocation{Vagon: "V", Status: &st5, CodeStationOper: "770005"}
	nw := domain.Dislocation{Vagon: "V", Status: &st2, CodeStationOper: "770005"} // та же станция

	sticky := enrichFromActual(&nw, &ex, now0())
	assert.True(t, sticky)
	require.NotNil(t, nw.Status)
	assert.Equal(t, 5, *nw.Status) // статус удержан

	// смена станции → sticky не срабатывает
	nw2 := domain.Dislocation{Vagon: "V", Status: &st2, CodeStationOper: "999999"}
	assert.False(t, enrichFromActual(&nw2, &ex, now0()))
	assert.Equal(t, 2, *nw2.Status)
}

// Новые поля: всегда из актуальной, если там не пусто (важно для запасного ЛК).
func TestCarryOver_NewFields(t *testing.T) {
	st2 := 2
	ex := domain.Dislocation{
		Vagon: "V", Status: &st2, Gruzotpr: "X",
		CarOwnerName: "СОБСТВЕННИК", GtdNumber: "ГТД777", FreightExactName: "УГОЛЬ КАМЕННЫЙ",
		CarTrustedName: "АО НТК", CarTrustedOkpo: "46441703",
	}
	nw := domain.Dislocation{Vagon: "V"} // ЛК-срез: новые поля пусты

	enrichFromActual(&nw, &ex, now0())
	assert.Equal(t, "СОБСТВЕННИК", nw.CarOwnerName)
	assert.Equal(t, "ГТД777", nw.GtdNumber)
	assert.Equal(t, "УГОЛЬ КАМЕННЫЙ", nw.FreightExactName)
	assert.Equal(t, "АО НТК", nw.CarTrustedName)
	assert.Equal(t, "46441703", nw.CarTrustedOkpo)
}

// Переадресация: операторские поля переносятся из актуальной безусловно (поток
// РЖД их не присылает); пустые в актуальной → пустые в новой (после отмены).
func TestCarryOver_PereadrCarried(t *testing.T) {
	st2 := 2
	ex := domain.Dislocation{Vagon: "V", Status: &st2, PereadrType: "ext", PereadrPort: "ВАНИНО"}
	nw := domain.Dislocation{Vagon: "V"}

	enrichFromActual(&nw, &ex, now0())
	assert.Equal(t, "ext", nw.PereadrType)
	assert.Equal(t, "ВАНИНО", nw.PereadrPort)

	// после отмены (в актуальной пусто) — не «воскресает»
	ex2 := domain.Dislocation{Vagon: "V", Status: &st2}
	nw2 := domain.Dislocation{Vagon: "V", PereadrType: "ext", PereadrPort: "ФАНТОМ"}
	enrichFromActual(&nw2, &ex2, now0())
	assert.Equal(t, "", nw2.PereadrType)
	assert.Equal(t, "", nw2.PereadrPort)
}

// fixZeroRasst: RasstStanNazn=0 и вагон не на станции назначения → из актуальной.
func TestCarryOver_FixZeroRasst(t *testing.T) {
	st2, zero, valid := 2, 0, 500
	ex := domain.Dislocation{Vagon: "V", Status: &st2, RasstStanNazn: &valid}
	// в пути: станция назначения ≠ станция операции
	nw := domain.Dislocation{Vagon: "V", RasstStanNazn: &zero, StanNazn: "МЫС АСТАФЬЕВА", StationOper: "УЛАК"}
	enrichFromActual(&nw, &ex, now0())
	require.NotNil(t, nw.RasstStanNazn)
	assert.Equal(t, 500, *nw.RasstStanNazn)

	// на станции назначения (=операции) → 0 корректен, не трогаем
	z2 := 0
	nw2 := domain.Dislocation{Vagon: "V", RasstStanNazn: &z2, StanNazn: "МЫС АСТАФЬЕВА", StationOper: "МЫС АСТАФЬЕВА"}
	enrichFromActual(&nw2, &ex, now0())
	assert.Equal(t, 0, *nw2.RasstStanNazn)
}
