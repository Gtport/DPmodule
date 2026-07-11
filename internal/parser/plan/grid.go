package plan

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/xuri/excelize/v2"
)

// hasLetter — есть ли в строке хоть одна буква. Имя столбца-листа (терминал/груз) —
// текст; строки перерабатывающей способности и «Остатка на 18:00» содержат только
// числа, и их НЕ надо принимать за имя листа (иначе метка получается «НМТП 160»).
func hasLetter(s string) bool {
	return strings.IndexFunc(s, unicode.IsLetter) >= 0
}

// GridParser — универсальный парсер «новой формы» плана подвода. Формат один для
// всех станций: шапка с «N п/п», блоки «План на DD-MM-YYYY», строки поездов
// (числовой N п/п), терминалы с подстолбцами грузов. Специфика станции — только
// в профиле (какие терминалы «наши» → Activ).
type GridParser struct {
	prof Profile
}

// NewGridParser строит generic-парсер для профиля станции.
func NewGridParser(p Profile) *GridParser { return &GridParser{prof: p} }

// leafCol — листовой столбец терминала: индекс столбца, метка (терминал+груз) и
// признак «наш» причал (для суммы Activ и фильтра «чужих» на фронте).
type leafCol struct {
	col   int
	label string
	isOur bool
}

// gridCols — найденные ключевые столбцы листа.
type gridCols struct {
	colIndex   int       // «Индекс» (индекс поезда)
	colPlan    int       // «План» — время нитки HH:MM
	colFact    int       // «Факт» — HH:MM или пусто
	colKolVag  int       // «Кол. ваг.» — всего вагонов в поезде
	colComment int       // «Комментарий»
	colStation int       // «Станция текущей операции» (нужна для с.ф.; пока не используется)
	rowHeader  int       // строка с «N п/п»
	leaves     []leafCol // листовые столбцы ВСЕХ терминалов (для Ports); isOur → Activ
}

// ─────────────────────────────────────────────────────────────────────────────
//  Чтение листа
// ─────────────────────────────────────────────────────────────────────────────

// ReadPlanSheet открывает .xlsx, берёт ПОСЛЕДНИЙ лист, снимает объединение ячеек
// (шапка плана — сплошь merge; без разъединения имена столбцов не читаются) и
// возвращает строки как [][]string. Вызывающий получает готовую сетку.
func ReadPlanSheet(path string) ([][]string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("открытие файла: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("файл не содержит листов")
	}
	sheet := sheets[len(sheets)-1]

	merged, err := f.GetMergeCells(sheet)
	if err != nil {
		return nil, fmt.Errorf("чтение объединённых ячеек: %w", err)
	}
	for _, mc := range merged {
		if err := f.UnmergeCell(sheet, mc.GetStartAxis(), mc.GetEndAxis()); err != nil {
			return nil, fmt.Errorf("разъединение ячеек: %w", err)
		}
	}

	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("чтение строк: %w", err)
	}
	return rows, nil
}

// ─────────────────────────────────────────────────────────────────────────────
//  Разбор
// ─────────────────────────────────────────────────────────────────────────────

// Parse разбирает строки листа в PlanDoc.
func (g *GridParser) Parse(rows [][]string, sourceFile string) (*PlanDoc, error) {
	cols, err := g.findColumns(rows)
	if err != nil {
		return nil, err
	}
	nitki, err := g.collect(rows, cols)
	if err != nil {
		return nil, err
	}
	return &PlanDoc{
		PlanCode:   g.prof.PlanCode,
		SourceFile: filepath.Base(sourceFile),
		Nitki:      nitki,
	}, nil
}

// findColumns находит строку шапки и ключевые столбцы; классифицирует листовые
// столбцы терминалов и отбирает «наши» (для Activ).
func (g *GridParser) findColumns(rows [][]string) (gridCols, error) {
	cols := gridCols{colIndex: -1, colPlan: -1, colFact: -1, colKolVag: -1, colComment: -1, colStation: -1, rowHeader: -1}

	// 1. Строка шапки: ищем «N п/п».
	for r := 0; r < min(6, len(rows)); r++ {
		for c := 0; c < min(5, len(rows[r])); c++ {
			cell := strings.TrimSpace(rows[r][c])
			if cell == "N п/п" || cell == "№ п/п" || strings.HasPrefix(cell, "N п") || strings.HasPrefix(cell, "№ п") {
				cols.rowHeader = r
				break
			}
		}
		if cols.rowHeader != -1 {
			break
		}
	}
	if cols.rowHeader == -1 {
		return cols, fmt.Errorf("plan[%s]: не найдена строка шапки с «N п/п» (не «новая форма»?)", g.prof.PlanCode)
	}
	row1 := cols.rowHeader

	// 2. Ключевые столбцы — по имени в строке шапки.
	for c, cell := range rows[row1] {
		cell = strings.TrimSpace(cell)
		switch {
		case cell == "Индекс":
			cols.colIndex = c
		case cell == "План":
			cols.colPlan = c
		case cell == "Факт":
			cols.colFact = c
		case strings.HasPrefix(cell, "Кол. ваг") || cell == "Кол.ваг.":
			cols.colKolVag = c
		case cell == "Комментарий":
			cols.colComment = c
		case (strings.Contains(cell, "Станция текущей") || strings.Contains(cell, "текущей операции")) &&
			!strings.Contains(cell, "Время") && !strings.Contains(cell, "время"):
			cols.colStation = c
		}
	}
	if cols.colIndex == -1 {
		return cols, fmt.Errorf("plan[%s]: не найден столбец «Индекс» в строке %d", g.prof.PlanCode, row1)
	}
	if cols.colPlan == -1 {
		return cols, fmt.Errorf("plan[%s]: не найден столбец «План» в строке %d", g.prof.PlanCode, row1)
	}

	// «Кол. ваг.» в новой форме обычно отсутствует как отдельный столбец: всего
	// вагонов поезда — это столбец «Итого» в строке терминалов (row1+1), стоящий
	// перед первым терминалом. Берём первый итоговый столбец как счётчик вагонов.
	if cols.colKolVag == -1 && row1+1 < len(rows) {
		for c, cell := range rows[row1+1] {
			u := strings.ToUpper(strings.TrimSpace(cell))
			if strings.Contains(u, "ИТОГО") || strings.Contains(u, "TOTAL") {
				cols.colKolVag = c
				break
			}
		}
	}

	// 3. Классификация листовых столбцов терминалов (все, с метками и признаком «наш»).
	cols.leaves = g.findLeaves(rows, row1)

	return cols, nil
}

// findLeaves определяет ВСЕ листовые (не агрегатные) подстолбцы терминалов с их
// метками (терминал + груз) и признаком «наш» (имя терминала ∈ profile.OurTerminals).
// «Наши» листья суммируются в Activ; полный список идёт в Ports нитки (столбцы
// портов таблицы) и позволяет фронту фильтровать «чужие» строки.
//
// Терминалы задаёт строка row1+1; их подзаголовки-грузы — строки row1+2..row1+4.
// «Листовой» столбец — самый глубокий непустой-не-ИТОГО подзаголовок без детей на
// следующем уровне. Алгоритм классификации листьев дословно повторяет эталон GTport.
func (g *GridParser) findLeaves(rows [][]string, row1 int) []leafCol {
	// Подзаголовочные строки для анализа листьев.
	var subRows [][]string
	for off := 2; off <= 4; off++ {
		if row1+off < len(rows) {
			subRows = append(subRows, rows[row1+off])
		}
	}

	isTotal := func(s string) bool {
		u := strings.ToUpper(strings.TrimSpace(s))
		return strings.Contains(u, "ИТОГО") || strings.Contains(u, "TOTAL")
	}

	// getLeafName: самый глубокий текстовый (не пустой, не «ИТОГО», не чисто числовой)
	// подзаголовок столбца col. Числовые строки (перераб. способность, остаток) —
	// не имена листьев, пропускаем, иначе метка «НМТП 160» вместо «НМТП Каменный уголь».
	getLeafName := func(col int) string {
		for i := len(subRows) - 1; i >= 0; i-- {
			if col >= len(subRows[i]) {
				continue
			}
			v := strings.TrimSpace(subRows[i][col])
			if v != "" && !isTotal(v) && hasLetter(v) {
				return v
			}
		}
		return ""
	}

	// isAggregateInLevel: у столбца col на уровне levelIdx есть дочерние листья на
	// уровне levelIdx+1 (пустые в текущем уровне, но непустые-не-ИТОГО ниже).
	isAggregateInLevel := func(levelIdx, col int) bool {
		if levelIdx+1 >= len(subRows) {
			return false
		}
		level0, level1 := subRows[levelIdx], subRows[levelIdx+1]
		nextInLevel := len(level0)
		for c2 := col + 1; c2 < len(level0); c2++ {
			if strings.TrimSpace(level0[c2]) != "" {
				nextInLevel = c2
				break
			}
		}
		for c2 := col + 1; c2 < nextInLevel && c2 < len(level1); c2++ {
			if strings.TrimSpace(level0[c2]) != "" {
				break
			}
			v1 := strings.TrimSpace(level1[c2])
			if v1 != "" && !isTotal(v1) && hasLetter(v1) {
				return true
			}
		}
		return false
	}

	// isLeafCol: столбец имеет непустое-не-ИТОГО имя и не является агрегатом.
	isLeafCol := func(col int) bool {
		if len(subRows) == 0 {
			return false
		}
		if getLeafName(col) == "" {
			return false
		}
		for i := 0; i < len(subRows)-1; i++ {
			if col < len(subRows[i]) {
				v := strings.TrimSpace(subRows[i][col])
				if v != "" && !isTotal(v) && isAggregateInLevel(i, col) {
					return false
				}
			}
		}
		return true
	}

	// Терминалы: непустые ячейки строки row1+1, кроме итогового столбца всего поезда.
	type terminal struct {
		start int
		name  string
	}
	var terminals []terminal
	if row1+1 < len(rows) {
		for c, cell := range rows[row1+1] {
			cell = strings.TrimSpace(cell)
			if cell == "" || isTotal(cell) {
				continue // пусто или «Итого» всего поезда — не терминал
			}
			terminals = append(terminals, terminal{start: c, name: cell})
		}
	}

	var leaves []leafCol
	for tIdx, term := range terminals {
		isOur := g.prof.isOurTerminal(term.name)
		termEnd := 1 << 30
		if tIdx+1 < len(terminals) {
			termEnd = terminals[tIdx+1].start
		}
		before := len(leaves)
		for c := term.start + 1; c < termEnd; c++ {
			if isLeafCol(c) {
				leaves = append(leaves, leafCol{col: c, label: leafLabel(term.name, getLeafName(c)), isOur: isOur})
			}
		}
		// Фолбэк: терминал без детализации (единственный итоговый столбец) —
		// берём сам заголовочный столбец как источник данных.
		if len(leaves) == before {
			leaves = append(leaves, leafCol{col: term.start, label: term.name, isOur: isOur})
		}
	}
	return leaves
}

// leafLabel строит метку столбца порта «терминал груз» без дублирования (если имя
// листа пустое или совпадает с терминалом — только терминал).
func leafLabel(terminal, leaf string) string {
	terminal = strings.TrimSpace(terminal)
	leaf = strings.TrimSpace(leaf)
	if leaf == "" || leaf == terminal {
		return terminal
	}
	return terminal + " " + leaf
}

// collect собирает нитки из строк листа. С.ф.-строки пока пропускаются (перенос
// распределения с.ф. — отдельный шаг), их число выводится в лог.
func (g *GridParser) collect(rows [][]string, cols gridCols) ([]PlanNitka, error) {
	var nitki []PlanNitka
	var blockDate time.Time
	sfEmitted, sfSkipped := 0, 0
	trains := 0            // число реальных ниток поездов (для гарда «нет поездов»)
	ostatokDone := false   // «Остаток на 18:00» эмитим один раз (первую строку)

	getCell := func(row []string, col int) string {
		if col < 0 || col >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[col])
	}

	for r := cols.rowHeader + 1; r < len(rows); r++ {
		if len(rows[r]) == 0 {
			continue
		}
		col0 := getCell(rows[r], 0)
		label := getCell(rows[r], cols.colIndex)

		// Заголовок блока дат: «План на DD-MM-YYYY» в col0.
		if strings.HasPrefix(strings.ToLower(col0), "план на") {
			if d := parseBlockDate(col0); !d.IsZero() {
				blockDate = d
			}
			continue
		}
		// «Остаток на 18:00» — служебная строка с числами по портам на момент 18:00.
		// Эмитим первую как спец-строку таблицы (не нитка поезда); остальные пропускаем.
		if isOstatokLabel(rows[r], cols.colIndex) {
			if !ostatokDone {
				ports, _ := buildPorts(rows[r], cols)
				nitki = append(nitki, PlanNitka{
					IndexPp:   ostatokMarker,
					Wagons:    atoiSafe(getCell(rows[r], cols.colKolVag)),
					Ports:     ports,
					IsOstatok: true,
				})
				ostatokDone = true
			}
			continue
		}
		switch label {
		case "Прибыло + Ост.18:00", "План выгрузки", "Остаток", "Перераб. спос.", "Заказ":
			continue
		}

		// С.ф.-строка с пустым порядковым номером (маркер «с.ф.» в «Индекс»).
		if col0 == "" {
			if isSfRow(label) {
				if n, ok := g.buildSfNitka(rows[r], cols, blockDate); ok {
					nitki = append(nitki, n)
					sfEmitted++
				} else {
					sfSkipped++
				}
			}
			continue
		}
		// Строка поезда: col0 — числовой порядковый номер.
		if !isAllDigits(col0) {
			continue
		}
		if blockDate.IsZero() {
			continue // нет даты блока — построить время нельзя
		}
		index := getCell(rows[r], cols.colIndex)
		if index == "" {
			continue // свободная нитка без индекса — эталон её не эмитит
		}
		// С.ф.-строка с порядковым номером («с.ф.БИКИН», «0000-000-0000»).
		if isSfRow(index) {
			if n, ok := g.buildSfNitka(rows[r], cols, blockDate); ok {
				nitki = append(nitki, n)
				sfEmitted++
			} else {
				sfSkipped++
			}
			continue
		}

		nitki = append(nitki, g.buildNitka(rows[r], cols, blockDate))
		trains++
	}

	if sfEmitted > 0 || sfSkipped > 0 {
		fmt.Printf("[plan:%s] с.ф.-строк: эмитировано %d, пропущено без станции %d\n", g.prof.PlanCode, sfEmitted, sfSkipped)
	}
	if trains == 0 {
		return nil, fmt.Errorf("plan[%s]: не найдено строк поездов", g.prof.PlanCode)
	}
	return nitki, nil
}

// buildNitka строит нитку из строки поезда.
func (g *GridParser) buildNitka(row []string, cols gridCols, blockDate time.Time) PlanNitka {
	get := func(col int) string {
		if col < 0 || col >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[col])
	}

	planJd := combineDateTime(blockDate, get(cols.colPlan)) // ЖД-время, без сдвига
	planMsk := applyMskRule(planJd)
	factMsk := applyMskRule(combineDateTime(blockDate, get(cols.colFact)))

	ports, activ := buildPorts(row, cols)

	index := get(cols.colIndex)
	return PlanNitka{
		Index:   index,
		IndexPp: index, // нормализация с.ф. — позже; для обычного поезда = Index
		PlanJd:  planJd,
		PlanMsk: planMsk,
		FactMsk: factMsk,
		Otkl:    formatOtkl(planMsk, factMsk),
		Wagons:  atoiSafe(get(cols.colKolVag)),
		Activ:   activ,
		Ports:   ports,
		Comment: get(cols.colComment),
	}
}

// buildSfNitka строит нитку сборного формирования (с.ф.): как обычную, но с флагом
// IsSf и нормализованным индексом «с.ф.<СИНОНИМ>». Синоним — из суффикса индекса или
// «Станции текущей операции». Если синоним определить нельзя — (нулевая, false).
func (g *GridParser) buildSfNitka(row []string, cols gridCols, blockDate time.Time) (PlanNitka, bool) {
	get := func(col int) string {
		if col < 0 || col >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[col])
	}
	syn := sfSynonym(get(cols.colIndex), get(cols.colStation))
	if syn == "" {
		return PlanNitka{}, false
	}
	n := g.buildNitka(row, cols, blockDate)
	n.Index = ""
	n.IndexPp = "с.ф." + syn
	n.IsSf = true
	return n, true
}

// buildPorts собирает ячейки портов строки по листовым столбцам (в порядке
// столбцов файла) и сумму «наших» (Activ). Общий для нитки и строки «Остаток».
func buildPorts(row []string, cols gridCols) ([]PortCell, int) {
	get := func(col int) string {
		if col < 0 || col >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[col])
	}
	ports := make([]PortCell, 0, len(cols.leaves))
	activ := 0
	for _, lf := range cols.leaves {
		n := atoiSafe(get(lf.col))
		ports = append(ports, PortCell{Label: lf.label, Count: n, IsOur: lf.isOur})
		if lf.isOur {
			activ += n
		}
	}
	return ports, activ
}

// ─────────────────────────────────────────────────────────────────────────────
//  Мелкие утилиты
// ─────────────────────────────────────────────────────────────────────────────

var blockDateRe = regexp.MustCompile(`(\d{2})[-.](\d{2})[-.](\d{4})`)

// parseBlockDate извлекает дату из «План на DD-MM-YYYY» (или «DD.MM.YYYY»).
// Время строится naive (канон: без таймзон и сдвигов).
func parseBlockDate(text string) time.Time {
	m := blockDateRe.FindStringSubmatch(text)
	if len(m) < 4 {
		return time.Time{}
	}
	t, err := time.Parse("02-01-2006", m[1]+"-"+m[2]+"-"+m[3])
	if err != nil {
		return time.Time{}
	}
	return t
}

// combineDateTime собирает дату блока + время «HH:MM» в naive time.Time.
// Возвращает нулевое время, если строка не содержит времени.
func combineDateTime(bd time.Time, hhmm string) time.Time {
	if bd.IsZero() || !strings.Contains(hhmm, ":") {
		return time.Time{}
	}
	pt, err := time.Parse("15:04", hhmm)
	if err != nil {
		return time.Time{}
	}
	return time.Date(bd.Year(), bd.Month(), bd.Day(), pt.Hour(), pt.Minute(), 0, 0, bd.Location())
}

// applyMskRule применяет бизнес-правило «час ≥ 18 → предыдущие операционные сутки».
// Это НЕ таймзонный сдвиг, а смещение операционного календаря предприятия.
func applyMskRule(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	if t.Hour() >= 18 {
		return t.AddDate(0, 0, -1)
	}
	return t
}

// formatOtkl форматирует отклонение «факт − план» как «±HH:MM».
func formatOtkl(plan, fact time.Time) string {
	if plan.IsZero() || fact.IsZero() {
		return ""
	}
	d := fact.Sub(plan)
	sign := ""
	if d < 0 {
		sign = "-"
		d = -d
	}
	return fmt.Sprintf("%s%02d:%02d", sign, int(d.Hours()), int(d.Minutes())%60)
}

// isAllDigits — строка состоит только из цифр (непустая).
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// atoiSafe парсит целое; пусто/мусор → 0.
func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// isSfRow — строка является сборным формированием (с.ф.): индекс несёт маркер «с.ф.»/«сф»
// (с суффиксом станции или без) либо спецкод «0000-000-0000». Перенос эталона GTport
// (шире прежнего isSfIndex — ловит и «с.ф.БИКИН», иначе такая строка утекала в обычные).
func isSfRow(index string) bool {
	u := strings.ToUpper(strings.TrimSpace(index))
	if u == "0000-000-0000" {
		return true
	}
	collapsed := strings.ReplaceAll(strings.ReplaceAll(u, ".", ""), " ", "")
	return strings.HasPrefix(collapsed, "СФ")
}

// sfSynonym извлекает синоним станции формирования (ВЕРХНИЙ регистр): из суффикса
// индекса («с.ф.БИКИН» → «БИКИН») или, для бесстанционных («с.ф.»/«0000-000-0000»),
// из «Станции текущей операции» (station). Пусто — если станцию определить нельзя.
func sfSynonym(index, station string) string {
	u := strings.ToUpper(strings.TrimSpace(index))
	if u != "0000-000-0000" {
		if suf := strings.TrimSpace(strings.TrimPrefix(strings.ReplaceAll(u, ".", ""), "СФ")); suf != "" {
			return suf
		}
	}
	return strings.ToUpper(strings.TrimSpace(station))
}

// ostatokMarker — метка служебной строки «Остаток на 18:00» (числа по портам на 18:00).
const ostatokMarker = "Остаток на 18:00"

// isOstatokLabel — строка «Остаток на 18:00» (в colIndex либо в первых столбцах).
func isOstatokLabel(row []string, colIndex int) bool {
	if colIndex >= 0 && colIndex < len(row) && strings.TrimSpace(row[colIndex]) == ostatokMarker {
		return true
	}
	for _, c := range []int{0, 1, 2} {
		if c != colIndex && c < len(row) && strings.TrimSpace(row[c]) == ostatokMarker {
			return true
		}
	}
	return false
}
