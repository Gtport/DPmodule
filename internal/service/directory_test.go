package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// stubDirRepo — in-memory реализация port.DirectoryRepository для тестов.
type stubDirRepo struct {
	stations   []domain.Station
	ops        []domain.CargoOperation
	marka      []domain.Marka
	ports      []domain.Ports
	routeSpeed []domain.RouteSpeed
	naznach    []domain.NaznachStation
}

func (s *stubDirRepo) LoadStations(context.Context) ([]domain.Station, error) {
	return s.stations, nil
}
func (s *stubDirRepo) LoadCargoOperations(context.Context) ([]domain.CargoOperation, error) {
	return s.ops, nil
}
func (s *stubDirRepo) LoadMarka(context.Context) ([]domain.Marka, error) { return s.marka, nil }
func (s *stubDirRepo) LoadPorts(context.Context) ([]domain.Ports, error) { return s.ports, nil }
func (s *stubDirRepo) LoadRouteSpeed(context.Context) ([]domain.RouteSpeed, error) {
	return s.routeSpeed, nil
}
func (s *stubDirRepo) LoadNaznachStation(context.Context) ([]domain.NaznachStation, error) {
	return s.naznach, nil
}

func TestDirectoryCache_LoadAndLookup(t *testing.T) {
	repo := &stubDirRepo{
		stations: []domain.Station{{Kod: 63710, Kod4: 6371, Name: "ХИМИЧЕСКАЯ", Road: "ВСЖД"}},
		ops:      []domain.CargoOperation{{Kod: 1, Oper: "ПОГРУЗКА", OperS: "Погр"}},
		marka: []domain.Marka{
			{Okpo: 1, StationKod: 2, CargoKod: 3, Shipper: "A"},
			{Okpo: 1, StationKod: 2, CargoKod: 3, Shipper: "B"}, // тот же ключ → срез из 2
		},
		ports: []domain.Ports{{Okpo: 1126022, Location: "МЫС АСТАФЬЕВА", NameS: "ГУТ-2"}},
		routeSpeed: []domain.RouteSpeed{
			// профиль по умолчанию (участки вперемешку — кэш должен отсортировать)
			{StationNach: "*", FromKm: 0, Speed: 27},
			{StationNach: "*", FromKm: 1364, Speed: 34},
			{StationNach: "*", FromKm: 911, Speed: 30},
			// переопределение станции
			{StationNach: "УЛАК", FromKm: 1364, Speed: 20},
		},
	}

	c := service.NewDirectoryCache(repo)
	require.NoError(t, c.Load(context.Background()))

	st, cargoOps, marka, ports, routeSpeed, naznach := c.Counts()
	assert.Equal(t, 1, st)
	assert.Equal(t, 1, cargoOps)
	assert.Equal(t, 1, marka) // 1 ключ (две записи под ним)
	assert.Equal(t, 1, ports)
	assert.Equal(t, 2, routeSpeed) // 2 профиля: '*' и 'УЛАК'
	assert.Equal(t, 0, naznach)    // перестановок не задано

	t.Run("station by kod / kod4", func(t *testing.T) {
		s, ok := c.GetStationByKod(63710)
		require.True(t, ok)
		assert.Equal(t, "ХИМИЧЕСКАЯ", s.Name)
		_, ok = c.GetStationByKod4(6371)
		assert.True(t, ok)
		_, ok = c.GetStationByKod(999)
		assert.False(t, ok)
	})

	t.Run("cargo operation", func(t *testing.T) {
		op, ok := c.GetCargoOperation(1)
		require.True(t, ok)
		assert.Equal(t, "Погр", op.OperS)
	})

	t.Run("marka composite key returns slice", func(t *testing.T) {
		mk, ok := c.GetMarkaByCompositeKey(1, 2, 3)
		require.True(t, ok)
		assert.Len(t, mk, 2)
		_, ok = c.GetMarkaByCompositeKey(9, 9, 9)
		assert.False(t, ok)
	})

	t.Run("port composite key", func(t *testing.T) {
		pr, ok := c.GetPortByCompositeKey(1126022, "МЫС АСТАФЬЕВА")
		require.True(t, ok)
		assert.Equal(t, "ГУТ-2", pr[0].NameS)
	})

	t.Run("route speed: default profile sorted desc", func(t *testing.T) {
		segs, ok := c.GetRouteSpeed("*", false)
		require.True(t, ok)
		require.Len(t, segs, 3)
		assert.Equal(t, 1364, segs[0].FromKm) // отсортировано по убыванию FromKm
		assert.Equal(t, 911, segs[1].FromKm)
		assert.Equal(t, 0, segs[2].FromKm)
	})

	t.Run("route speed: station override", func(t *testing.T) {
		segs, ok := c.GetRouteSpeed("УЛАК", false)
		require.True(t, ok)
		require.Len(t, segs, 1)
		assert.Equal(t, 20.0, segs[0].Speed)
	})

	t.Run("route speed: unknown station falls back to default", func(t *testing.T) {
		segs, ok := c.GetRouteSpeed("НЕИЗВЕСТНАЯ", false)
		require.True(t, ok)
		assert.Len(t, segs, 3) // вернулся профиль '*'
	})

	t.Run("route speed: missing profile entirely", func(t *testing.T) {
		_, ok := c.GetRouteSpeed("УЛАК", true) // is_bam не сидирован, '*' для bam тоже нет
		assert.False(t, ok)
	})
}
