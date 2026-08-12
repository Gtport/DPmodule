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
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
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

// Terminals — кнопки карточки «Подход груза»: ВСЕ включённые терминалы
// реестра ports (решение владельца 13.08.2026 — отчёт доступен любому
// клиенту; раскладка nmtp_column лишь уточняет колонки, без неё весь груз
// падает в «прочее»). Раньше список брался из nmtp_column, и клиент без
// раскладки не видел карточку вовсе.
func (s *NmtpService) Terminals(_ context.Context) ([]string, error) {
	return s.dir.EnabledTerminals(), nil
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
	// Пустая раскладка — не ошибка (решение владельца 13.08.2026): форма
	// строится с одной грузовой колонкой «прочее» — работает у любого клиента.
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

	// Ручные привязки «вагон → колонка» ЭТОГО терминала (сильнее правил).
	overrides, ours := s.vagonOverrides(ctx, cols)

	report, seen, stale := buildNmtpReport(s.actual.All(), cols, marks, terminal, termStations, brosDates, naznachOnly, overrides)
	report.Norm = p.NmtpNorm

	// Гашение (best-effort: сбой удаления не роняет отчёт):
	// 1) привязка протухла — у вагона НОВЫЙ рейс (дата приёма позже привязки);
	//    гасим в любом режиме, распознание положительное;
	// 2) вагон выпал из подхода (прибыл/выбыл) — только в режиме по умолчанию:
	//    в naznachOnly уходящие «НА {сосед}» скрыты отбором, но живы;
	// 3) страховка от вечных сирот (отчёт могли долго не строить):
	//    всё старше nmtpBindingMaxAge.
	var dead []string
	for v := range stale {
		dead = append(dead, v)
	}
	if !naznachOnly {
		for _, v := range ours {
			if !seen[v] && !stale[v] {
				dead = append(dead, v)
			}
		}
	}
	_ = s.repo.DeleteVagonColumns(ctx, dead)
	_ = s.repo.DeleteVagonColumnsBefore(ctx, clock.Now().Time().Add(-nmtpBindingMaxAge))
	return report, nil
}

// nmtpOverride — привязка вагона в терминах агрегации: индекс колонки в cols
// и момент назначения (для протухания по новому рейсу).
type nmtpOverride struct {
	col     int
	created time.Time
}

// nmtpLayout — раскладка вагона в колонку формы: правила nmtp_column (матч по
// специфичности) + ручные привязки. Общая для агрегации отчёта и MoveTrain,
// чтобы «что видит пользователь» и «что переносится» никогда не расходились.
type nmtpLayout struct {
	matchers   []nmtpMatcher
	checkOrder []int
	otherIdx   int
	norm       *MarkNormalizer
}

func newNmtpLayout(cols []domain.NmtpColumn, markDict []string) *nmtpLayout {
	l := &nmtpLayout{
		matchers: make([]nmtpMatcher, len(cols)),
		otherIdx: len(cols),
		norm:     NewMarkNormalizer(markDict),
	}
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
		l.matchers[i] = m
	}
	// Порядок проверки: специфичные раньше («СЛЯБЫ» раньше «ПРОКАТ» без марки);
	// порядок отображения (order) не трогаем.
	l.checkOrder = make([]int, len(l.matchers))
	for i := range l.checkOrder {
		l.checkOrder[i] = i
	}
	sort.SliceStable(l.checkOrder, func(a, b int) bool {
		ma, mb := l.matchers[l.checkOrder[a]], l.matchers[l.checkOrder[b]]
		if ma.specificity != mb.specificity {
			return ma.specificity > mb.specificity
		}
		return ma.order < mb.order
	})
	return l
}

// columnOf — колонка вагона: живая привязка сильнее правил; привязка старого
// рейса (дата приёма позже назначения) протухает — stale=true, вагон по
// правилам. Не сматчился ничем — otherIdx («прочее»).
func (l *nmtpLayout) columnOf(rec *domain.Dislocation, overrides map[string]nmtpOverride) (col int, applied, stale bool) {
	ov, hasOv := overrides[rec.Vagon]
	if hasOv && rec.DateNach != nil && rec.DateNach.Time().After(ov.created) {
		hasOv = false
		stale = true
	}
	if hasOv {
		return ov.col, true, false
	}
	mark := l.norm.Mark(rec.FreightExactName, rec.CargoSms)
	client := strings.ToUpper(strings.TrimSpace(rec.Client))
	station := strings.ToUpper(strings.TrimSpace(rec.StationNach))
	for _, mi := range l.checkOrder {
		m := &l.matchers[mi]
		if m.clients != nil && !m.clients[client] {
			continue
		}
		if m.stations != nil && !m.stations[station] {
			continue
		}
		if m.marks != nil && !m.marks[mark] {
			continue
		}
		return m.order, false, stale
	}
	return l.otherIdx, false, stale
}

// nmtpFromOther — сентинел «исходная колонка — „прочее"» для MoveTrain:
// у колонки «прочее» нет id в nmtp_column, а переносить из неё нужно.
const nmtpFromOther int64 = -1

// nmtpBindingMaxAge — страховочный срок жизни привязки «вагон → колонка»:
// рейс длится недели, 60 суток с запасом. Основные механизмы гашения — новый
// рейс и выпадение из подхода; возраст ловит сирот, когда отчёт долго не строят.
const nmtpBindingMaxAge = 60 * 24 * time.Hour

// vagonOverrides — привязки вагонов к колонкам данного терминала: карта
// vagon → {индекс колонки, момент назначения} и список «наших» вагонов (для
// гашения). Привязки чужих терминалов игнорируются и не гасятся.
func (s *NmtpService) vagonOverrides(ctx context.Context, cols []domain.NmtpColumn) (map[string]nmtpOverride, []string) {
	all, err := s.repo.VagonColumns(ctx)
	if err != nil || len(all) == 0 {
		return nil, nil // best-effort: без привязок отчёт живёт по правилам
	}
	byID := map[int64]int{}
	for i, c := range cols {
		byID[c.ID] = i
	}
	overrides := map[string]nmtpOverride{}
	var ours []string
	for v, b := range all {
		if idx, ok := byID[b.ColumnID]; ok {
			overrides[v] = nmtpOverride{col: idx, created: b.CreatedAt.Time()}
			ours = append(ours, v)
		}
	}
	return overrides, ours
}

// MoveTrain — ручной перенос поезда в колонку (указание грузовладельца):
// привязка пишется поимённо по номерам вагонов (nmtp_vagon_column) и переживает
// переформирование поезда. columnID = 0 — снять привязку («вернуть по
// правилам»). fromColumnID сужает перенос до одной группы состава: > 0 —
// вагоны, показанные сейчас в этой колонке; nmtpFromOther — вагоны «прочего»;
// 0 — весь состав (сборные поезда: переносить группу, не весь поезд —
// замечание владельца 30.07.2026). Возвращает число вагонов.
func (s *NmtpService) MoveTrain(ctx context.Context, terminal, index, station string,
	prog *domain.LocalTime, columnID, fromColumnID int64, who string) (int, error) {
	if _, ok := s.dir.PortByNameS(terminal); !ok {
		return 0, fmt.Errorf("неизвестный терминал: %s", terminal)
	}
	cols, err := s.repo.Columns(ctx, terminal)
	if err != nil {
		return 0, err
	}
	colIdx := func(id int64) int {
		for i, c := range cols {
			if c.ID == id {
				return i
			}
		}
		return -1
	}
	if columnID != 0 && colIdx(columnID) < 0 {
		return 0, fmt.Errorf("колонка %d не принадлежит терминалу %s", columnID, terminal)
	}
	// Фильтр «откуда»: индекс исходной колонки в раскладке (или «прочее»).
	fromIdx := -1
	switch {
	case fromColumnID == nmtpFromOther:
		fromIdx = len(cols) // otherIdx
	case fromColumnID != 0:
		if fromIdx = colIdx(fromColumnID); fromIdx < 0 {
			return 0, fmt.Errorf("исходная колонка %d не принадлежит терминалу %s", fromColumnID, terminal)
		}
	}
	if prog == nil {
		return 0, fmt.Errorf("не задан прогноз поезда (ключ строки)")
	}

	// Текущая колонка вагона считается ТОЙ ЖЕ раскладкой, что отчёт (правила +
	// живые привязки) — переносится ровно то, что пользователь видит в колонке.
	var layout *nmtpLayout
	var overrides map[string]nmtpOverride
	if fromIdx >= 0 {
		marks, err := s.repo.Marks(ctx)
		if err != nil {
			return 0, err
		}
		layout = newNmtpLayout(cols, marks)
		overrides, _ = s.vagonOverrides(ctx, cols)
	}

	// Вагоны состава — тем же отбором и ключом строки, что агрегация отчёта.
	progKey := prog.Time().Format("2006-01-02 15:04")
	trainFound := false
	var vagons []string
	for _, rec := range s.actual.All() {
		if rec.GruzpolS != terminal && rec.Naznach != terminal {
			continue
		}
		if rec.Status != nil && (*rec.Status == 10 || *rec.Status == 12) {
			continue
		}
		if rec.ProgJd == nil || rec.ProgJd.Time().Format("2006-01-02 15:04") != progKey {
			continue
		}
		idx := rec.Index
		if rec.IndexPp != "" {
			idx = rec.IndexPp
		}
		if idx != index || rec.StationOper != station {
			continue
		}
		trainFound = true
		if fromIdx >= 0 {
			if col, _, _ := layout.columnOf(&rec, overrides); col != fromIdx {
				continue
			}
		}
		vagons = append(vagons, rec.Vagon)
	}
	if !trainFound {
		return 0, fmt.Errorf("поезд %s (%s) не найден в подходе — обновите отчёт", index, station)
	}
	if len(vagons) == 0 {
		return 0, fmt.Errorf("в выбранной колонке у поезда %s нет вагонов — обновите отчёт", index)
	}
	if columnID == 0 {
		return len(vagons), s.repo.DeleteVagonColumns(ctx, vagons)
	}
	return len(vagons), s.repo.SetVagonColumns(ctx, vagons, columnID, who)
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

// buildNmtpReport — чистая агрегация (покрыта тестами). overrides — ручные
// привязки «вагон → колонка» (сильнее правил); возвращает также, какие из
// привязанных вагонов применились (seen) и какие протухли из-за нового рейса
// вагона — дата приёма позже привязки (stale, кандидаты на гашение).
func buildNmtpReport(records []domain.Dislocation, cols []domain.NmtpColumn, markDict []string,
	terminal string, termStations []string, brosDates map[string]*domain.LocalTime,
	naznachOnly bool, overrides map[string]nmtpOverride) (domain.NmtpReport, map[string]bool, map[string]bool) {
	seenOverrides := map[string]bool{}
	staleOverrides := map[string]bool{}

	layout := newNmtpLayout(cols, markDict)
	nCols := len(cols) + 1 // + «прочее»
	otherIdx := layout.otherIdx

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
			// Примечание: смена индекса «был NNN» (середина index_main, как
			// gtport buildPrimFields) + пометка перестановки, через запятую.
			note := ""
			if rec.IndexMain != "" && rec.IndexMain != idx {
				if parts := strings.Split(rec.IndexMain, "-"); len(parts) == 3 && len(parts[1]) >= 3 {
					note = "был " + parts[1]
				}
			}
			if rec.GruzpolS != rec.Naznach && rec.GruzpolS != "" && rec.Naznach != "" {
				if note != "" {
					note += ", "
				}
				if rec.Naznach == terminal {
					note += "С " + rec.GruzpolS
				} else {
					note += "НА " + rec.Naznach
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

		// Раскладка вагона в колонку (привязка сильнее правил, см. columnOf).
		col, applied, staleOv := layout.columnOf(rec, overrides)
		if applied {
			seenOverrides[rec.Vagon] = true
		}
		if staleOv {
			staleOverrides[rec.Vagon] = true
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
		report.Columns = append(report.Columns, domain.NmtpColumnHead{ID: c.ID, Group: c.GroupLabel, Station: c.StationLabel, Mark: c.MarkLabel})
	}

	// Секции в порядке файла: терминальные станции → дороги (восток → запад).
	type sectionKey struct {
		label     string
		near      bool
		isStation bool
	}
	var keys []sectionKey
	for _, st := range termStations {
		keys = append(keys, sectionKey{st, true, true})
	}
	for _, r := range nmtpRoads {
		keys = append(keys, sectionKey{r.Label, nmtpNearRoads[r.Code], false})
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
			keys = append(keys, sectionKey{label, false, false})
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
			out = append(out, domain.NmtpSection{Label: k.label, Near: k.near, IsStation: k.isStation, Rows: rows, Total: total})
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

	return report, seenOverrides, staleOverrides
}
