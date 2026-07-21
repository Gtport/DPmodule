package service

// Golden-тесты сборки «Истории движения вагона»: свёртка операций по визитам
// станций, матч справочников (станция/операция) и нормализация индекса.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

// trailDirRepo — минимальный справочник для сборки трейла (станции + операции).
type trailDirRepo struct{}

func (trailDirRepo) LoadStations(context.Context) ([]domain.Station, error) {
	return []domain.Station{
		{Kod: 930000, Kod4: 9300, Name: "ИРКУТСК-СОРТИРОВОЧНЫЙ", Road: "ВСЖД"},
		{Kod: 984700, Kod4: 9847, Name: "НАХОДКА", Road: "ДВЖД"},
	}, nil
}

func (trailDirRepo) LoadCargoOperations(context.Context) ([]domain.CargoOperation, error) {
	return []domain.CargoOperation{
		{Kod: 2, Oper: "ОТПРАВЛЕНИЕ ВАГОНА СО СТАНЦИИ", OperS: "Отправление"},
		{Kod: 4, Oper: "ВКЛЮЧЕНИЕ ВАГОНА В ПОЕЗД", OperS: "Формирование"},
	}, nil
}

func (trailDirRepo) LoadCargo(context.Context) ([]domain.Cargo, error) { return nil, nil }
func (trailDirRepo) LoadMarka(context.Context) ([]domain.Marka, error) { return nil, nil }
func (trailDirRepo) LoadPorts(context.Context) ([]domain.Ports, error) {
	return []domain.Ports{{Okpo: 1126022, Location: "НАХОДКА", NameS: "УТ-1", ProviderClient: "nmtp"}}, nil
}
func (trailDirRepo) LoadRouteSpeed(context.Context) ([]domain.RouteSpeed, error) { return nil, nil }
func (trailDirRepo) LoadNaznachStation(context.Context) ([]domain.NaznachStation, error) {
	return nil, nil
}
func (trailDirRepo) UpdateNaznachStationNaznach(context.Context, string, string, string) error {
	return nil
}

func trailDir(t *testing.T) *DirectoryCache {
	t.Helper()
	c := NewDirectoryCache(trailDirRepo{})
	require.NoError(t, c.Load(context.Background()))
	return c
}

func trailTime(s string) domain.LocalTime {
	ts, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		panic(err)
	}
	return domain.LocalTime(ts)
}

func trailRow() domain.VagonHistory {
	d := trailTime("2026-07-01T00:00:00")
	return domain.VagonHistory{ID: "63123456/930000/01.07.2026", Vagon: "63123456", DateNachD: &d, GruzpolS: "УТ-1"}
}

// Визит — непрерывная серия операций на станции: возврат на ту же станцию
// позже даёт ОТДЕЛЬНУЮ строку (решение владельца), хронология не ломается.
func TestBuildTrailView_VisitsAreConsecutiveRuns(t *testing.T) {
	ops := []domain.VagonOperation{
		{DateOp: trailTime("2026-07-02T08:16:00"), KopVmd: "4", StanOp: "930000", IndexPoezd: "930000123984700"},
		{DateOp: trailTime("2026-07-02T12:00:00"), KopVmd: "9", StanOp: "930000"},
		{DateOp: trailTime("2026-07-02T19:40:00"), KopVmd: "2", StanOp: "930000"},
		{DateOp: trailTime("2026-07-03T04:10:00"), KopVmd: "4", StanOp: "984700"},
		{DateOp: trailTime("2026-07-09T11:02:00"), KopVmd: "2", StanOp: "930000"},
	}
	v := buildTrailView(trailRow(), ops, trailDir(t))

	require.Len(t, v.Visits, 3)
	assert.Equal(t, 5, v.Count)
	assert.Equal(t, trailTime("2026-07-02T08:16:00"), *v.From)
	assert.Equal(t, trailTime("2026-07-09T11:02:00"), *v.To)

	// Первый визит: три операции, показываем первую и последнюю.
	assert.Equal(t, "ИРКУТСК-СОРТИРОВОЧНЫЙ", v.Visits[0].Station)
	assert.Equal(t, "ВСЖД", v.Visits[0].Road)
	assert.Equal(t, 3, v.Visits[0].Count)
	assert.Equal(t, trailTime("2026-07-02T08:16:00"), v.Visits[0].First.DateOp)
	assert.Equal(t, trailTime("2026-07-02T19:40:00"), v.Visits[0].Last.DateOp)
	assert.Len(t, v.Visits[0].Ops, 3) // все операции визита — под разворот и в Excel

	// Второй визит — другая станция; третий — возврат на первую отдельной строкой.
	assert.Equal(t, "НАХОДКА", v.Visits[1].Station)
	assert.Equal(t, "930000", v.Visits[2].StanOp)
	assert.Equal(t, 1, v.Visits[2].Count)
	assert.Equal(t, v.Visits[2].First, v.Visits[2].Last)
}

// Матч справочников: имена операций из cargo_operations, неизвестный код —
// пустые имена (код остаётся виден), станции вне справочника — без имени.
func TestBuildTrailView_DictionaryMatch(t *testing.T) {
	ops := []domain.VagonOperation{
		{DateOp: trailTime("2026-07-02T08:16:00"), KopVmd: "4", StanOp: "930000"},
		{DateOp: trailTime("2026-07-03T04:10:00"), KopVmd: "77", StanOp: "111111"},
	}
	v := buildTrailView(trailRow(), ops, trailDir(t))

	require.Len(t, v.Visits, 2)
	assert.Equal(t, "ВКЛЮЧЕНИЕ ВАГОНА В ПОЕЗД", v.Visits[0].First.Oper)
	assert.Equal(t, "Формирование", v.Visits[0].First.OperS)

	assert.Equal(t, "77", v.Visits[1].First.KopVmd)
	assert.Empty(t, v.Visits[1].First.Oper)
	assert.Equal(t, "111111", v.Visits[1].StanOp)
	assert.Empty(t, v.Visits[1].Station)
}

// Индекс: 15 цифр → XXXX-XXX-XXXX; «не в поезде» (пусто) → «Б/И».
func TestBuildTrailView_IndexNormalized(t *testing.T) {
	ops := []domain.VagonOperation{
		{DateOp: trailTime("2026-07-02T08:16:00"), KopVmd: "4", StanOp: "930000", IndexPoezd: "930000123984700"},
		{DateOp: trailTime("2026-07-02T12:00:00"), KopVmd: "2", StanOp: "930000", IndexPoezd: ""},
	}
	v := buildTrailView(trailRow(), ops, trailDir(t))

	require.Len(t, v.Visits, 1)
	assert.Equal(t, "9300-123-9847", v.Visits[0].First.Index)
	assert.Equal(t, "Б/И", v.Visits[0].Last.Index)
}

// Пустой трейл: период не заполняется — интерфейс сразу идёт в АСУ.
func TestBuildTrailView_Empty(t *testing.T) {
	v := buildTrailView(trailRow(), nil, trailDir(t))
	assert.Equal(t, 0, v.Count)
	assert.Nil(t, v.From)
	assert.Nil(t, v.To)
	assert.Empty(t, v.Visits)
	assert.Equal(t, "63123456", v.Vagon)
	assert.Equal(t, "УТ-1", v.Terminal)
}

// Клиент провайдера — по терминалу рейса (gruzpol_s → ports.name_s), снимок
// дислокации и ОКПО не нужны: вагон мог уже выбыть.
func TestClientForTerminal(t *testing.T) {
	s := &VagonOpService{dir: trailDir(t)}
	assert.Equal(t, "nmtp", s.clientForTerminal("УТ-1"))
	assert.Empty(t, s.clientForTerminal("НЕТ-ТАКОГО"))
	assert.Empty(t, s.clientForTerminal(""))
}
