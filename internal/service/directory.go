package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	cargo           map[int64]domain.Cargo           // код груза ЕТСНГ → группа/имя/метка (Stage 1)
	marka           map[string][]domain.Marka        // ключ MarkaKey (неуникален → срез)
	markaByStation  map[int64][]domain.Marka         // код станции отправления → записи (частичный матч S2-3)
	markaOkpos      map[int64]struct{}               // множество ОКПО в marka (проверка «ОКПО известен»)
	ports           map[string][]domain.Ports        // ключ PortKey (неуникален → срез)
	portsByOkpo     map[int64][]domain.Ports         // ОКПО → терминалы (для приёма ЛК: «чей файл»)
	portsByNameS    map[string]domain.Ports          // краткое имя причала (NameS) → терминал (Stage 4: станция+pc)
	planTargets     map[string]map[string]struct{}   // plan_code → множество NameS (площадки плана)
	routeSpeed      map[string][]domain.RouteSpeed   // ключ RouteSpeedKey; участки по убыванию FromKm
	naznachStation  map[string]domain.NaznachStation // ключ NaznachKey; только enabled + непустой naznach (§3.17)
}

func NewDirectoryCache(repo port.DirectoryRepository) *DirectoryCache {
	return &DirectoryCache{
		repo:            repo,
		stations:        map[int]domain.Station{},
		stationsByKod4:  map[int]domain.Station{},
		cargoOperations: map[int]domain.CargoOperation{},
		cargo:           map[int64]domain.Cargo{},
		marka:           map[string][]domain.Marka{},
		markaByStation:  map[int64][]domain.Marka{},
		markaOkpos:      map[int64]struct{}{},
		ports:           map[string][]domain.Ports{},
		portsByOkpo:     map[int64][]domain.Ports{},
		portsByNameS:    map[string]domain.Ports{},
		planTargets:     map[string]map[string]struct{}{},
		routeSpeed:      map[string][]domain.RouteSpeed{},
		naznachStation:  map[string]domain.NaznachStation{},
	}
}

// NaznachKey — составной ключ перестановки назначения: (станция назначения, станция
// отправления). Разделитель — управляющий символ, в именах станций не встречается.
func NaznachKey(destStation, originStation string) string {
	return destStation + "\x1f" + originStation
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
	cargo, err := c.repo.LoadCargo(ctx)
	if err != nil {
		return fmt.Errorf("load cargo: %w", err)
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
	naznach, err := c.repo.LoadNaznachStation(ctx)
	if err != nil {
		return fmt.Errorf("load naznach_station: %w", err)
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
	cg := make(map[int64]domain.Cargo, len(cargo))
	for _, g := range cargo {
		cg[g.Kod] = g
	}
	mk := make(map[string][]domain.Marka)
	mkByStation := make(map[int64][]domain.Marka)
	mkOkpos := make(map[int64]struct{})
	for _, m := range marka {
		k := MarkaKey(m.Okpo, m.StationKod, m.CargoKod)
		mk[k] = append(mk[k], m)
		mkByStation[m.StationKod] = append(mkByStation[m.StationKod], m)
		mkOkpos[m.Okpo] = struct{}{}
	}
	// Перестановки назначения: в кэш только включённые с непустым naznach — иначе
	// поиск возвращает «не найдено», и enrichFromNaznachStation откатывается к GruzpolS.
	nz := make(map[string]domain.NaznachStation, len(naznach))
	for _, n := range naznach {
		if !n.Enabled || n.Naznach == "" {
			continue
		}
		nz[NaznachKey(n.DestStation, n.OriginStation)] = n
	}
	pr := make(map[string][]domain.Ports)
	pbo := make(map[int64][]domain.Ports)
	pbn := make(map[string]domain.Ports)
	// planTargets: plan_code → множество кратких имён причалов (NameS). Целевой набор
	// площадок плана подвода строится из данных, а не хардкодом (замена
	// isMaTargetNaznachOrGruzpolS эталона). Пустые plan_code/NameS пропускаем.
	pt := make(map[string]map[string]struct{})
	for _, p := range ports {
		k := PortKey(p.Okpo, p.Location)
		pr[k] = append(pr[k], p)
		pbo[p.Okpo] = append(pbo[p.Okpo], p)
		if ns := strings.TrimSpace(p.NameS); ns != "" {
			pbn[ns] = p // NameS уникален (площадка причала); для Stage 4 — станция + pc_*
		}

		code := strings.TrimSpace(p.PlanCode)
		name := strings.TrimSpace(p.NameS)
		if code == "" || name == "" {
			continue
		}
		if pt[code] == nil {
			pt[code] = make(map[string]struct{})
		}
		pt[code][name] = struct{}{}
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
	c.cargo = cg
	c.marka = mk
	c.markaByStation = mkByStation
	c.markaOkpos = mkOkpos
	c.ports = pr
	c.portsByOkpo = pbo
	c.portsByNameS = pbn
	c.planTargets = pt
	c.routeSpeed = rs
	c.naznachStation = nz
	c.mu.Unlock()
	return nil
}

// Counts — сводка по числу ключей (для логов после загрузки).
func (c *DirectoryCache) Counts() (stations, cargoOps, cargo, marka, ports, routeSpeed, naznach int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.stations), len(c.cargoOperations), len(c.cargo), len(c.marka), len(c.ports), len(c.routeSpeed), len(c.naznachStation)
}

// ──────────────────────────────── lookup ────────────────────────────────

// TargetNaznach возвращает множество кратких имён причалов (NameS), относящихся к
// плану подвода planCode — целевые площадки для фильтра дислокации и матча. Набор
// строится из ports.plan_code (не хардкод). Возвращается копия (безопасно после
// снятия блокировки). Для неизвестного plan_code — пустое непустое множество.
func (c *DirectoryCache) TargetNaznach(planCode string) map[string]struct{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	src := c.planTargets[planCode]
	out := make(map[string]struct{}, len(src))
	for name := range src {
		out[name] = struct{}{}
	}
	return out
}

// PlanCodes возвращает отсортированный список кодов планов подвода (ports.plan_code),
// у которых есть целевые площадки — для перечисления в статус-панели (не хардкод).
func (c *DirectoryCache) PlanCodes() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.planTargets))
	for code := range c.planTargets {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

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

// GetCargoByKod — груз по коду ЕТСНГ (Stage 1: группа/краткое имя/метка).
func (c *DirectoryCache) GetCargoByKod(kod int64) (domain.Cargo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	g, ok := c.cargo[kod]
	return g, ok
}

func (c *DirectoryCache) GetMarkaByCompositeKey(okpo, stationKod, cargoKod int64) ([]domain.Marka, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.marka[MarkaKey(okpo, stationKod, cargoKod)]
	return m, ok
}

// GetMarkaByStationAndCargo — записи marka по (станция отправления + груз), любой ОКПО.
// Для частичного матча S2-3, когда ОКПО грузоотправителя в marka не известен (§3.17).
func (c *DirectoryCache) GetMarkaByStationAndCargo(stationKod, cargoKod int64) []domain.Marka {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []domain.Marka
	for _, m := range c.markaByStation[stationKod] {
		if m.CargoKod == cargoKod {
			out = append(out, m)
		}
	}
	return out
}

// MarkaHasOkpo — известен ли ОКПО грузоотправителя в marka вообще (в любом сочетании).
// Частичный матч по станции+грузу допускается ТОЛЬКО когда ОКПО не известен (паритет
// с gtlogic: для известного отправителя пробел в станции/грузе не домысливаем).
func (c *DirectoryCache) MarkaHasOkpo(okpo int64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.markaOkpos[okpo]
	return ok
}

// GetNaznach — площадка назначения по (станция назначения, станция отправления).
// Возвращает только включённые перестановки с непустым naznach; иначе (false)
// вызывающий откатывается к GruzpolS (§3.17).
func (c *DirectoryCache) GetNaznach(destStation, originStation string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n, ok := c.naznachStation[NaznachKey(destStation, originStation)]
	if !ok {
		return "", false
	}
	return n.Naznach, true
}

func (c *DirectoryCache) GetPortByCompositeKey(okpo int64, location string) ([]domain.Ports, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.ports[PortKey(okpo, location)]
	return p, ok
}

// PortsByOkpo возвращает все терминалы юр.лица по ОКПО грузополучателя (окпо не
// уникален: у одного ОКПО может быть несколько терминалов на разных станциях).
// Используется приёмом ЛК для контроля «чей файл» (файл ЛК — на юр.лицо/ОКПО).
func (c *DirectoryCache) PortsByOkpo(okpo int64) ([]domain.Ports, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.portsByOkpo[okpo]
	return p, ok
}

// PortByNameS возвращает терминал по краткому имени причала (NameS: АЭ/ГУТ-2/УТ-1).
// Для Stage 4: по Naznach записи находим станцию (StationCode — пул слотов) и
// перерабатывающую способность pc_* (интервал). NameS уникален (одна площадка).
func (c *DirectoryCache) PortByNameS(nameS string) (domain.Ports, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.portsByNameS[strings.TrimSpace(nameS)]
	return p, ok
}

// EnabledOkpos возвращает отсортированное множество ОКПО, у которых есть хотя бы
// один активный терминал. Для контроля приёма ЛК: какие грузополучатели ожидаются
// (пока единственный канал 'lk' питает всех; при связке port→data_source сузим).
func (c *DirectoryCache) EnabledOkpos() []int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]int64, 0, len(c.portsByOkpo))
	for okpo, ports := range c.portsByOkpo {
		for _, p := range ports {
			if p.Enabled {
				out = append(out, okpo)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
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
