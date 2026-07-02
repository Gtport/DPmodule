package service

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// DefaultRouteProfile — ключ station_nach профиля скоростей «по умолчанию»
// (аналог ветки default из switch в gtlogic). См. GetRouteSpeed.
const DefaultRouteProfile = "*"

// DirectoryCache — справочники обогащения в оперативной памяти. Грузятся один раз
// при старте (Load), читаются обогащением (Stage 1–2). Доступ под RWMutex — задел
// под горячую перезагрузку. Зеркалит DirectoryCache из gtlogic на минимальном срезе.
type DirectoryCache struct {
	repo port.DirectoryRepository

	mu              sync.RWMutex
	stations        map[int]domain.Station
	stationsByKod4  map[int]domain.Station
	cargoOperations map[int]domain.CargoOperation
	marka           map[string][]domain.Marka      // ключ MarkaKey (неуникален → срез)
	ports           map[string][]domain.Ports      // ключ PortKey (неуникален → срез)
	routeSpeed      map[string][]domain.RouteSpeed // ключ RouteSpeedKey; участки по убыванию FromKm
}

func NewDirectoryCache(repo port.DirectoryRepository) *DirectoryCache {
	return &DirectoryCache{
		repo:            repo,
		stations:        map[int]domain.Station{},
		stationsByKod4:  map[int]domain.Station{},
		cargoOperations: map[int]domain.CargoOperation{},
		marka:           map[string][]domain.Marka{},
		ports:           map[string][]domain.Ports{},
		routeSpeed:      map[string][]domain.RouteSpeed{},
	}
}

// MarkaKey / PortKey — составные ключи поиска (совпадают со схемой ключей gtlogic).
func MarkaKey(okpo, stationKod, cargoKod int64) string {
	return fmt.Sprintf("%d:%d:%d", okpo, stationKod, cargoKod)
}

func PortKey(okpo int64, location string) string {
	return fmt.Sprintf("%d:%s", okpo, location)
}

func RouteSpeedKey(stationNach string, isBam bool) string {
	return fmt.Sprintf("%s:%t", stationNach, isBam)
}

// Load загружает все справочники из хранилища и атомарно заменяет содержимое кэша.
// Вызывать при старте (и в будущем — при перезагрузке).
func (c *DirectoryCache) Load(ctx context.Context) error {
	stations, err := c.repo.LoadStations(ctx)
	if err != nil {
		return fmt.Errorf("load stations: %w", err)
	}
	ops, err := c.repo.LoadCargoOperations(ctx)
	if err != nil {
		return fmt.Errorf("load cargo_operations: %w", err)
	}
	marka, err := c.repo.LoadMarka(ctx)
	if err != nil {
		return fmt.Errorf("load marka: %w", err)
	}
	ports, err := c.repo.LoadPorts(ctx)
	if err != nil {
		return fmt.Errorf("load ports: %w", err)
	}
	routeSpeed, err := c.repo.LoadRouteSpeed(ctx)
	if err != nil {
		return fmt.Errorf("load route_speed: %w", err)
	}

	st := make(map[int]domain.Station, len(stations))
	st4 := make(map[int]domain.Station, len(stations))
	for _, s := range stations {
		st[s.Kod] = s
		st4[s.Kod4] = s
	}
	co := make(map[int]domain.CargoOperation, len(ops))
	for _, o := range ops {
		co[o.Kod] = o
	}
	mk := make(map[string][]domain.Marka)
	for _, m := range marka {
		k := MarkaKey(m.Okpo, m.StationKod, m.CargoKod)
		mk[k] = append(mk[k], m)
	}
	pr := make(map[string][]domain.Ports)
	for _, p := range ports {
		k := PortKey(p.Okpo, p.Location)
		pr[k] = append(pr[k], p)
	}
	rs := make(map[string][]domain.RouteSpeed)
	for _, r := range routeSpeed {
		k := RouteSpeedKey(r.StationNach, r.IsBam)
		rs[k] = append(rs[k], r)
	}
	// Участки — по убыванию FromKm: потребитель (Stage 2) идёт от дальнего к ближнему.
	for k := range rs {
		segs := rs[k]
		sort.Slice(segs, func(i, j int) bool { return segs[i].FromKm > segs[j].FromKm })
	}

	c.mu.Lock()
	c.stations = st
	c.stationsByKod4 = st4
	c.cargoOperations = co
	c.marka = mk
	c.ports = pr
	c.routeSpeed = rs
	c.mu.Unlock()
	return nil
}

// Counts — сводка по числу ключей (для логов после загрузки).
func (c *DirectoryCache) Counts() (stations, cargoOps, marka, ports, routeSpeed int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.stations), len(c.cargoOperations), len(c.marka), len(c.ports), len(c.routeSpeed)
}

// ──────────────────────────────── lookup ────────────────────────────────

func (c *DirectoryCache) GetStationByKod(kod int) (domain.Station, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.stations[kod]
	return s, ok
}

func (c *DirectoryCache) GetStationByKod4(kod4 int) (domain.Station, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.stationsByKod4[kod4]
	return s, ok
}

func (c *DirectoryCache) GetCargoOperation(kod int) (domain.CargoOperation, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	o, ok := c.cargoOperations[kod]
	return o, ok
}

func (c *DirectoryCache) GetMarkaByCompositeKey(okpo, stationKod, cargoKod int64) ([]domain.Marka, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.marka[MarkaKey(okpo, stationKod, cargoKod)]
	return m, ok
}

func (c *DirectoryCache) GetPortByCompositeKey(okpo int64, location string) ([]domain.Ports, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.ports[PortKey(okpo, location)]
	return p, ok
}

// GetRouteSpeed возвращает участки скоростного профиля (по убыванию FromKm) для
// станции отправления: сначала точный профиль (stationNach, isBam), при отсутствии —
// профиль по умолчанию (DefaultRouteProfile, isBam). Это data-driven аналог
// switch/default из gtlogic. Второе значение — найден ли профиль вообще.
func (c *DirectoryCache) GetRouteSpeed(stationNach string, isBam bool) ([]domain.RouteSpeed, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if segs, ok := c.routeSpeed[RouteSpeedKey(stationNach, isBam)]; ok {
		return segs, true
	}
	segs, ok := c.routeSpeed[RouteSpeedKey(DefaultRouteProfile, isBam)]
	return segs, ok
}
