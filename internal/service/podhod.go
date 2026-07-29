package service

// Отчёт «Подход» (перенос gtport PortReportTable: server/internal/repository/
// port_report_repository.go + service/port_report_service.go) — справка по
// поездам, идущим на терминал, из RAM-снимка актуальной дислокации.
//
// Двухуровневая группировка (дословно из gtport):
//   L1 (поезд):     ключ index(приоритет index_pp) | stan_nazn | prog_jd(до минуты)
//   L2 (подгруппа): ключ L1 | index_main | cargo_s | naznach | gruzpol_s
//
// Фильтры (эквивалент gtport): prog_msk задан (только с прогнозом); статус не
// 10 и не 12 (в gtport исключался только 10 — статуса 12 «выгружен-обновился»
// там не было); терминал по gruzpol_s ИЛИ naznach; клиент — множество имён,
// разделитель '|'.
//
// Отклонения от gtport (решения владельца, план от 29.07.2026):
//   - prim_2 перестановок — универсальные формулировки по совпадению станций
//     терминалов из реестра ports, а не switch из шести захардкоженных фраз;
//   - переадресация — naznach=="ВП" + pereadr_port (в gtport: naznach=="ДР" +
//     info_1; поля info_* упразднены);
//   - при равенстве частот date_nach побеждает более ранняя дата (в gtport
//     выбор зависел от порядка обхода map — недетерминированный).

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// PodhodService — отчёт «Подход» и его пресеты (клиентские варианты карточек).
type PodhodService struct {
	actual  *ActualCache
	dir     *DirectoryCache
	presets port.ReportPresetRepository // nil — пресетов нет (нет БД)
}

func NewPodhodService(actual *ActualCache, dir *DirectoryCache, presets port.ReportPresetRepository) *PodhodService {
	return &PodhodService{actual: actual, dir: dir, presets: presets}
}

// PodhodSubgroup — подгруппа поезда (L2): партия одного груза/отправителя.
// JSON-контракт повторяет gtport PortReportSubgroup — сверка со старым экраном 1:1.
type PodhodSubgroup struct {
	StationNach string            `json:"station_nach"`
	DateNach    *domain.LocalTime `json:"date_nach"` // самая частая дата погрузки подгруппы
	Gruzotpr    string            `json:"gruzotpr"`
	VagonCount  int               `json:"vagon_count"`
	TotalWeight float64           `json:"total_weight"`
	Sprav1      string            `json:"sprav_1"`
	Sprav2      string            `json:"sprav_2"` // первый вагон подгруппы
	Sprav3      string            `json:"sprav_3"`
	Prim1       string            `json:"prim_1"` // «был CCC» при смене индекса
	Prim2       string            `json:"prim_2"` // переадресация / перестановка
	Prim3       string            `json:"prim_3"` // prim_1 + prim_2
	Prim4       string            `json:"prim_4"` // цветовая метка вагона
}

// PodhodItem — поезд (L1) с подгруппами; n — порядковый номер по prog_msk.
type PodhodItem struct {
	N           int               `json:"n"`
	Index       string            `json:"index"`
	PlanMsk     *domain.LocalTime `json:"plan_msk"`
	StationOper string            `json:"station_oper"`
	DorogaOper  string            `json:"doroga_oper"`
	OperS       string            `json:"oper_s"`
	ProgMsk     *domain.LocalTime `json:"prog_msk"`
	Subgroups   []PodhodSubgroup  `json:"subgroups"`
}

// PodhodReport — ответ отчёта. Clients — все клиенты среди записей терминала
// ДО клиентского фильтра (питает мультиселект на фронте; нового поля в gtport
// не было — там список клиентов зашивался в кнопки).
type PodhodReport struct {
	Items   []PodhodItem `json:"items"`
	Total   int          `json:"total"`
	Clients []string     `json:"clients"`
}

// Report строит отчёт по терминалу (краткое имя причала, ports.name_s) с
// опциональным фильтром клиентов (имена через '|', формат gtport client_filter).
func (s *PodhodService) Report(terminal, clientsRaw string) (PodhodReport, error) {
	terminal = strings.TrimSpace(terminal)
	if terminal == "" {
		return PodhodReport{}, fmt.Errorf("не указан терминал")
	}
	if _, ok := s.dir.PortByNameS(terminal); !ok {
		return PodhodReport{}, fmt.Errorf("неизвестный терминал: %s", terminal)
	}
	return aggregatePodhod(s.actual.All(), terminal, podhodClientSet(clientsRaw), s.terminalStations()), nil
}

// Presets — включённые пресеты формы «Подход» (карточки «Подход {имя}»).
func (s *PodhodService) Presets(ctx context.Context) ([]domain.ReportPreset, error) {
	if s.presets == nil {
		return []domain.ReportPreset{}, nil
	}
	list, err := s.presets.List(ctx, "podhod")
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []domain.ReportPreset{}
	}
	return list, nil
}

// terminalStations — карта «терминал → код причальной станции» из реестра
// ports; по совпадению станций различаем перестановку и переадресацию.
func (s *PodhodService) terminalStations() map[string]string {
	out := make(map[string]string)
	for _, t := range s.dir.EnabledTerminals() {
		if p, ok := s.dir.PortByNameS(t); ok {
			out[t] = p.StationCode
		}
	}
	return out
}

// podhodClientSet разбирает фильтр клиентов "A|B" в множество; пусто → nil
// («фильтр отключён»). Дословно gtport buildClientSet.
func podhodClientSet(raw string) map[string]bool {
	if raw == "" {
		return nil
	}
	set := make(map[string]bool)
	for _, p := range strings.Split(raw, "|") {
		if p = strings.TrimSpace(p); p != "" {
			set[p] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// aggregatePodhod — чистая функция агрегации (страховка переноса — golden-тест
// podhod_internal_test.go). Перенос gtport GetPortReport с заменами из шапки файла.
func aggregatePodhod(records []domain.Dislocation, terminal string, clients map[string]bool, stationOf map[string]string) PodhodReport {
	firstLevelMap := make(map[string]*PodhodItem)
	subgroupMap := make(map[string]*PodhodSubgroup)
	subgroupOrder := make(map[string][]string)   // порядок подгрупп внутри поезда — порядок появления
	firstLevelOrder := make([]string, 0)         // ключи L1 в порядке появления (для детерминизма)
	dateNachFreq := make(map[string]map[string]int)
	dateNachValues := make(map[string]map[string]*domain.LocalTime)
	clientSeen := make(map[string]bool)

	for i := range records {
		rec := &records[i]

		// Терминал: по грузополучателю ИЛИ фактическому назначению.
		if rec.GruzpolS != terminal && rec.Naznach != terminal {
			continue
		}
		// Только вагоны с прогнозом прибытия.
		if rec.ProgMsk == nil {
			continue
		}
		// Исключаем выгруженные: 10 (как в gtport) и 12 (выгружен-обновился, DPmodule).
		if rec.Status != nil && (*rec.Status == 10 || *rec.Status == 12) {
			continue
		}

		// Список клиентов терминала — до клиентского фильтра.
		if c := strings.TrimSpace(rec.Client); c != "" {
			clientSeen[c] = true
		}

		if len(clients) > 0 && !clients[strings.TrimSpace(rec.Client)] {
			continue
		}

		// Ключ L1: index(приоритет index_pp) | stan_nazn | prog_jd(до минуты).
		indexForKey := rec.Index
		if rec.IndexPp != "" {
			indexForKey = rec.IndexPp
		}
		progJdKey := ""
		if rec.ProgJd != nil {
			progJdKey = rec.ProgJd.Time().Format("2006-01-02 15:04")
		}
		firstLevelKey := indexForKey + "|" + rec.StanNazn + "|" + progJdKey

		if _, ok := firstLevelMap[firstLevelKey]; !ok {
			firstLevelMap[firstLevelKey] = &PodhodItem{
				Index:       indexForKey,
				PlanMsk:     rec.PlanMsk,
				StationOper: rec.StationOper,
				DorogaOper:  rec.DorogaOper,
				OperS:       rec.OperS,
				ProgMsk:     rec.ProgMsk,
				Subgroups:   []PodhodSubgroup{},
			}
			firstLevelOrder = append(firstLevelOrder, firstLevelKey)
		}

		// Ключ L2: gruzpol_s в ключе разделяет подгруппы с одинаковым
		// назначением, но разными получателями (как в gtport).
		secondLevelKey := firstLevelKey + "|" + rec.IndexMain + "|" + rec.CargoS + "|" + rec.Naznach + "|" + rec.GruzpolS

		weight := 0.0
		if rec.Ves != nil {
			weight = *rec.Ves
		}

		// Частоты date_nach (до дня): партия грузится в один день, выбросы
		// отдельных вагонов не должны портить дату подгруппы.
		if dateNachFreq[secondLevelKey] == nil {
			dateNachFreq[secondLevelKey] = make(map[string]int)
			dateNachValues[secondLevelKey] = make(map[string]*domain.LocalTime)
		}
		if rec.DateNach != nil {
			dateKey := rec.DateNach.Time().Format("2006-01-02")
			dateNachFreq[secondLevelKey][dateKey]++
			if dateNachValues[secondLevelKey][dateKey] == nil {
				dateNachValues[secondLevelKey][dateKey] = rec.DateNach
			}
		}

		if sg, ok := subgroupMap[secondLevelKey]; ok {
			sg.VagonCount++
			sg.TotalWeight += weight
		} else {
			prim1, prim2, prim3, prim4 := buildPodhodPrim(rec, stationOf)
			subgroupMap[secondLevelKey] = &PodhodSubgroup{
				StationNach: rec.StationNach,
				DateNach:    rec.DateNach,
				Gruzotpr:    rec.Gruzotpr,
				VagonCount:  1,
				TotalWeight: weight,
				Sprav1:      rec.Sprav1,
				Sprav2:      rec.Vagon, // первый вагон подгруппы
				Sprav3:      rec.Sprav3,
				Prim1:       prim1,
				Prim2:       prim2,
				Prim3:       prim3,
				Prim4:       prim4,
			}
			subgroupOrder[firstLevelKey] = append(subgroupOrder[firstLevelKey], secondLevelKey)
		}
	}

	// Самая частая date_nach подгруппы; при равенстве — более ранняя.
	for slKey, sg := range subgroupMap {
		var bestKey string
		var bestCount int
		for dateKey, count := range dateNachFreq[slKey] {
			if count > bestCount || (count == bestCount && dateKey < bestKey) {
				bestCount = count
				bestKey = dateKey
			}
		}
		if bestKey != "" {
			sg.DateNach = dateNachValues[slKey][bestKey]
		}
	}

	// Подгруппы к поезду — в порядке появления; поезда — по prog_msk ASC.
	items := make([]PodhodItem, 0, len(firstLevelOrder))
	for _, flKey := range firstLevelOrder {
		item := firstLevelMap[flKey]
		for _, slKey := range subgroupOrder[flKey] {
			item.Subgroups = append(item.Subgroups, *subgroupMap[slKey])
		}
		items = append(items, *item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		switch {
		case a.ProgMsk == nil && b.ProgMsk == nil:
			if a.Index != b.Index {
				return a.Index < b.Index
			}
			return a.StationOper < b.StationOper
		case a.ProgMsk == nil:
			return true // без прогноза — в начало (как в gtport)
		case b.ProgMsk == nil:
			return false
		default:
			return a.ProgMsk.Time().Before(b.ProgMsk.Time())
		}
	})
	for i := range items {
		items[i].N = i + 1
	}

	clientList := make([]string, 0, len(clientSeen))
	for c := range clientSeen {
		clientList = append(clientList, c)
	}
	sort.Strings(clientList)

	return PodhodReport{Items: items, Total: len(items), Clients: clientList}
}

// buildPodhodPrim — примечания подгруппы (перенос gtport buildPrimFields).
func buildPodhodPrim(rec *domain.Dislocation, stationOf map[string]string) (prim1, prim2, prim3, prim4 string) {
	currentIndex := rec.Index
	if rec.IndexPp != "" {
		currentIndex = rec.IndexPp
	}

	// Prim1: поезд сменил назначение — показываем старую CCC-часть индекса.
	if currentIndex != rec.IndexMain && rec.IndexMain != "" && len(rec.IndexMain) >= 9 {
		parts := strings.Split(rec.IndexMain, "-")
		if len(parts) >= 3 && len(parts[1]) >= 3 {
			prim1 = "был " + parts[1]
		}
	}

	// Prim2: переадресация на внешний порт либо движение на чужую площадку.
	switch {
	case rec.Naznach == domain.NaznachExternalPort && rec.PereadrPort != "":
		prim2 = "Переадресация на " + rec.PereadrPort
	case rec.Naznach != "" && rec.GruzpolS != "" && rec.Naznach != rec.GruzpolS:
		gs, okG := stationOf[rec.GruzpolS]
		ns, okN := stationOf[rec.Naznach]
		if okG && okN && gs != "" && gs == ns {
			prim2 = "Перестановка на " + rec.Naznach // та же станция — перестановка
		} else {
			prim2 = "Переадр с " + rec.GruzpolS + " на " + rec.Naznach
		}
	}

	switch {
	case prim1 != "" && prim2 != "":
		prim3 = prim1 + ", " + prim2
	case prim1 != "":
		prim3 = prim1
	default:
		prim3 = prim2
	}
	prim4 = rec.Color
	return
}
