package service

import (
	"testing"

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

// Прибывший (актуальный =10): полная замена, prost_dn свежий, Index=IndexPp.
func TestCarryOver_CopyAll(t *testing.T) {
	st10, pd := 10, 3
	ex := domain.Dislocation{
		Vagon: "V", ID: "arr-id", IndexPp: "PLAN-NITKA", Status: &st10,
		GruzpolS: "АЭ", StanNazn: "МЫС АСТАФЬЕВА", StationOper: "МЫС АСТАФЬЕВА",
		InvoiceMain: "СВОДНАЯ",
	}
	newProst := 7
	nw := domain.Dislocation{Vagon: "V", Index: "NEW", Invoice: "ЭТ1", ProstDn: &newProst}

	enrichFromActual(&nw, &ex, now0())

	assert.Equal(t, "arr-id", nw.ID)        // всё из актуальной
	assert.Equal(t, "PLAN-NITKA", nw.Index) // Index = IndexPp
	assert.Equal(t, "АЭ", nw.GruzpolS)
	require.NotNil(t, nw.ProstDn)
	assert.Equal(t, 7, *nw.ProstDn) // prost_dn свежий из новой
	assert.Equal(t, "СВОДНАЯ", nw.InvoiceMain)
	_ = pd
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
	}
	nw := domain.Dislocation{Vagon: "V"} // ЛК-срез: новые поля пусты

	enrichFromActual(&nw, &ex, now0())
	assert.Equal(t, "СОБСТВЕННИК", nw.CarOwnerName)
	assert.Equal(t, "ГТД777", nw.GtdNumber)
	assert.Equal(t, "УГОЛЬ КАМЕННЫЙ", nw.FreightExactName)
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
