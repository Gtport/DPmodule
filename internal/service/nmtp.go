package service

// Отчёт «Подход вагонов» по форме порта (НМТП) — перенос gtport PortReportNmtp
// с унификацией (решения владельца 29.07.2026):
//   - колонки формы задаёт справочник nmtp_column (клиент × станции погрузки ×
//     марка), а не sprav_1 с хардкод-маппингом: в DPmodule sprav_1 пуст;
//   - вагон матчится в САМУЮ СПЕЦИФИЧНУЮ совпавшую колонку (больше заданных
//     правил — раньше проверка), не сматчившиеся падают в колонку «прочее» —
//     громко, а не молча;
//   - марка груза: код ЕТСНГ её не различает (весь концентрат — 161043),
//     нормализатор ищет известные марки (nmtp_mark) в freight_exact_name по
//     границам слова, длинные первыми; фолбэк — cargo_sms словаря cargo
//     (сортовые угли Д/Г/Т и металл ЗАГ/СЛЯБЫ/ЧУГУН/РЕЛ);
//   - строки — поезда в подходе с отбором «Подхода» (получатель ИЛИ назначение
//     — так устроен файл порта: поезда «НА АТТИС» стоят в листе ГУТ-2 с
//     пометкой; gtport НМТП фильтровал строго по назначению — осознанный отход);
//   - ручных правок нет (решение владельца: «убираем, но помним»).

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
)

// Порядок дорог с востока на запад — как в файле порта (география сети РЖД,
// не порт-специфика). Дороги вне списка попадают в конец секций с кодом как
// есть — поезд не теряется.
var nmtpRoads = []struct{ Code, Label string }{
	{"ДВС", "ДАЛЬНЕВОСТОЧНАЯ ЖД"},
	{"ЗАБ", "ЗАБАЙКАЛЬСКАЯ ЖД"},
	{"ВСБ", "ВОСТОЧНО-СИБИРСКАЯ ЖД"},
	{"КРС", "КРАСНОЯРСКАЯ ЖД"},
	{"ЗСБ", "ЗАПАДНО-СИБИРСКАЯ ЖД"},
	{"ЮУР", "ЮЖНО-УРАЛЬСКАЯ ЖД"},
	{"СВР", "СВЕРДЛОВСКАЯ ЖД"},
}

// nmtpNearRoads — «ближние» дороги формулы «Прогноз выгрузки по подходу»
// (из формул файла: вагоны терминальных станций + этих дорог, делённые на 7).
var nmtpNearRoads = map[string]bool{"ДВС": true, "ЗАБ": true, "ВСБ": true}

// nmtpMatcher — правила одной колонки, развёрнутые в set-ы.
type nmtpMatcher struct {
	head        domain.NmtpColumnHead
	clients     map[string]bool
	stations    map[string]bool
	marks       map[string]bool
	specificity int // сколько правил задано (порядок проверки)
	order       int // sort_order (порядок отображения и tie-break)
}

func splitSet(raw string) map[string]bool {
	out := map[string]bool{}
	for _, s := range strings.Split(raw, "|") {
		if s = strings.TrimSpace(s); s != "" {
			out[strings.ToUpper(s)] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MarkNormalizer извлекает марку груза из свободного текста freight_exact_name:
// известные марки ищутся по границам слова (символы марок — буквы/цифры/+/-),
// длинные первыми («ГЖО» побеждает «Г», «ГЖ+Ж» — «ГЖ»). Не нашлась — фолбэк
// cargo_sms (у сортовых углей и металла метка словаря и есть марка/продукция).
type MarkNormalizer struct {
	marks []string // UPPER, отсортированы по длине по убыванию
}

func NewMarkNormalizer(marks []string) *MarkNormalizer {
	up := make([]string, 0, len(marks))
	for _, m := range marks {
		if m = strings.ToUpper(strings.TrimSpace(m)); m != "" {
			up = append(up, m)
		}
	}
	sort.SliceStable(up, func(i, j int) bool { return len([]rune(up[i])) > len([]rune(up[j])) })
	return &MarkNormalizer{marks: up}
}

func isMarkRune(r rune) bool {
	return r == '+' || r == '-' ||
		(r >= 'А' && r <= 'Я') || r == 'Ё' ||
		(r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func (n *MarkNormalizer) Mark(freightExactName, cargoSms string) string {
	text := []rune(strings.ToUpper(freightExactName))
	for _, mark := range n.marks {
		mr := []rune(mark)
		for i := 0; i+len(mr) <= len(text); i++ {
			if string(text[i:i+len(mr)]) != mark {
				continue
			}
			if i > 0 && isMarkRune(text[i-1]) {
				continue
			}
			if j := i + len(mr); j < len(text) && isMarkRune(text[j]) {
				continue
			}
			return mark
		}
	}
	return strings.ToUpper(strings.TrimSpace(cargoSms))
}

// ── Сервис ──────────────────────────────────────────────────────────────────

type NmtpService struct {
	actual *ActualCache
	dir    *DirectoryCache
	repo   port.NmtpRepository
	bros   port.BrosRepository // даты бросания; nil — без дат
}

func NewNmtpService(actual *ActualCache, dir *DirectoryCache, repo port.NmtpRepository, bros port.BrosRepository) *NmtpService {
	return &NmtpService{actual: actual, dir: dir, repo: repo, bros: bros}
}

// Terminals — терминалы, у которых настроена раскладка (кнопки карточки).
func (s *NmtpService) Terminals(ctx context.Context) ([]string, error) {
	return s.repo.Terminals(ctx)
}

// Report — форма терминала из текущего снимка. naznachOnly — режим «скрыть
// перестановки» (как gtport UseNaznachOnly): строго по назначению, без поездов,
// переставляемых на соседний терминал; по умолчанию — получатель ИЛИ назначение.
func (s *NmtpService) Report(ctx context.Context, terminal string, naznachOnly bool) (domain.NmtpReport, error) {
	p, ok := s.dir.PortByNameS(terminal)
	if !ok {
		return domain.NmtpReport{}, fmt.Errorf("неизвестный терминал: %s", terminal)
	}
	cols, err := s.repo.Columns(ctx, terminal)
	if err != nil {
		return domain.NmtpReport{}, err
	}
	if len(cols) == 0 {
		return domain.NmtpReport{}, fmt.Errorf("для терминала %s не настроены колонки НМТП (справочник nmtp_column)", terminal)
	}
	marks, err := s.repo.Marks(ctx)
	if err != nil {
		return domain.NmtpReport{}, err
	}

	// Даты бросания активных бросков по индексу поезда.
	brosDates := map[string]*domain.LocalTime{}
	if s.bros != nil {
		if active, err := s.bros.Active(ctx); err == nil {
			for _, b := range active {
				if b.Index1 != "" {
					brosDates[b.Index1] = b.DateBr
				}
			}
		}
	}

	// Станции терминалов станции порта (верхние секции файла): все станции
	// реестра, отсортированные по коду по убыванию (порядок главной страницы).
	termStations := terminalStationNames(s.dir)

	report := buildNmtpReport(s.actual.All(), cols, marks, terminal, termStations, brosDates, naznachOnly)
	report.Norm = p.NmtpNorm
	return report, nil
}

// terminalStationNames — имена причальных станций из реестра ports (уникальные,
// по коду станции по убыванию — как раскладка главной: Мыс, Находка).
func terminalStationNames(dir *DirectoryCache) []string {
	type st struct {
		name string
		code int
	}
	seen := map[string]bool{}
	var list []st
	for _, t := range terminalTargets(dir) {
		if t.Station == "" || seen[t.Station] {
			continue
		}
		seen[t.Station] = true
		code := 0
		fmt.Sscanf(t.StationCode, "%d", &code)
		list = append(list, st{t.Station, code})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].code > list[j].code })
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.name
	}
	return out
}

// nmtpMinTrainVagons — порог счётчиков поездов: состав короче не считается
// поездом (правило владельца 30.07.2026; в gtport та же идея порогом >25 в
// формуле листа). Строку в форме короткий состав всё равно получает.
const nmtpMinTrainVagons = 20

// buildNmtpReport — чистая агрегация (покрыта тестами).
func buildNmtpReport(records []domain.Dislocation, cols []domain.NmtpColumn, markDict []string,
	terminal string, termStations []string, brosDates map[string]*domain.LocalTime,
	naznachOnly bool) domain.NmtpReport {

	norm := NewMarkNormalizer(markDict)
	matchers := make([]nmtpMatcher, len(cols))
	for i, c := range cols {
		m := nmtpMatcher{
			head:     domain.NmtpColumnHead{Group: c.GroupLabel, Station: c.StationLabel, Mark: c.MarkLabel},
			clients:  splitSet(c.MatchClients),
			stations: splitSet(c.MatchStations),
			marks:    splitSet(c.MatchMarks),
			order:    i,
		}
		for _, set := range []map[string]bool{m.clients, m.stations, m.marks} {
			if set != nil {
				m.specificity++
			}
		}
		matchers[i] = m
	}
	// Порядок проверки: специфичные раньше («СЛЯБЫ» раньше «ПРОКАТ» без марки);
	// порядок отображения (order) не трогаем.
	checkOrder := make([]int, len(matchers))
	for i := range checkOrder {
		checkOrder[i] = i
	}
	sort.SliceStable(checkOrder, func(a, b int) bool {
		ma, mb := matchers[checkOrder[a]], matchers[checkOrder[b]]
		if ma.specificity != mb.specificity {
			return ma.specificity > mb.specificity
		}
		return ma.order < mb.order
	})

	nCols := len(cols) + 1 // + «прочее»
	otherIdx := len(cols)

	type train struct {
		row      domain.NmtpTrainRow
		firstNpp *int
		road     string
	}
	trains := map[string]*train{}
	var order []string

	for i := range records {
		rec := &records[i]
		if naznachOnly {
			if rec.Naznach != terminal {
				continue // «скрыть перестановки»: без уходящих «НА {сосед}»
			}
		} else if rec.GruzpolS != terminal && rec.Naznach != terminal {
			continue
		}
		if rec.Status != nil && (*rec.Status == 10 || *rec.Status == 12) {
			continue
		}
		if rec.ProgJd == nil {
			continue
		}
		idx := rec.Index
		if rec.IndexPp != "" {
			idx = rec.IndexPp
		}
		// Ключ строки — индекс + станция + прогноз до минуты (как ключ L1 у
		// «Подхода»): один индекс ≠ один поезд — безиндексные («Б/И») стоят на
		// разных станциях и по одному индексу слипались бы в одну строку.
		key := idx + "|" + rec.StationOper + "|" + rec.ProgJd.Time().Format("2006-01-02 15:04")
		tr, ok := trains[key]
		if !ok {
			note := ""
			if rec.GruzpolS != rec.Naznach && rec.GruzpolS != "" && rec.Naznach != "" {
				if rec.Naznach == terminal {
					note = "С " + rec.GruzpolS
				} else {
					note = "НА " + rec.Naznach
				}
			}
			tr = &train{row: domain.NmtpTrainRow{
				Index: idx, StationOper: rec.StationOper, Note: note,
				Prog:   rec.ProgJd,
				Counts: make([]int, nCols), Tons: make([]float64, nCols),
			}}
			trains[key] = tr
			order = append(order, key)
		}
		if tr.road == "" && rec.DorogaOper != "" {
			tr.road = rec.DorogaOper
		}
		// Плановый поезд — хоть один вагон в плане подвода; только плановым
		// печатается «ожид. дата/время приб.» (правило владельца 30.07.2026).
		if rec.PlanJd != nil {
			tr.row.Planned = true
		}
		// «Вагон для контроля» — головной вагон состава (мин. номер в поезде).
		if tr.row.ControlVagon == "" || (rec.NppVag != nil && (tr.firstNpp == nil || *rec.NppVag < *tr.firstNpp)) {
			tr.row.ControlVagon = rec.Vagon
			tr.firstNpp = rec.NppVag
		}
		// Мода даты приёма не считается — у поезда она едина в подавляющем
		// большинстве; берём самую раннюю (партия грузилась в один день).
		if rec.DateNach != nil && (tr.row.DateNach == nil || rec.DateNach.Time().Before(tr.row.DateNach.Time())) {
			tr.row.DateNach = rec.DateNach
		}
		if rec.Status != nil && *rec.Status == 5 && tr.row.DateBros == nil {
			tr.row.DateBros = brosDates[idx]
			if tr.row.DateBros == nil {
				tr.row.DateBros = rec.TimeOp // нет записи в bros — момент операции бросания
			}
		}

		// Раскладка вагона в колонку.
		mark := norm.Mark(rec.FreightExactName, rec.CargoSms)
		client := strings.ToUpper(strings.TrimSpace(rec.Client))
		station := strings.ToUpper(strings.TrimSpace(rec.StationNach))
		col := otherIdx
		for _, mi := range checkOrder {
			m := &matchers[mi]
			if m.clients != nil && !m.clients[client] {
				continue
			}
			if m.stations != nil && !m.stations[station] {
				continue
			}
			if m.marks != nil && !m.marks[mark] {
				continue
			}
			col = m.order
			break
		}
		tr.row.Counts[col]++
		tr.row.Total++
		if rec.Ves != nil {
			tr.row.Tons[col] += *rec.Ves
		}
	}

	// Раскладка поездов по секциям: станции терминалов, затем дороги.
	roadLabel := map[string]string{}
	for _, r := range nmtpRoads {
		roadLabel[r.Code] = r.Label
	}
	sectionOf := func(t *train) string {
		for _, st := range termStations {
			if t.row.StationOper == st {
				return st
			}
		}
		if l, ok := roadLabel[t.road]; ok {
			return l
		}
		if t.road != "" {
			return t.road
		}
		return "ПРОЧИЕ"
	}

	report := domain.NmtpReport{
		Terminal:  terminal,
		ColCounts: make([]int, nCols),
		ColTons:   make([]float64, nCols),
	}
	for _, c := range cols {
		report.Columns = append(report.Columns, domain.NmtpColumnHead{Group: c.GroupLabel, Station: c.StationLabel, Mark: c.MarkLabel})
	}

	// Секции в порядке файла: терминальные станции → дороги (восток → запад).
	type sectionKey struct {
		label string
		near  bool
	}
	var keys []sectionKey
	for _, st := range termStations {
		keys = append(keys, sectionKey{st, true})
	}
	for _, r := range nmtpRoads {
		keys = append(keys, sectionKey{r.Label, nmtpNearRoads[r.Code]})
	}

	active := map[string][]domain.NmtpTrainRow{}
	abandoned := map[string][]domain.NmtpTrainRow{}
	known := map[string]bool{}
	for _, k := range keys {
		known[k.label] = true
	}
	for _, idx := range order {
		tr := trains[idx]
		label := sectionOf(tr)
		if !known[label] { // дорога вне известного списка — секция в конец
			known[label] = true
			keys = append(keys, sectionKey{label, false})
		}
		if tr.row.DateBros != nil {
			abandoned[label] = append(abandoned[label], tr.row)
		} else {
			active[label] = append(active[label], tr.row)
		}
	}

	build := func(src map[string][]domain.NmtpTrainRow) []domain.NmtpSection {
		var out []domain.NmtpSection
		for _, k := range keys {
			rows := src[k.label]
			if rows == nil {
				rows = []domain.NmtpTrainRow{} // в JSON — [], не null (экран ждёт массив)
			}
			// Внутри секции — по прогнозу по возрастанию (как файл: ближние выше).
			sort.SliceStable(rows, func(a, b int) bool {
				pa, pb := rows[a].Prog, rows[b].Prog
				if pa == nil || pb == nil {
					return pb == nil && pa != nil
				}
				return pa.Time().Before(pb.Time())
			})
			total := 0
			for _, r := range rows {
				total += r.Total
			}
			out = append(out, domain.NmtpSection{Label: k.label, Near: k.near, Rows: rows, Total: total})
		}
		return out
	}
	report.Sections = build(active)
	report.Abandoned = build(abandoned)

	// Итоги и подвал.
	clientTons := map[string]float64{}
	clientOrder := []string{}
	addTotals := func(secs []domain.NmtpSection, trainsCounter *int) {
		for _, sec := range secs {
			for _, r := range sec.Rows {
				// Короткий состав поездом не считается (< nmtpMinTrainVagons),
				// но в строках, вагонах и тоннаже участвует полностью.
				if r.Total >= nmtpMinTrainVagons {
					*trainsCounter++
				}
				report.TotalVagons += r.Total
				for c := 0; c < nCols; c++ {
					report.ColCounts[c] += r.Counts[c]
					report.ColTons[c] += r.Tons[c]
				}
			}
		}
	}
	addTotals(report.Sections, &report.TrainsActive)
	addTotals(report.Abandoned, &report.TrainsAbandoned)
	for c := 0; c < nCols; c++ {
		report.ColTons[c] = report.ColTons[c] / 1000 // тыс. тонн
		report.TotalTons += report.ColTons[c]
		if report.ColCounts[c] > 0 && c == otherIdx {
			report.HasOther = true
		}
		group := "ПРОЧЕЕ"
		if c < len(cols) {
			group = cols[c].GroupLabel
		}
		if _, ok := clientTons[group]; !ok {
			clientOrder = append(clientOrder, group)
		}
		clientTons[group] += report.ColTons[c]
	}
	// В свод — только группы с тоннажом (порядок появления колонок).
	for _, g := range clientOrder {
		if clientTons[g] > 0 {
			report.ClientTons = append(report.ClientTons, domain.NmtpClientTons{Client: g, Tons: clientTons[g]})
		}
	}

	// «Прогноз выгрузки по подходу»: вагоны ближних секций (только активные) / 7.
	near := 0
	for _, sec := range report.Sections {
		if sec.Near {
			near += sec.Total
		}
	}
	report.UnloadForecast = float64(near) / 7

	return report
}
