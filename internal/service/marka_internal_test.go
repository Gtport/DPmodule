package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

// markaStubRepo — минимальный DirectoryRepository для S2-3: только marka + перестановки.
type markaStubRepo struct {
	marka   []domain.Marka
	naznach []domain.NaznachStation
}

func (markaStubRepo) LoadStations(context.Context) ([]domain.Station, error) { return nil, nil }
func (markaStubRepo) LoadCargoOperations(context.Context) ([]domain.CargoOperation, error) {
	return nil, nil
}
func (s markaStubRepo) LoadMarka(context.Context) ([]domain.Marka, error) { return s.marka, nil }
func (markaStubRepo) LoadPorts(context.Context) ([]domain.Ports, error)   { return nil, nil }
func (markaStubRepo) LoadRouteSpeed(context.Context) ([]domain.RouteSpeed, error) {
	return nil, nil
}
func (s markaStubRepo) LoadNaznachStation(context.Context) ([]domain.NaznachStation, error) {
	return s.naznach, nil
}

func markaDir(t *testing.T, marka []domain.Marka, nz []domain.NaznachStation) *DirectoryCache {
	t.Helper()
	c := NewDirectoryCache(markaStubRepo{marka: marka, naznach: nz})
	require.NoError(t, c.Load(context.Background()))
	return c
}

var markaFixture = []domain.Marka{
	{Okpo: 1, StationKod: 2, CargoKod: 3, Shipper: "ОТПР", CargoS: "УГОЛЬ", Client: "КЛ", CargoGroup: "УГ", Sms1: "уг"},
}

func TestEnrichFromMarka(t *testing.T) {
	dir := markaDir(t, markaFixture, nil)

	t.Run("полное совпадение", func(t *testing.T) {
		r := &domain.Dislocation{GruzotprOkpo: "1", CodeStationNach: "2", CodeCargo: "3"}
		assert.Equal(t, markaFull, enrichFromMarka(r, dir))
		assert.Equal(t, "ОТПР", r.Gruzotpr)
		assert.Equal(t, "УГОЛЬ", r.CargoS)
		assert.Equal(t, "КЛ", r.Client)
		assert.Equal(t, "УГ", r.CargoGroup)
		assert.Equal(t, "уг", r.CargoSms)
		assert.Equal(t, "уг", r.Sms1)
	})

	t.Run("частичное (ОКПО не известен) — станция+груз", func(t *testing.T) {
		r := &domain.Dislocation{GruzotprOkpo: "99", CodeStationNach: "2", CodeCargo: "3"}
		assert.Equal(t, markaPartial, enrichFromMarka(r, dir))
		assert.Equal(t, "ОТПР", r.Gruzotpr) // взят у отправителя с той же станцией+грузом
	})

	t.Run("ОКПО известен, но нет сочетания — без домысла", func(t *testing.T) {
		r := &domain.Dislocation{GruzotprOkpo: "1", CodeStationNach: "5", CodeCargo: "6"}
		assert.Equal(t, markaNone, enrichFromMarka(r, dir))
		assert.Empty(t, r.Gruzotpr)
	})

	t.Run("пустой ключевой компонент → none", func(t *testing.T) {
		r := &domain.Dislocation{GruzotprOkpo: "1", CodeStationNach: "2"} // нет CodeCargo
		assert.Equal(t, markaNone, enrichFromMarka(r, dir))
	})
}

func TestEnrichNaznach(t *testing.T) {
	nz := []domain.NaznachStation{
		{DestStation: "МЫС АСТАФЬЕВА", OriginStation: "ЛЕНИНСК-КУЗНЕЦКИЙ 2", Naznach: "АЭ", Enabled: true},
		{DestStation: "МЫС АСТАФЬЕВА", OriginStation: "ВЫКЛ", Naznach: "АЭ", Enabled: false}, // выключена — не грузится
		{DestStation: "МЫС АСТАФЬЕВА", OriginStation: "ПУСТО", Naznach: "", Enabled: true},   // пустой naznach — не грузится
	}
	dir := markaDir(t, nil, nz)

	t.Run("перестановка сработала", func(t *testing.T) {
		r := &domain.Dislocation{StanNazn: "МЫС АСТАФЬЕВА", StationNach: "ЛЕНИНСК-КУЗНЕЦКИЙ 2", GruzpolS: "ГУТ-2"}
		assert.True(t, enrichNaznach(r, dir))
		assert.Equal(t, "АЭ", r.Naznach) // не дефолтный ГУТ-2
	})

	t.Run("нет перестановки → дефолт GruzpolS", func(t *testing.T) {
		r := &domain.Dislocation{StanNazn: "МЫС АСТАФЬЕВА", StationNach: "ДРУГАЯ", GruzpolS: "ГУТ-2"}
		assert.False(t, enrichNaznach(r, dir))
		assert.Equal(t, "ГУТ-2", r.Naznach)
	})

	t.Run("другая станция назначения → дефолт", func(t *testing.T) {
		r := &domain.Dislocation{StanNazn: "НАХОДКА", StationNach: "ЛЕНИНСК-КУЗНЕЦКИЙ 2", GruzpolS: "УТ-1"}
		assert.False(t, enrichNaznach(r, dir))
		assert.Equal(t, "УТ-1", r.Naznach)
	})

	t.Run("выключенная/пустая перестановка → дефолт", func(t *testing.T) {
		r := &domain.Dislocation{StanNazn: "МЫС АСТАФЬЕВА", StationNach: "ВЫКЛ", GruzpolS: "ГУТ-2"}
		assert.False(t, enrichNaznach(r, dir))
		assert.Equal(t, "ГУТ-2", r.Naznach)
	})
}

func TestApplyMarkaEnrichment(t *testing.T) {
	nz := []domain.NaznachStation{
		{DestStation: "МЫС АСТАФЬЕВА", OriginStation: "ST-NACH", Naznach: "АЭ", Enabled: true},
	}
	dir := markaDir(t, markaFixture, nz)

	kept := []domain.Dislocation{
		// новый: без груза, с ключом marka + перестановкой назначения
		{Vagon: "V1", GruzotprOkpo: "1", CodeStationNach: "2", CodeCargo: "3",
			StanNazn: "МЫС АСТАФЬЕВА", StationNach: "ST-NACH", GruzpolS: "ГУТ-2"},
		// существующий: груз уже перенесён carry-over'ом — marka не трогаем, Naznach тоже
		{Vagon: "V2", Gruzotpr: "УЖЕ", Naznach: "УТ-1", GruzpolS: "УТ-1"},
		// новый без совпадения в marka — кандидат донорства S2-3c
		{Vagon: "V3", GruzotprOkpo: "7", CodeStationNach: "8", CodeCargo: "9",
			StanNazn: "НАХОДКА", StationNach: "X", GruzpolS: "УТ-1"},
	}
	st := applyMarkaEnrichment(kept, dir)

	assert.Equal(t, 2, st.Candidates)      // V1, V3
	assert.Equal(t, 1, st.FilledFull)      // V1
	assert.Equal(t, 1, st.MissedMarka)     // V3
	assert.Equal(t, 1, st.NaznachOverride) // V1

	assert.Equal(t, "ОТПР", kept[0].Gruzotpr)
	assert.Equal(t, "АЭ", kept[0].Naznach)   // перестановка
	assert.Equal(t, "УЖЕ", kept[1].Gruzotpr) // не тронут
	assert.Equal(t, "УТ-1", kept[1].Naznach) // не тронут
	assert.Empty(t, kept[2].Gruzotpr)        // marka не нашла
	assert.Equal(t, "УТ-1", kept[2].Naznach) // дефолт
}
