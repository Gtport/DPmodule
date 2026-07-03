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

// Enricher — обогащение записей дислокации из справочников (перенос Stage 1–2 из
// gtlogic). Stage 1: имена станций и операций из stations/cargo_operations. Мутирует
// записи на месте, читает справочники из DirectoryCache. Без БД и сети.
type Enricher struct {
	dir *DirectoryCache
}

func NewEnricher(dir *DirectoryCache) *Enricher { return &Enricher{dir: dir} }

// Stage1Stats — диагностика прогона Stage 1 («падать громко» на целостности:
// коды справочников, которых не хватило). Not-found — уникальные коды.
type Stage1Stats struct {
	Records            int   // всего записей
	NaznEnriched       int   // записей с заполненной станцией назначения
	StationsNotFound   []int // коды станций, отсутствующие в справочнике
	OperationsNotFound []int // коды операций, отсутствующие в справочнике
}

// Stage1 обогащает имена станций (отправления/назначения/операции) и операций.
// Порядок и квирки — как в gtlogic FirstEnrichmentBatch: отсутствие станции
// ОТПРАВЛЕНИЯ прерывает обогащение станций этой записи (назначение/операция не
// заполняются) — сохранено для паритета поведения.
func (e *Enricher) Stage1(records []domain.Dislocation) Stage1Stats {
	stationsNF := map[int]struct{}{}
	opsNF := map[int]struct{}{}
	st := Stage1Stats{Records: len(records)}

	for i := range records {
		e.enrichStations(&records[i], stationsNF)
		e.enrichOperation(&records[i], opsNF)
		if records[i].StanNazn != "" {
			st.NaznEnriched++
		}
	}

	st.StationsNotFound = sortedKeys(stationsNF)
	st.OperationsNotFound = sortedKeys(opsNF)
	return st
}

// enrichStations заполняет станции отправления/назначения/операции. Ранний выход
// при ненайденной/безымянной станции — дословно как в gtlogic (см. заголовок Stage1).
func (e *Enricher) enrichStations(r *domain.Dislocation, notFound map[int]struct{}) {
	// Станция отправления → StationNach, DorogaNach.
	if r.CodeStationNach != "" {
		if kod, err := strconv.Atoi(r.CodeStationNach); err == nil {
			station, ok := e.dir.GetStationByKod(kod)
			if !ok || station.Name == "" {
				notFound[kod] = struct{}{}
				return
			}
			r.StationNach = station.Name
			r.DorogaNach = station.Road
		}
	}

	// Станция назначения → StanNazn, Code4StanNazn (только имя и 4-значный код).
	if r.CodeStanNazn != "" {
		if kod, err := strconv.Atoi(r.CodeStanNazn); err == nil {
			station, ok := e.dir.GetStationByKod(kod)
			if !ok || station.Name == "" {
				notFound[kod] = struct{}{}
				return
			}
			r.StanNazn = station.Name
			r.Code4StanNazn = strconv.Itoa(station.Kod4)
		}
	}

	// Станция операции → StationOper, DorogaOper, Latitude, Longitude.
	if r.CodeStationOper != "" {
		if kod, err := strconv.Atoi(r.CodeStationOper); err == nil {
			station, ok := e.dir.GetStationByKod(kod)
			if !ok || station.Name == "" {
				notFound[kod] = struct{}{}
				return
			}
			r.StationOper = station.Name
			r.DorogaOper = station.Road
			if station.Latitude != nil {
				r.Latitude = fmt.Sprintf("%f", *station.Latitude)
			}
			if station.Longitude != nil {
				r.Longitude = fmt.Sprintf("%f", *station.Longitude)
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

// Stage1bConfig — параметры производных расчётов (из настроек, не хардкод).
type Stage1bConfig struct {
	CutoffHour int // порог часа для date_op_jd (date_cutoff_hour профиля); ≤0 → 18
	ProstDnMin int // порог простоя в сутках → статус 4 (client_settings)
	ProstChMin int // порог простоя в часах → статус 4 (client_settings)
}

// Stage1b вычисляет производные поля дислокации (перенос gtlogic
// calculateDerivedFields, ревизия статусов — TARGET.md §3.13): date_op, date_op_jd,
// status, id_status4/5, date_kon, delay, id_disl. Мутирует записи на месте.
// «Сейчас» для delay — по Москве (clock.Now()). Возвращает распределение статусов.
func (e *Enricher) Stage1b(records []domain.Dislocation, cfg Stage1bConfig) map[int]int {
	if cfg.CutoffHour <= 0 {
		cfg.CutoffHour = 18
	}
	now := time.Time(clock.Now())
	dist := map[int]int{}
	for i := range records {
		r := &records[i]
		deriveDates(r, cfg.CutoffHour)
		status := computeStatus(r, cfg.ProstDnMin, cfg.ProstChMin)
		r.Status = &status
		dist[status]++
		switch status {
		case 5:
			r.IdStatus5 = brosKey(r)
		case 4:
			r.IdStatus4 = brosKey(r)
		}
		r.DateKon = computeDateKon(r, status)
		r.Delay = computeDelay(r.DateDostav, now)
		r.IdDisl = computeIdDisl(r)
	}
	return dist
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
func computeStatus(r *domain.Dislocation, prostDnMin, prostChMin int) int {
	atDest := r.StationOper != "" && r.StanNazn != "" && r.StationOper == r.StanNazn

	// Порожний — раньше всего.
	if r.PorozhPriznak == "1" {
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

// computeDateKon: 10 → date_op_jd; 12 → nil (порожний, закрывать нечего); иначе time_op.
func computeDateKon(r *domain.Dislocation, status int) *domain.LocalTime {
	switch status {
	case 10:
		return r.DateOpJd
	case 12:
		return nil
	default:
		return r.TimeOp
	}
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
