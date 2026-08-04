package service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// Enricher — построчное обогащение батча дислокации из справочников. Stage 1
// целиком: станции → идентификация порта + фильтр → операции → статусы/производные.
// НЕ обращается к предыдущему снимку (это Stage 2). Читает справочники из
// DirectoryCache, без БД и сети. Порядок и квирки — как в gtlogic (см. §3.13).
type Enricher struct {
	dir *DirectoryCache
}

func NewEnricher(dir *DirectoryCache) *Enricher { return &Enricher{dir: dir} }

// Stage1Config — настроечные пороги производных расчётов (не хардкод).
type Stage1Config struct {
	CutoffHour  int             // порог часа для date_op_jd (date_cutoff_hour профиля); ≤0 → 18
	ProstDnMin  int             // порог простоя в сутках → статус 4 (client_settings)
	ProstChMin  int             // порог простоя в часах → статус 4 (client_settings)
	ExcludeOper map[string]bool // нерабочий парк: коды операций, вычёркивающие вагон из снимка (StatusPolicy.ExcludedOperSet)
}

// Stage1Stats — диагностика прогона Stage 1.
type Stage1Stats struct {
	Input              int         // записей на входе
	Kept               int         // осталось после фильтра включённых портов
	NaznEnriched       int         // с заполненной станцией назначения
	PortUnresolved     int         // отброшено: (ОКПО+станция) не резолвится
	PortDisabled       int         // отброшено: порт выключен
	OperExcluded       int         // отброшено: нерабочий парк (код операции из exclude_oper_codes)
	StationsNotFound   []int       // коды станций вне справочника
	OperationsNotFound []int       // коды операций вне справочника
	CargoNotFound      []int       // коды грузов вне словаря cargo
	StatusDist         map[int]int // распределение статусов
}

// Stage1 — вся построчная обработка нового батча, по порядку:
//  1. станции (коды → имена; неизвестный справочнику код гасит только свои поля,
//     разбор остальных станций записи не прерывает);
//  2. идентификация порта (ОКПО+StanNazn → GruzpolS): нерезолвнутый порт запись
//     НЕ выбрасывает (как в gtlogic), отбрасывается только ВЫКЛЮЧЕННЫЙ порт;
//  3. груз из словаря cargo (CodeCargo → CargoGroup/CargoS/CargoSms) — каждому
//     оставшемуся вагону, независимо от отправителя (marka в Stage 2 добирает
//     только бизнес-атрибуцию: отправитель/клиент/sms);
//  4. операции (Oper/OperS) — только на оставшихся;
//  5. статусы и производные (status, date_op/date_op_jd, date_kon, delay, id_disl,
//     id_status4/5).
//
// Возвращает отфильтрованный обогащённый набор («новую мапу») и статистику. «Сейчас»
// для delay — по Москве (clock.Now()). Stage 2 (сравнение с актуальным снимком,
// carry-over, marka для новых вагонов, очереди) — отдельная фаза.
func (e *Enricher) Stage1(records []domain.Dislocation, cfg Stage1Config) ([]domain.Dislocation, Stage1Stats) {
	if cfg.CutoffHour <= 0 {
		cfg.CutoffHour = 18
	}
	now := time.Time(clock.Now())
	stationsNF := map[int]struct{}{}
	opsNF := map[int]struct{}{}
	cargoNF := map[int]struct{}{}
	st := Stage1Stats{Input: len(records), StatusDist: map[int]int{}}
	kept := make([]domain.Dislocation, 0, len(records))

	for i := range records {
		r := records[i]

		// 0. Нерабочий парк (решение владельца 27.07.2026): вагон с кодом
		// операции из exclude_oper_codes (дефолт 72 «Списание») в снимок не
		// попадает — его нет ни на экранах, ни в задержках, ни в прогнозах.
		if cfg.ExcludeOper[strings.TrimSpace(r.CodeOper)] {
			st.OperExcluded++
			continue
		}

		// 1. Станции.
		e.enrichStations(&r, stationsNF)
		if r.StanNazn != "" {
			st.NaznEnriched++
		}

		// 2. Идентификация порта + фильтр.
		if !e.identifyPort(&r, &st) {
			continue
		}

		// 3. Груз из словаря cargo (только на оставшихся).
		e.enrichCargo(&r, cargoNF)

		// 4. Операции (только на оставшихся).
		e.enrichOperation(&r, opsNF)

		// 5. Статусы и производные.
		deriveDates(&r, cfg.CutoffHour)
		status := computeStatus(&r, cfg.ProstDnMin, cfg.ProstChMin, e.dir.PorozhInbound(&r))
		r.Status = &status
		st.StatusDist[status]++
		switch status {
		case 5:
			r.IdStatus5 = brosKey(&r)
		case 4:
			r.IdStatus4 = brosKey(&r)
		}
		r.DateKon = computeDateKon(&r, status)
		r.Delay = computeDelay(r.DateDostav, now)
		r.IdDisl = computeIdDisl(&r)

		kept = append(kept, r)
	}

	st.Kept = len(kept)
	st.StationsNotFound = sortedKeys(stationsNF)
	st.OperationsNotFound = sortedKeys(opsNF)
	st.CargoNotFound = sortedKeys(cargoNF)
	return kept, st
}

// enrichCargo заполняет груз-поля из словаря cargo по коду ЕТСНГ (CodeCargo).
// Универсальная идентичность груза — не зависит от отправителя, поэтому Stage 1;
// бизнес-атрибуцию (Gruzotpr/Client/Sms) добирает marka в Stage 2. Пустой код
// (порожний вагон) — не ошибка; код вне словаря — счётчик CargoNotFound.
func (e *Enricher) enrichCargo(r *domain.Dislocation, notFound map[int]struct{}) {
	if r.CodeCargo == "" {
		return
	}
	kod, err := strconv.ParseInt(r.CodeCargo, 10, 64)
	if err != nil {
		return
	}
	g, ok := e.dir.GetCargoByKod(kod)
	if !ok {
		notFound[int(kod)] = struct{}{}
		return
	}
	r.CargoGroup = g.CargoGroup
	r.CargoS = g.CargoS
	r.CargoSms = g.CargoSms
}

// identifyPort идентифицирует порт по составному ключу (ОКПО грузополучателя +
// StanNazn) и заполняет Gruzpol/GruzpolS. Возвращает false ТОЛЬКО когда порт
// найден, но выключен — такую запись Stage 1 отбрасывает (выключенный терминал
// не показываем осознанно).
//
// Не резолвится вовсе (нет ОКПО, нет StanNazn, пары нет в ports) — запись
// ОСТАЁТСЯ в снимке с пустым терминалом, как в gtlogic (enrichFromPorts там
// просто выходит, ничего не заполнив). Решение владельца 03.08.2026: вагон
// нельзя терять из-за справочника — пропажа в дислокации означает, что он
// физически выпал из выгрузки, а не что мы не смогли его разложить. Счётчик
// PortUnresolved остаётся: это диагностика неполноты справочников.
func (e *Enricher) identifyPort(r *domain.Dislocation, st *Stage1Stats) bool {
	if r.GruzpolOkpo == "" || r.StanNazn == "" {
		st.PortUnresolved++
		return true
	}
	okpo, err := strconv.ParseInt(r.GruzpolOkpo, 10, 64)
	if err != nil {
		st.PortUnresolved++
		return true
	}
	ports, ok := e.dir.GetPortByCompositeKey(okpo, r.StanNazn)
	if !ok || len(ports) == 0 {
		st.PortUnresolved++
		return true
	}
	p := ports[0]
	if !p.Enabled {
		st.PortDisabled++
		return false
	}
	r.Gruzpol = p.Organisation
	r.GruzpolS = p.NameS
	return true
}

// enrichStations заполняет станции отправления/назначения/операции. Каждая
// станция обрабатывается НЕЗАВИСИМО: неизвестный справочнику код заполняет только
// свои поля пустотой и попадает в notFound, но разбор остальных станций не
// прерывает (решение владельца 03.08.2026).
//
// ⚠️ Раньше здесь стоял ранний выход «как в gtlogic», и он вместе с фильтром
// порта выбрасывал вагоны из снимка целиком. В gtlogic такого следствия не было:
// там enrichFromPorts просто ничего не заполнял, а запись оставалась в дислокации
// (см. enrich_stage2.go: enrichFromPorts). Наступили на это 03.08.2026: 15 вагонов
// НМТП со станцией отправления ШУШАРЫ (033004 — Октябрьская дорога, в справочнике
// её нет) числились пропавшими, хотя приходили в каждой выгрузке. На xlsx-пути
// дефект маскировался: кабинет печатает код в скобках без ведущего нуля
// («ШУШАРЫ (33004)»), парсер ждёт ровно шесть цифр и оставлял поле пустым — а
// пустое поле обрыва не вызывало.
func (e *Enricher) enrichStations(r *domain.Dislocation, notFound map[int]struct{}) {
	// Станция отправления → StationNach, DorogaNach.
	if r.CodeStationNach != "" {
		if kod, err := strconv.Atoi(r.CodeStationNach); err == nil {
			if station, ok := e.dir.GetStationByKod(kod); ok && station.Name != "" {
				r.StationNach = station.Name
				r.DorogaNach = station.Road
			} else {
				notFound[kod] = struct{}{}
			}
		}
	}

	// Станция назначения → StanNazn, Code4StanNazn (только имя и 4-значный код).
	if r.CodeStanNazn != "" {
		if kod, err := strconv.Atoi(r.CodeStanNazn); err == nil {
			if station, ok := e.dir.GetStationByKod(kod); ok && station.Name != "" {
				r.StanNazn = station.Name
				r.Code4StanNazn = strconv.Itoa(station.Kod4)
			} else {
				notFound[kod] = struct{}{}
			}
		}
	}

	// Станция операции → StationOper, DorogaOper, Latitude, Longitude.
	if r.CodeStationOper != "" {
		if kod, err := strconv.Atoi(r.CodeStationOper); err == nil {
			if station, ok := e.dir.GetStationByKod(kod); ok && station.Name != "" {
				r.StationOper = station.Name
				r.DorogaOper = station.Road
				if station.Latitude != nil {
					r.Latitude = fmt.Sprintf("%f", *station.Latitude)
				}
				if station.Longitude != nil {
					r.Longitude = fmt.Sprintf("%f", *station.Longitude)
				}
				// Маркер альтернативного пути (БАМ) — из станции операции (где вагон
				// сейчас). Persistent (alternative_move); читается прогнозом (§3.18).
				if station.IsBam {
					r.AlternativeMove = 1
				}
			} else {
				notFound[kod] = struct{}{}
			}
		}
	}
}

// enrichOperation заполняет имя грузовой операции (CodeOper → Oper, OperS).
func (e *Enricher) enrichOperation(r *domain.Dislocation, notFound map[int]struct{}) {
	if r.CodeOper == "" {
		return
	}
	kod, err := strconv.Atoi(r.CodeOper)
	if err != nil {
		return
	}
	op, ok := e.dir.GetCargoOperation(kod)
	if !ok {
		notFound[kod] = struct{}{}
		return
	}
	r.Oper = op.Oper
	r.OperS = op.OperS
}

// deriveDates: date_op = дата из time_op; date_op_jd = time_op (+1 сутки если
// час ≥ cutoff — операционные ЖД-сутки).
func deriveDates(r *domain.Dislocation, cutoff int) {
	if r.TimeOp == nil || time.Time(*r.TimeOp).IsZero() {
		r.DateOp = nil
		r.DateOpJd = nil
		return
	}
	t := time.Time(*r.TimeOp)
	dop := domain.LocalTime(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC))
	r.DateOp = &dop
	jd := t
	if t.Hour() >= cutoff {
		jd = t.Add(24 * time.Hour)
	}
	ljd := domain.LocalTime(jd)
	r.DateOpJd = &ljd
}

// computeStatus — дерево статусов (§3.13). Порожний признак проверяется первым.
// porozhInbound («порожний под погрузку», DirectoryCache.PorozhInbound) гасит
// порожнюю ветку: изначально пустой вагон, едущий к нам ЗА грузом, живёт обычным
// деревом статусов движения (0/1/2/4/5/9/10) — иначе весь его подвод слипался бы
// в неподвижный статус 6 (решение владельца 04.08.2026, отход от gtport).
func computeStatus(r *domain.Dislocation, prostDnMin, prostChMin int, porozhInbound bool) int {
	atDest := r.StationOper != "" && r.StanNazn != "" && r.StationOper == r.StanNazn

	// Порожний — раньше всего (но не «под погрузку»: тот идёт дальше как гружёный).
	if r.PorozhPriznak == "1" && !porozhInbound {
		if atDest {
			return 12 // порожний в порту
		}
		return 6 // порожний в пути
	}

	// Гружёный на станции назначения.
	if atDest {
		if r.DatePrib != nil && !time.Time(*r.DatePrib).IsZero() {
			return 10 // прибыл
		}
		return 9 // кандидат в прибывшие (date_prib пусто)
	}

	// На станции отправления.
	if r.CodeStationNach != "" && r.CodeStationOper != "" && r.CodeStationNach == r.CodeStationOper {
		if r.Index == "Б/И" {
			return 0
		}
		return 1
	}

	// Брошен.
	if r.CodeOper == "92" {
		return 5
	}

	// Долгий простой в пути.
	prostDn, prostCh := derefInt(r.ProstDn), derefInt(r.ProstCh)
	if r.StationOper != r.StanNazn && r.StationOper != r.StationNach &&
		r.StationOper != "" && r.StanNazn != "" && r.StationNach != "" &&
		r.CodeOper != "92" && (prostDn >= prostDnMin || prostCh >= prostChMin) {
		return 4
	}

	return 2 // в пути
}

// computeDateKon: 10 (прибыл) → date_op_jd; иначе (включая 12 — выгружен в порту) → time_op.
func computeDateKon(r *domain.Dislocation, status int) *domain.LocalTime {
	if status == 10 {
		return r.DateOpJd
	}
	return r.TimeOp
}

// computeDelay: просрочка в сутках, если норматив доставки в прошлом (по «сейчас» МСК).
func computeDelay(dostav *domain.LocalTime, now time.Time) *int {
	if dostav == nil || time.Time(*dostav).IsZero() {
		return nil
	}
	today := now.Truncate(24 * time.Hour)
	d := time.Time(*dostav).Truncate(24 * time.Hour)
	if d.Before(today) {
		days := int(today.Sub(d).Hours() / 24)
		return &days
	}
	return nil
}

// computeIdDisl — ключ поезда: index/code_station_oper/oper_s/date_op(ДД.ММ.ГГГГ),
// только непустые компоненты.
func computeIdDisl(r *domain.Dislocation) string {
	parts := make([]string, 0, 4)
	if r.Index != "" {
		parts = append(parts, r.Index)
	}
	if r.CodeStationOper != "" {
		parts = append(parts, r.CodeStationOper)
	}
	if r.OperS != "" {
		parts = append(parts, r.OperS)
	}
	if r.DateOp != nil && !time.Time(*r.DateOp).IsZero() {
		parts = append(parts, time.Time(*r.DateOp).Format("02.01.2006"))
	}
	return strings.Join(parts, "/")
}

// brosKey — ключ агрегации (перенос createBrosKey из gtlogic): index|code_station_oper|
// time_op. Пусто, если нет любого компонента. id_status5 (брошен) и id_status4 (простой).
func brosKey(r *domain.Dislocation) string {
	if r.Index == "" || r.CodeStationOper == "" || r.TimeOp == nil {
		return ""
	}
	return r.Index + "|" + r.CodeStationOper + "|" + time.Time(*r.TimeOp).Format("2006-01-02 15:04:05")
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func sortedKeys(m map[int]struct{}) []int {
	if len(m) == 0 {
		return nil
	}
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
