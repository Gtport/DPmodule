package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

// markaStubRepo — минимальный DirectoryRepository для внутренних тестов Stage 2/3
// (marka + перестановки + станции + профили скоростей).
type markaStubRepo struct {
	marka      []domain.Marka
	cargo      []domain.Cargo
	naznach    []domain.NaznachStation
	stations   []domain.Station
	routeSpeed []domain.RouteSpeed
}

func (s markaStubRepo) LoadStations(context.Context) ([]domain.Station, error) {
	return s.stations, nil
}
func (markaStubRepo) LoadCargoOperations(context.Context) ([]domain.CargoOperation, error) {
	return nil, nil
}
func (s markaStubRepo) LoadCargo(context.Context) ([]domain.Cargo, error) { return s.cargo, nil }
func (s markaStubRepo) LoadMarka(context.Context) ([]domain.Marka, error) { return s.marka, nil }
func (markaStubRepo) LoadPorts(context.Context) ([]domain.Ports, error)   { return nil, nil }
func (s markaStubRepo) LoadRouteSpeed(context.Context) ([]domain.RouteSpeed, error) {
	return s.routeSpeed, nil
}
func (s markaStubRepo) LoadNaznachStation(context.Context) ([]domain.NaznachStation, error) {
	return s.naznach, nil
}

func markaDir(t *testing.T, marka []domain.Marka, cargo []domain.Cargo, nz []domain.NaznachStation) *DirectoryCache {
	t.Helper()
	c := NewDirectoryCache(markaStubRepo{marka: marka, cargo: cargo, naznach: nz})
	require.NoError(t, c.Load(context.Background()))
	return c
}

// Ключ marka — по группе груза (000028); группу вагону даёт словарь cargo.
var markaFixture = []domain.Marka{
	{Okpo: 1, StationKod: 2, CargoGroup: "УГОЛЬ", Shipper: "ОТПР", Client: "КЛ", Sms1: "Улак", Sms3: "УЛАК"},
}

var cargoFixture = []domain.Cargo{
	{Kod: 161113, CargoGroup: "УГОЛЬ", CargoS: "УГОЛЬ Г", CargoSms: "Г"},
	{Kod: 161043, CargoGroup: "УГОЛЬ", CargoS: "КОНЦЕНТРАТ", CargoSms: "КОНЦ"},
}

func TestEnrichFromMarka(t *testing.T) {
	dir := markaDir(t, markaFixture, cargoFixture, nil)

	t.Run("строгое совпадение (ОКПО+станция+группа)", func(t *testing.T) {
		r := &domain.Dislocation{GruzotprOkpo: "1", CodeStationNach: "2", CargoGroup: "УГОЛЬ"}
		assert.True(t, enrichFromMarka(r, dir))
		assert.Equal(t, "ОТПР", r.Gruzotpr)
		assert.Equal(t, "КЛ", r.Client)
		assert.Equal(t, "Улак", r.Sms1)
		assert.Equal(t, "УЛАК", r.Sms3)
	})

	t.Run("новый код знакомой группы матчится (смысл переработки)", func(t *testing.T) {
		// Код 161043 в marka никогда не значился — но группа УГОЛЬ известна.
		r := &domain.Dislocation{GruzotprOkpo: "1", CodeStationNach: "2", CodeCargo: "161043"}
		reapplyCargoDict(r, dir)
		assert.True(t, enrichFromMarka(r, dir))
		assert.Equal(t, "ОТПР", r.Gruzotpr)
		assert.Equal(t, "КОНЦЕНТРАТ", r.CargoS) // имя груза — из словаря, не из marka
	})

	t.Run("чужой ОКПО на знакомой станции+группе — СТРОГО не матчится", func(t *testing.T) {
		// Раньше срабатывал частичный матч (станция+группа любого отправителя) —
		// подставлял атрибуцию чужого отправителя. Упразднён решением владельца.
		r := &domain.Dislocation{GruzotprOkpo: "99", CodeStationNach: "2", CargoGroup: "УГОЛЬ"}
		assert.False(t, enrichFromMarka(r, dir))
		assert.Empty(t, r.Gruzotpr)
	})

	t.Run("ОКПО известен, но нет сочетания — без домысла", func(t *testing.T) {
		r := &domain.Dislocation{GruzotprOkpo: "1", CodeStationNach: "5", CargoGroup: "МЕТАЛЛ"}
		assert.False(t, enrichFromMarka(r, dir))
		assert.Empty(t, r.Gruzotpr)
	})

	t.Run("пустая группа (порожний/код вне словаря) → не матчится", func(t *testing.T) {
		r := &domain.Dislocation{GruzotprOkpo: "1", CodeStationNach: "2"}
		assert.False(t, enrichFromMarka(r, dir))
	})
}

func TestEnrichNaznach(t *testing.T) {
	nz := []domain.NaznachStation{
		{DestStation: "МЫС АСТАФЬЕВА", OriginStation: "ЛЕНИНСК-КУЗНЕЦКИЙ 2", Naznach: "АЭ", Enabled: true},
		{DestStation: "МЫС АСТАФЬЕВА", OriginStation: "ВЫКЛ", Naznach: "АЭ", Enabled: false}, // выключена — не грузится
		{DestStation: "МЫС АСТАФЬЕВА", OriginStation: "ПУСТО", Naznach: "", Enabled: true},   // пустой naznach — не грузится
	}
	dir := markaDir(t, nil, nil, nz)

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
	dir := markaDir(t, markaFixture, cargoFixture, nz)

	kept := []domain.Dislocation{
		// новый: словарь даст группу УГОЛЬ → marka по группе + перестановка назначения
		{Vagon: "V1", GruzotprOkpo: "1", CodeStationNach: "2", CodeCargo: "161113",
			StanNazn: "МЫС АСТАФЬЕВА", StationNach: "ST-NACH", GruzpolS: "ГУТ-2"},
		// существующий: атрибуция перенесена carry-over'ом — marka не трогаем, Naznach
		// тоже; словарь по коду затирает перенесённые груз-поля, Sms2 пересчитывается
		{Vagon: "V2", Gruzotpr: "УЖЕ", Naznach: "УТ-1", GruzpolS: "УТ-1",
			CodeCargo: "161043", CargoS: "КОНЦЕНТР УГОЛЬН", Sms1: "РУК", Sms2: "старое"},
		// новый без совпадения в marka — кандидат донорства S2-3c
		{Vagon: "V3", GruzotprOkpo: "7", CodeStationNach: "8", CodeCargo: "161113",
			StanNazn: "НАХОДКА", StationNach: "X", GruzpolS: "УТ-1"},
		// порожний: кода нет — перенесённые груз-поля не трогаем
		{Vagon: "V4", Gruzotpr: "БЫЛ", Naznach: "УТ-1", GruzpolS: "УТ-1",
			CargoS: "ПРОШЛЫЙ ГРУЗ", CargoGroup: "УГОЛЬ", CargoSms: "КОНЦ", Sms1: "РУК"},
	}
	st := applyMarkaEnrichment(kept, dir)

	assert.Equal(t, 2, st.Candidates)      // V1, V3
	assert.Equal(t, 1, st.FilledFull)      // V1
	assert.Equal(t, 1, st.MissedMarka)     // V3
	assert.Equal(t, 1, st.NaznachOverride) // V1

	assert.Equal(t, "ОТПР", kept[0].Gruzotpr)
	assert.Equal(t, "УГОЛЬ Г", kept[0].CargoS) // из словаря
	assert.Equal(t, "Улак Г", kept[0].Sms2)    // расчёт: Sms1 marka + CargoSms словаря
	assert.Equal(t, "АЭ", kept[0].Naznach)     // перестановка

	assert.Equal(t, "УЖЕ", kept[1].Gruzotpr)      // не тронут
	assert.Equal(t, "КОНЦЕНТРАТ", kept[1].CargoS) // словарь — источник правды
	assert.Equal(t, "РУК КОНЦ", kept[1].Sms2)     // пересчитан
	assert.Equal(t, "УТ-1", kept[1].Naznach)      // не тронут

	assert.Empty(t, kept[2].Gruzotpr)        // marka не нашла
	assert.Equal(t, "УТ-1", kept[2].Naznach) // дефолт

	assert.Equal(t, "ПРОШЛЫЙ ГРУЗ", kept[3].CargoS) // порожний: перенос сохранён
	assert.Equal(t, "РУК КОНЦ", kept[3].Sms2)       // расчёт из перенесённых
}

// S2-3d: наследование бизнес-атрибуции по составу (IndexMain) при единогласии.
func TestApplyTrainInheritance(t *testing.T) {
	dir := markaDir(t, markaFixture, cargoFixture, nil)
	s2 := 2 // статус «в пути»

	t.Run("единогласный состав → сирота наследует, sms_2 со своей меткой", func(t *testing.T) {
		kept := []domain.Dislocation{
			// доноры: заматчились marka (ОКПО 1, станция 2, УГОЛЬ)
			{Vagon: "D1", GruzotprOkpo: "1", CodeStationNach: "2", CodeCargo: "161113", IndexMain: "IX-1", Status: &s2},
			{Vagon: "D2", GruzotprOkpo: "1", CodeStationNach: "2", CodeCargo: "161113", IndexMain: "IX-1", Status: &s2},
			// сирота: ОКПО нулевое, станция вне marka — но код груза свой, читаемый
			{Vagon: "S", GruzotprOkpo: "00000000", CodeStationNach: "999", CodeCargo: "161043", IndexMain: "IX-1", Status: &s2},
		}
		st := applyMarkaEnrichment(kept, dir)

		assert.Equal(t, 1, st.FilledByTrain)
		assert.Equal(t, 0, st.MissedMarka) // сирота закрыт составом
		assert.Equal(t, "ОТПР", kept[2].Gruzotpr)
		assert.Equal(t, "КЛ", kept[2].Client)
		assert.Equal(t, "Улак", kept[2].Sms1)
		assert.Equal(t, "УЛАК", kept[2].Sms3)
		assert.Equal(t, "00000000", kept[2].GruzotprOkpo) // сырое ОКПО не подделано
		assert.Equal(t, "Улак КОНЦ", kept[2].Sms2)        // sms_1 состава + СВОЯ метка груза
	})

	t.Run("сборный состав (разногласие) → не наследуем", func(t *testing.T) {
		kept := []domain.Dislocation{
			{Vagon: "D1", Gruzotpr: "ОТПР-А", IndexMain: "IX-2", Status: &s2, CodeCargo: "161113"},
			{Vagon: "D2", Gruzotpr: "ОТПР-Б", IndexMain: "IX-2", Status: &s2, CodeCargo: "161113"},
			{Vagon: "S", GruzotprOkpo: "00000000", CodeStationNach: "999", CodeCargo: "161113", IndexMain: "IX-2", Status: &s2},
		}
		st := applyMarkaEnrichment(kept, dir)
		assert.Equal(t, 0, st.FilledByTrain)
		assert.Equal(t, 1, st.MissedMarka) // остался кандидатом донорства
		assert.Empty(t, kept[2].Gruzotpr)
	})

	t.Run("несовпадение группы → не наследуем", func(t *testing.T) {
		kept := []domain.Dislocation{
			{Vagon: "D", Gruzotpr: "ОТПР", CargoGroup: "МЕТАЛЛ", IndexMain: "IX-3", Status: &s2},
			// сирота-уголь в металлическом составе
			{Vagon: "S", GruzotprOkpo: "0", CodeStationNach: "999", CodeCargo: "161113", IndexMain: "IX-3", Status: &s2},
		}
		st := applyMarkaEnrichment(kept, dir)
		assert.Equal(t, 0, st.FilledByTrain)
		assert.Empty(t, kept[1].Gruzotpr)
	})

	t.Run("порожний и статус 0 не участвуют", func(t *testing.T) {
		s0 := 0
		kept := []domain.Dislocation{
			{Vagon: "D", Gruzotpr: "ОТПР", CargoGroup: "УГОЛЬ", IndexMain: "IX-4", Status: &s2},
			{Vagon: "P", PorozhPriznak: "1", IndexMain: "IX-4", Status: &s2},                      // порожний
			{Vagon: "Z", GruzotprOkpo: "0", CodeCargo: "161113", IndexMain: "IX-4", Status: &s0}, // на ст. отправления
		}
		st := applyMarkaEnrichment(kept, dir)
		assert.Equal(t, 0, st.FilledByTrain)
		assert.Empty(t, kept[1].Gruzotpr)
		assert.Empty(t, kept[2].Gruzotpr)
	})
}
