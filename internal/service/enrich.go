package service

import (
	"fmt"
	"sort"
	"strconv"

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
