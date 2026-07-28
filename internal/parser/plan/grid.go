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

// GridParser — универсальный парсер «новой формы» плана подвода. Общее у всех
// станций — не геометрия, а состав: блоки «План на DD-MM-YYYY», строки поездов
// (индекс 4-3-4 или с.ф.), терминалы с подстолбцами грузов. Специфика станции —
// только в профиле (какие терминалы «наши» → Activ).
//
// ⚠️ Лист верстают руками, и раскладка плывёт (по книге Находки за июль: «Индекс»
// то в B, то в C; «Кол. ваг.» то подписан, то приходит как «Итого», то отсутствует;
// уровней шапки 2, 3 или 4; 29.07 терминалы переехали в САМУ строку подписей).
// Поэтому парсер не считает смещений от строки шапки, а ищет опоры по содержимому:
// строку шапки — по «План», строку терминалов — по именам вне словаря служебных
// подписей, недостающие ключевые столбцы — по данным строк поездов.
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
	colStation int       // «Станция текущей операции» (нужна для с.ф.)
	rowHeader  int       // строка подписей столбцов (опора — «План»)
	rowTerm    int       // строка имён терминалов (может совпадать с rowHeader)
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
	// Распознанная геометрия — в лог: раскладка листа плывёт, и при разборе спорного
	// файла это первое, что нужно видеть (что парсер счёл шапкой и терминалами).
	ourLeaves := 0
	for _, lf := range cols.leaves {
		if lf.isOur {
			ourLeaves++
		}
	}
	fmt.Printf("[plan:%s] шапка стр.%d, терминалы стр.%d; столбцы: индекс=%s план=%s факт=%s ваг=%s станция=%s коммент=%s; листьев %d, из них наших %d\n",
		g.prof.PlanCode, cols.rowHeader+1, cols.rowTerm+1,
		colName(cols.colIndex), colName(cols.colPlan), colName(cols.colFact), colName(cols.colKolVag),
		colName(cols.colStation), colName(cols.colComment), len(cols.leaves), ourLeaves)

	nitki, err := g.collect(rows, cols)
	if err != nil {
		return nil, err
	}
	return &PlanDoc{
		PlanCode:    g.prof.PlanCode,
		SourceFile:  filepath.Base(sourceFile),
		StationName: titleStation(rows),
		Nitki:       nitki,
	}, nil
}

// titleRe — заголовок файла «План подвода поездов к станции X на DD.MM.YYYY».
// Имя станции — до « на <дата>» (в имени станции может быть пробел: МЫС АСТАФЬЕВА).
var titleRe = regexp.MustCompile(`(?i)к\s+станции\s+(.+?)\s+на\s+\d{2}[.\-/]\d{2}[.\-/]\d{4}`)

// titleStation извлекает имя станции из строки-заголовка (первые строки листа,
// до шапки с «N п/п»). Возвращает имя ВЕРХНИМ регистром; «» — заголовок не найден.
// Используется гардом «файл не той станции» в сервисе загрузки плана.
func titleStation(rows [][]string) string {
	for r := 0; r < min(6, len(rows)); r++ {
		for c := 0; c < min(8, len(rows[r])); c++ {
			if m := titleRe.FindStringSubmatch(rows[r][c]); m != nil {
				return strings.ToUpper(strings.Join(strings.Fields(m[1]), " "))
			}
		}
	}
	return ""
}

// findColumns находит строку шапки, строку терминалов и ключевые столбцы;
// классифицирует листовые столбцы терминалов и отбирает «наши» (для Activ).
func (g *GridParser) findColumns(rows [][]string) (gridCols, error) {
	cols := gridCols{colIndex: -1, colPlan: -1, colFact: -1, colKolVag: -1, colComment: -1, colStation: -1, rowHeader: -1, rowTerm: -1}

	// 1. Строка шапки. Опора — «План»: единственная подпись, которая есть во ВСЕХ
	// виденных раскладках обеих станций. «N п/п» опорой быть не может — в листе от
	// 29.07 её не проставили, и по ней разбор падал целиком.
	cols.rowHeader = findHeaderRow(rows)
	if cols.rowHeader == -1 {
		return cols, fmt.Errorf("plan[%s]: не найдена строка шапки со столбцом «План» (не «новая форма»?)", g.prof.PlanCode)
	}
	row1 := cols.rowHeader

	// 2. Ключевые столбцы — по подписи в строке шапки. Чего не подписали, ниже
	// доопределим по данным.
	for c, cell := range rows[row1] {
		cell = strings.TrimSpace(cell)
		switch {
		case cell == "Индекс":
			cols.colIndex = c
		case strings.EqualFold(cell, "План"):
			cols.colPlan = c
		case strings.EqualFold(cell, "Факт"):
			cols.colFact = c
		case strings.HasPrefix(cell, "Кол. ваг") || cell == "Кол.ваг.":
			cols.colKolVag = c
		case cell == "Комментарий" || cell == "Примечание":
			cols.colComment = c
		case (strings.Contains(cell, "Станция текущей") || strings.Contains(cell, "текущей операции")) &&
			!strings.Contains(cell, "Время") && !strings.Contains(cell, "время"):
			cols.colStation = c
		}
	}
	if cols.colPlan == -1 {
		return cols, fmt.Errorf("plan[%s]: не найден столбец «План» в строке %d", g.prof.PlanCode, row1)
	}

	// 3. Строка терминалов. Обычно следующая за шапкой, но 29.07 по Находке имена
	// терминалов встали в саму строку подписей — считать «шапка+1» нельзя.
	cols.rowTerm = findTerminalRow(rows, row1, cols.colPlan)

	// 4. Столбец «Индекс» без подписи — по данным: где чаще всего встречается
	// валидный индекс поезда 4-3-4 или маркер с.ф.
	if cols.colIndex == -1 {
		cols.colIndex = guessIndexCol(rows, cols.rowTerm)
	}
	if cols.colIndex == -1 {
		return cols, fmt.Errorf("plan[%s]: не найден столбец «Индекс» (нет подписи и нет строк с индексом 4-3-4)", g.prof.PlanCode)
	}

	// 5. Классификация листовых столбцов терминалов (все, с метками и признаком «наш»).
	cols.leaves = g.findLeaves(rows, cols.rowTerm)

	// 6. «Кол. ваг.» без подписи — это «Итого» строки терминалов, стоящее ЛЕВЕЕ
	// первого терминала (итог всего поезда). Раньше брали первое «ИТОГО» в строке,
	// а в форме 29.07 первым идёт «ИТОГО» терминала Сухой порт — и в вагонах поезда
	// оказывалось его число.
	if cols.colKolVag == -1 {
		cols.colKolVag = findTrainTotalCol(rows, cols.rowTerm, cols.colPlan, firstLeafCol(cols.leaves))
	}

	// 7. Станция текущей операции (нужна с.ф.) и комментарий без подписей — по данным.
	if cols.colStation == -1 {
		cols.colStation = guessStationCol(rows, cols)
	}
	if cols.colComment == -1 {
		cols.colComment = guessCommentCol(rows, cols)
	}

	return cols, nil
}

// headerWords — служебные подписи столбцов во всех виденных раскладках. Словарь
// нужен ровно для одного: отличить строку подписей от строки терминалов. Имя
// терминала — это текст, которого в словаре НЕТ. Здесь только слова бланка,
// никаких имён портов (хардкод портов запрещён каноном).
var headerWords = map[string]bool{
	"индекс":                               true,
	"план":                                 true,
	"факт":                                 true,
	"номер поезда":                         true,
	"станция текущей операции":             true,
	"время текущей операции":               true,
	"комментарий":                          true,
	"примечание":                           true,
	"передвинуть нитку":                    true,
	"сведения о повагонном составе поезда": true,
	"остаток на 18:00":                     true,
}

// normLabel приводит подпись к сравнимому виду: нижний регистр, схлопнутые пробелы.
func normLabel(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// isHeaderWord — ячейка является служебной подписью шапки (а не именем терминала).
func isHeaderWord(s string) bool {
	n := normLabel(s)
	if headerWords[n] {
		return true
	}
	return strings.HasPrefix(n, "n п") || strings.HasPrefix(n, "№ п") ||
		strings.HasPrefix(n, "кол. ваг") || strings.HasPrefix(n, "кол.ваг") ||
		strings.HasPrefix(n, "код прич")
}

// isTotalCell — ячейка «Итого»/«ИТОГО»/«Total» (агрегат, не имя).
func isTotalCell(s string) bool {
	u := strings.ToUpper(strings.TrimSpace(s))
	return strings.Contains(u, "ИТОГО") || strings.Contains(u, "TOTAL")
}

// findHeaderRow — строка подписей столбцов: первая из верхних, где есть ячейка
// ровно «План», без учёта регистра (в файле встречается и «факт» строчными —
// подпись набирают руками). Заголовок блока «План на DD-MM-YYYY» не мешает:
// сравнение со всей ячейкой целиком, а не по началу строки.
func findHeaderRow(rows [][]string) int {
	for r := 0; r < min(8, len(rows)); r++ {
		for _, cell := range rows[r] {
			if strings.EqualFold(strings.TrimSpace(cell), "План") {
				return r
			}
		}
	}
	return -1
}

// termCandidates — столбцы строки r правее «Плана», похожие на имена терминалов:
// текст, не «Итого», не служебная подпись шапки.
func termCandidates(row []string, colPlan int) []int {
	var out []int
	for c := colPlan + 1; c < len(row); c++ {
		v := strings.TrimSpace(row[c])
		if v == "" || isTotalCell(v) || isHeaderWord(v) || !hasLetter(v) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// termScore — сколько кандидатов строки r имеют «детей»: непустую ячейку в строке
// ниже внутри своего диапазона (до следующего кандидата). У терминала под ним всегда
// стоят грузы или «ИТОГО»; у случайной подписи шапки под ней пусто.
func termScore(rows [][]string, r, colPlan int) int {
	cand := termCandidates(rows[r], colPlan)
	if r+1 >= len(rows) {
		return 0
	}
	below := rows[r+1]
	score := 0
	for i, c := range cand {
		end := len(rows[r])
		if i+1 < len(cand) {
			end = cand[i+1]
		}
		for c2 := c; c2 < end && c2 < len(below); c2++ {
			if strings.TrimSpace(below[c2]) != "" {
				score++
				break
			}
		}
	}
	return score
}

// findTerminalRow — строка имён терминалов: самая ВЕРХНЯЯ из окна «шапка … шапка+2»,
// где есть имена вне словаря служебных подписей И у них есть подстрока-продолжение
// (грузы или «ИТОГО») в следующей строке. Так строка терминалов отличается от строки
// подписей независимо от числа уровней шапки:
//   - форма Мыса — правее «Плана» стоят «Факт»/«Кол. ваг.»/«Сведения о повагонном
//     составе поезда»/«Комментарий»/«Согл. стан.»: под ними пусто → терминалы ниже;
//   - форма Находки от 29.07 — там сразу имена терминалов с грузами под ними → та же строка.
//
// Для самой строки шапки порог строже (нужно ≥2 таких имени): одна случайная подпись
// в дальнем столбце не должна выдать строку подписей за строку терминалов — на этом
// ловился лист «06» Мыса с подписью «Согл. стан.».
func findTerminalRow(rows [][]string, rowHeader, colPlan int) int {
	for r := rowHeader; r < min(rowHeader+3, len(rows)); r++ {
		need := 1
		if r == rowHeader {
			need = 2
		}
		if termScore(rows, r, colPlan) >= need {
			return r
		}
	}
	return min(rowHeader+1, max(len(rows)-1, 0))
}

// firstLeafCol — самый левый листовой столбец (−1, если листьев нет).
func firstLeafCol(leaves []leafCol) int {
	first := -1
	for _, lf := range leaves {
		if first == -1 || lf.col < first {
			first = lf.col
		}
	}
	return first
}

// findTrainTotalCol — столбец «Итого» всего поезда: итоговая ячейка строки
// терминалов между «Планом» и первым терминалом. Правее — итоги отдельных
// терминалов, их за состав поезда принимать нельзя.
func findTrainTotalCol(rows [][]string, rowTerm, colPlan, colFirstLeaf int) int {
	if rowTerm < 0 || rowTerm >= len(rows) {
		return -1
	}
	limit := len(rows[rowTerm])
	if colFirstLeaf >= 0 && colFirstLeaf < limit {
		limit = colFirstLeaf
	}
	for c := colPlan + 1; c < limit; c++ {
		if isTotalCell(rows[rowTerm][c]) {
			return c
		}
	}
	return -1
}

// dataRows возвращает индексы строк ниже шапки (кандидаты в строки поездов).
func dataRows(rows [][]string, from int) []int {
	out := make([]int, 0, len(rows))
	for r := from + 1; r < len(rows); r++ {
		if len(rows[r]) > 0 {
			out = append(out, r)
		}
	}
	return out
}

// guessIndexCol — столбец «Индекс» по данным: где больше всего валидных индексов
// 4-3-4 и маркеров с.ф. Нужен для листов без подписей столбцов (файл 29.07 пришёл
// без «N п/п» и «Индекс» вовсе).
func guessIndexCol(rows [][]string, rowTerm int) int {
	hits := map[int]int{}
	for _, r := range dataRows(rows, rowTerm) {
		for c, cell := range rows[r] {
			v := strings.TrimSpace(cell)
			if trainIndexRe.MatchString(v) || isSfRow(v) {
				hits[c]++
			}
		}
	}
	best, bestN := -1, 0
	for c, n := range hits {
		if n > bestN || (n == bestN && best != -1 && c < best) {
			best, bestN = c, n
		}
	}
	return best
}

// guessStationCol — «Станция текущей операции» по данным: столбец левее «Плана»,
// где в строках поездов чаще всего стоит чистое имя (буквы без цифр и двоеточий).
// Без него строки с.ф. теряются молча — их станция берётся именно отсюда.
func guessStationCol(rows [][]string, cols gridCols) int {
	hits := map[int]int{}
	for _, r := range dataRows(rows, cols.rowTerm) {
		if !isTrainRow(rows[r], cols.colIndex) {
			continue
		}
		for c := 0; c < min(cols.colPlan, len(rows[r])); c++ {
			if c == cols.colIndex {
				continue
			}
			v := strings.TrimSpace(rows[r][c])
			if v == "" || !hasLetter(v) || strings.ContainsAny(v, "0123456789:") {
				continue
			}
			hits[c]++
		}
	}
	best, bestN := -1, 0
	for c, n := range hits {
		if n > bestN || (n == bestN && best != -1 && c < best) {
			best, bestN = c, n
		}
	}
	return best
}

// guessCommentCol — «Комментарий» по данным: первый столбец правее последнего
// листа, где в строках поездов встречается текст.
func guessCommentCol(rows [][]string, cols gridCols) int {
	last := -1
	for _, lf := range cols.leaves {
		if lf.col > last {
			last = lf.col
		}
	}
	best, bestN := -1, 0
	for _, r := range dataRows(rows, cols.rowTerm) {
		if !isTrainRow(rows[r], cols.colIndex) {
			continue
		}
		for c := last + 1; c < len(rows[r]); c++ {
			if v := strings.TrimSpace(rows[r][c]); v != "" && hasLetter(v) {
				if n := countTextCells(rows, cols, c); n > bestN {
					best, bestN = c, n
				}
				break
			}
		}
	}
	return best
}

// countTextCells — сколько строк поездов несут текст в столбце col.
func countTextCells(rows [][]string, cols gridCols, col int) int {
	n := 0
	for _, r := range dataRows(rows, cols.rowTerm) {
		if !isTrainRow(rows[r], cols.colIndex) || col >= len(rows[r]) {
			continue
		}
		if v := strings.TrimSpace(rows[r][col]); v != "" && hasLetter(v) {
			n++
		}
	}
	return n
}

// isTrainRow — строка несёт нитку поезда (валидный индекс или маркер с.ф.).
func isTrainRow(row []string, colIndex int) bool {
	if colIndex < 0 || colIndex >= len(row) {
		return false
	}
	v := strings.TrimSpace(row[colIndex])
	return trainIndexRe.MatchString(v) || isSfRow(v)
}

// findLeaves определяет ВСЕ листовые (не агрегатные) подстолбцы терминалов с их
// метками (терминал + груз) и признаком «наш» (имя терминала ∈ profile.OurTerminals).
// «Наши» листья суммируются в Activ; полный список идёт в Ports нитки (столбцы
// портов таблицы) и позволяет фронту фильтровать «чужие» строки.
//
// Терминалы задаёт строка rowTerm (найденная findTerminalRow, НЕ «шапка+1»); их
// подзаголовки-грузы — строки rowTerm+1..rowTerm+3.
// «Листовой» столбец — самый глубокий непустой-не-ИТОГО подзаголовок без детей на
// следующем уровне. Алгоритм классификации листьев дословно повторяет эталон GTport.
func (g *GridParser) findLeaves(rows [][]string, rowTerm int) []leafCol {
	if rowTerm < 0 || rowTerm >= len(rows) {
		return nil
	}
	// Подзаголовочные строки для анализа листьев.
	var subRows [][]string
	for off := 1; off <= 3; off++ {
		if rowTerm+off < len(rows) {
			subRows = append(subRows, rows[rowTerm+off])
		}
	}

	isTotal := isTotalCell

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
	for c, cell := range rows[rowTerm] {
		cell = strings.TrimSpace(cell)
		// Пусто, «Итого» всего поезда, служебная подпись столбца — не терминал.
		// Подписи важны, когда строка терминалов СОВПАДАЕТ со строкой шапки
		// (форма Находки 29.07): иначе «Индекс»/«План»/«факт» станут терминалами.
		if cell == "" || isTotal(cell) || isHeaderWord(cell) || !hasLetter(cell) {
			continue
		}
		terminals = append(terminals, terminal{start: c, name: cell})
	}

	// Правая граница поиска листьев у ПОСЛЕДНЕГО терминала — реальная ширина
	// заголовочных строк, а не 1<<30: столбца-листа за пределами заголовков быть не
	// может (getLeafName пуст → не лист), а холостой добег до ~1e9 давал ~6 с на разбор.
	width := len(rows[rowTerm])
	for _, sr := range subRows {
		if len(sr) > width {
			width = len(sr)
		}
	}

	var leaves []leafCol
	for tIdx, term := range terminals {
		isOur := g.prof.isOurTerminal(term.name)
		termEnd := width
		if tIdx+1 < len(terminals) {
			termEnd = terminals[tIdx+1].start
		}
		before := len(leaves)
		// С term.start (не +1): в «новой форме» название терминала стоит прямо над
		// его первым грузом (НМТП/col → «Каменный уголь»/тот же col), и старт-столбец
		// сам является листом. isLeafCol отсеет старт-столбец, если это агрегат/«ИТОГО»
		// (старый формат с per-терминальным итогом), — тогда фолбэк ниже не сработает,
		// т.к. настоящие листья найдутся правее. Совместимо с обоими форматами.
		for c := term.start; c < termEnd; c++ {
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
				ports, activ := buildPorts(rows[r], cols)
				nitki = append(nitki, PlanNitka{
					IndexPp:   ostatokMarker,
					Wagons:    atoiSafe(getCell(rows[r], cols.colKolVag)),
					Activ:     activ,
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

		// Нитка распознаётся ПО СОДЕРЖИМОМУ столбца «Индекс», номер п/п не требуем:
		// в свежих блоках месячной книги он не проставлен (прод-фикс gtport).
		//  - маркер «с.ф.» → нитка сборного формирования;
		//  - валидный индекс 4-3-4 → нитка поезда (в т.ч. без времени: «не подводить»/
		//    пусто в «Плане» — нитка отображается, но планового времени не имеет);
		//  - иначе — свободная нитка/служебная строка, пропускаем.
		if blockDate.IsZero() {
			continue // нет даты блока — нитку не к чему привязать
		}
		if isSfRow(label) {
			if n, ok := g.buildSfNitka(rows[r], cols, blockDate); ok {
				nitki = append(nitki, n)
				sfEmitted++
			} else {
				sfSkipped++
			}
			continue
		}
		if !trainIndexRe.MatchString(label) {
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

	// Гард «разбор пустой». Лист верстают руками, раскладка плывёт; при сдвиге
	// столбцов состава нитки находятся, а вагоны у всех нулевые — и такой план
	// молча ложился в базу (случай Находки 29.07: терминалами стали имена грузов).
	// Падать здесь громко дешевле, чем чинить последствия у диспетчера.
	wagons, activ := 0, 0
	for _, n := range nitki {
		wagons += n.Wagons
		activ += n.Activ
	}
	if wagons == 0 {
		return nil, fmt.Errorf("plan[%s]: столбцы состава поезда не распознаны — у всех %d ниток 0 вагонов; похоже, изменилась форма листа", g.prof.PlanCode, trains)
	}
	if activ == 0 {
		fmt.Printf("[plan:%s] ⚠ по «нашим» терминалам (%s) 0 вагонов при %d в плане — проверьте профиль станции и имена терминалов в файле\n",
			g.prof.PlanCode, strings.Join(g.prof.OurTerminals, ", "), wagons)
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

	planCell := get(cols.colPlan)
	planJd := combineDateTime(blockDate, planCell) // ЖД-время, без сдвига
	planMsk := applyMskRule(planJd)
	factMsk := applyMskRule(combineDateTime(blockDate, get(cols.colFact)))
	// «План» не время («не подводить», пусто, прочее) → нитка без планового
	// времени; сырой текст сохраняем для отображения в колонке «План».
	planRaw := ""
	if planJd.IsZero() {
		planRaw = planCell
	}

	ports, activ := buildPorts(row, cols)

	index := get(cols.colIndex)
	return PlanNitka{
		Index:   index,
		IndexPp: index, // нормализация с.ф. — позже; для обычного поезда = Index
		PlanJd:  planJd,
		PlanMsk: planMsk,
		FactMsk: factMsk,
		Otkl:    formatOtkl(planMsk, factMsk),
		PlanRaw: planRaw,
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

// trainIndexRe — валидный индекс поезда «АААА-БББ-ВВВВ» (признак строки-нитки;
// номер п/п не используется — в свежих блоках месячной книги он не проставлен).
var trainIndexRe = regexp.MustCompile(`^\d{4}-\d{3}-\d{4}$`)

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

// colName — буквенное имя столбца Excel (A, B, …, AA) для диагностики; «—» для −1.
func colName(col int) string {
	if col < 0 {
		return "—"
	}
	name := ""
	for c := col; ; c = c/26 - 1 {
		name = string(rune('A'+c%26)) + name
		if c < 26 {
			break
		}
	}
	return name
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
