// Package report — печатные формы, которые собирает сам бэкенд (Excel).
//
// Первая такая форма — бланк ГУ-45 из сырой памятки провайдера: остальные
// выгрузки в проекте делает фронт (`xlsx-js-style`), но здесь исходник —
// документ, который до фронта не доезжает, поэтому лист собирается на сервере.
package report

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Типографские константы бланка — они напечатаны на форме, а не приходят из
// данных: номер бланка и год утверждения одинаковы на всех памятках.
const (
	formBlankCode = "0363809"
	formApproved  = `Утверждена ОАО "РЖД" в 2004 г.`
	formTitleLine = "Форма ГУ-45 ВЦ"
)

// Геометрия листа: строки шапки бланка фиксированы, таблица начинается сразу
// за ними. Держим номерами, чтобы не считать смещения по всему файлу.
const (
	rowStation  = 3  // «Станция <имя> <код>»
	rowRailway  = 4  // «<дорога> ж.д. - филиал ОАО «РЖД»»
	rowTitle    = 6  // «ПАМЯТКА ПРИЕМОСДАТЧИКА № N на подачу/уборку вагонов»
	rowOwner    = 8  // владелец п/п + место подачи
	rowLoco     = 10 // локомотив + индекс поезда
	rowHead     = 12 // первая строка шапки таблицы (шапка занимает 3 строки)
	rowHeadNums = 15 // строка нумерации колонок 1…12
	rowFirst    = 16 // первая строка вагона
)

// PamyatkaGU45XLSX собирает из разобранной памятки лист Excel по образцу
// печатного бланка ГУ-45: та же шапка, те же 12 колонок в том же порядке,
// тот же подвал с местом для отметок.
//
// Колонки 9–11 (задержка, № акта ГУ-23, кол-во взвешиваний) в источнике не
// приходят — остаются пустыми, как в бланке, когда их не заполняли. Индекс
// поезда провайдер тоже не отдаёт: строка есть, значение пустое.
//
// Сведения об ЭЦП внизу бланка воспроизводятся по тем данным, что у нас есть
// (составитель, подписанты, время составления и регистрации); сертификаты
// лежат в `Docsigntab`, который мы намеренно не разбираем.
func PamyatkaGU45XLSX(doc domain.PamyatkaDoc) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	sheet := sheetName(doc.Number)
	if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
		return nil, fmt.Errorf("ГУ-45 №%s: имя листа: %w", doc.Number, err)
	}

	st, err := newGU45Styles(f)
	if err != nil {
		return nil, fmt.Errorf("ГУ-45 №%s: стили: %w", doc.Number, err)
	}
	if err := layoutSheet(f, sheet); err != nil {
		return nil, fmt.Errorf("ГУ-45 №%s: разметка листа: %w", doc.Number, err)
	}
	if err := writeHeader(f, sheet, st, doc); err != nil {
		return nil, fmt.Errorf("ГУ-45 №%s: шапка: %w", doc.Number, err)
	}
	if err := writeTableHead(f, sheet, st); err != nil {
		return nil, fmt.Errorf("ГУ-45 №%s: шапка таблицы: %w", doc.Number, err)
	}
	lastRow, err := writeVagons(f, sheet, st, doc.Vagons)
	if err != nil {
		return nil, fmt.Errorf("ГУ-45 №%s: строки вагонов: %w", doc.Number, err)
	}
	if err := writeFooter(f, sheet, st, doc, lastRow+2); err != nil {
		return nil, fmt.Errorf("ГУ-45 №%s: подвал: %w", doc.Number, err)
	}
	if err := printSetup(f, sheet); err != nil {
		return nil, fmt.Errorf("ГУ-45 №%s: параметры печати: %w", doc.Number, err)
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("ГУ-45 №%s: запись книги: %w", doc.Number, err)
	}
	return buf.Bytes(), nil
}

// FileName — имя файла выгрузки: латиницей, как называют памятки в переписке
// (`gu45_11357.xlsx`), с клиентом провайдера — номер уникален лишь в его пределах.
func FileName(client, number string) string {
	return fmt.Sprintf("gu45_%s_%s.xlsx", client, number)
}

// sheetName — «ГУ-45 №11357»; Excel не пускает имя листа длиннее 31 символа.
func sheetName(number string) string {
	name := []rune("ГУ-45 №" + number)
	if len(name) > 31 {
		name = name[:31]
	}
	return string(name)
}

// --- стили ---

type gu45Styles struct {
	title    int // заголовок памятки
	formName int // «Форма ГУ-45 ВЦ» справа сверху
	formCode int // номер бланка в рамке
	approved int // «Утверждена ОАО "РЖД"…»
	label    int // подпись поля («Станция», «Место подачи»)
	field    int // значение поля — с подчёркиванием, как линия в бланке
	head     int // ячейка шапки таблицы
	headNum  int // строка нумерации колонок
	cell     int // ячейка данных по центру
	cellLeft int // ячейка данных с выключкой влево (вагон/груз)
	note     int // мелкий текст подвала (сведения об ЭП)
	noteHead int // заголовок блока ЭП
}

func newGU45Styles(f *excelize.File) (gu45Styles, error) {
	const font = "Arial"
	thin := []excelize.Border{
		{Type: "left", Color: "000000", Style: 1},
		{Type: "right", Color: "000000", Style: 1},
		{Type: "top", Color: "000000", Style: 1},
		{Type: "bottom", Color: "000000", Style: 1},
	}

	var st gu45Styles
	var err error
	mk := func(s *excelize.Style) int {
		if err != nil {
			return 0
		}
		var id int
		id, err = f.NewStyle(s)
		return id
	}

	st.title = mk(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 12, Bold: true},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	st.formName = mk(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 10, Bold: true},
		Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"},
	})
	st.formCode = mk(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 10},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    thin,
	})
	st.approved = mk(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 9},
		Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"},
	})
	st.label = mk(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 9},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "bottom"},
	})
	st.field = mk(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 9},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "bottom", WrapText: true},
		Border:    []excelize.Border{{Type: "bottom", Color: "000000", Style: 1}},
	})
	st.head = mk(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 8},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    thin,
	})
	st.headNum = mk(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 8},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    thin,
	})
	st.cell = mk(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 9},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "top", WrapText: true},
		Border:    thin,
	})
	st.cellLeft = mk(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 9},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "top", WrapText: true},
		Border:    thin,
	})
	st.note = mk(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 8, Color: "1F4E9C"},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "top", WrapText: true},
	})
	st.noteHead = mk(&excelize.Style{
		Font:      &excelize.Font{Family: font, Size: 8, Color: "1F4E9C", Bold: true},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err != nil {
		return gu45Styles{}, err
	}
	return st, nil
}

// --- разметка ---

// colWidths — ширины 12 колонок бланка в единицах Excel. Подобраны так, чтобы
// лист влезал в ширину A4 книжной ориентации, а наименование груза и примечание
// переносились в 2–3 строки, как на печатной форме.
var colWidths = []struct {
	col   string
	width float64
}{
	{"A", 5}, {"B", 26}, {"C", 6.5}, {"D", 8}, {"E", 7},
	{"F", 10}, {"G", 12}, {"H", 10}, {"I", 8}, {"J", 9}, {"K", 6.5}, {"L", 20},
}

func layoutSheet(f *excelize.File, sheet string) error {
	for _, c := range colWidths {
		if err := f.SetColWidth(sheet, c.col, c.col, c.width); err != nil {
			return err
		}
	}
	return nil
}

// --- шапка бланка ---

func writeHeader(f *excelize.File, sheet string, st gu45Styles, doc domain.PamyatkaDoc) error {
	w := newWriter(f, sheet)

	w.merged("I1", "K1", formTitleLine, st.formName)
	w.set("L1", formBlankCode, st.formCode)
	w.merged("H2", "L2", formApproved, st.approved)

	w.set("A"+itoa(rowStation), "Станция", st.label)
	w.merged("B"+itoa(rowStation), "F"+itoa(rowStation), joinNonEmpty(" ", doc.StationName, doc.StationCode), st.field)

	w.merged("A"+itoa(rowRailway), "B"+itoa(rowRailway), doc.RailwayName, st.field)
	w.merged("C"+itoa(rowRailway), "G"+itoa(rowRailway), "ж.д. - филиал ОАО «РЖД»", st.label)

	w.merged("A"+itoa(rowTitle), "L"+itoa(rowTitle),
		fmt.Sprintf("ПАМЯТКА ПРИЕМОСДАТЧИКА № %s на %s вагонов", doc.Number, operTypeWord(doc.OperType)), st.title)
	if err := f.SetRowHeight(sheet, rowTitle, 22); err != nil {
		return err
	}

	w.merged("A"+itoa(rowOwner), "B"+itoa(rowOwner), "Наименование владельца п/п (клиента)", st.label)
	w.merged("C"+itoa(rowOwner), "G"+itoa(rowOwner), doc.PathOwner.Name, st.field)
	w.set("H"+itoa(rowOwner), "Место подачи", st.label)
	w.merged("I"+itoa(rowOwner), "L"+itoa(rowOwner), doc.GetPlace, st.field)
	if err := f.SetRowHeight(sheet, rowOwner, 28); err != nil {
		return err
	}

	w.merged("A"+itoa(rowLoco), "B"+itoa(rowLoco), "подача производилась локомотивом", st.label)
	w.merged("C"+itoa(rowLoco), "G"+itoa(rowLoco), doc.GetBy, st.field)
	w.set("H"+itoa(rowLoco), "Индекс поезда", st.label)
	// Индекс поезда провайдер не отдаёт — оставляем линию под ручное заполнение.
	w.merged("I"+itoa(rowLoco), "L"+itoa(rowLoco), "", st.field)

	return w.err
}

// operTypeWord — «подачу»/«уборку» как в заголовке бланка. Источник шлёт слово
// уже в нужной форме, но в Metadatatab оно встречается с заглавной («Подачу»).
func operTypeWord(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return "подачу/уборку"
	}
	return strings.ToLower(t)
}

// --- шапка таблицы ---

// headCell — ячейка шапки: диапазон объединения и текст.
type headCell struct {
	from, to string
	text     string
}

func writeTableHead(f *excelize.File, sheet string, st gu45Styles) error {
	r1, r2, r3 := itoa(rowHead), itoa(rowHead+1), itoa(rowHead+2)

	cells := []headCell{
		{"A" + r1, "A" + r3, "№\nп/п"},
		// Графа 2 в бланке разделена линией — в Excel это два соседних
		// объединения, граница между ними и есть та самая линия.
		{"B" + r1, "B" + r2, "№ вагона"},
		{"B" + r3, "B" + r3, "Наименование груза"},
		{"C" + r1, "C" + r3, "Код.\nж.д.\nадм."},
		{"D" + r1, "D" + r3, "При-\nнадл.\nваго-\nна"},
		{"E" + r1, "E" + r3, "Груз.\nопер."},
		{"F" + r1, "H" + r1, "Время выполнения операции\nдень-месяц\nчасы-минуты"},
		{"F" + r2, "F" + r3, "подача/\nпередача\nна выстав.\nпуть"},
		{"G" + r2, "G" + r3, "уведомл.\nо заверш.\nгр. опер./\nвозврат на\nвыст. путь"},
		{"H" + r2, "H" + r3, "уборка"},
		{"I" + r1, "J" + r1, "Задержка окончания\nгруз. операции"},
		{"I" + r2, "I" + r3, "время\nчас-\nмин"},
		{"J" + r2, "J" + r3, "№ акта\nГУ-23"},
		{"K" + r1, "K" + r3, "Кол-\nво.\nвзв."},
		{"L" + r1, "L" + r3, "Примечание"},
	}

	w := newWriter(f, sheet)
	for _, c := range cells {
		w.merged(c.from, c.to, c.text, st.head)
	}
	if w.err != nil {
		return w.err
	}

	// Строка нумерации колонок — по ней в бланке ссылаются на графы.
	for i := 1; i <= 12; i++ {
		cell, err := excelize.CoordinatesToCellName(i, rowHeadNums)
		if err != nil {
			return err
		}
		if err := f.SetCellValue(sheet, cell, i); err != nil {
			return err
		}
	}
	if err := f.SetCellStyle(sheet, "A"+itoa(rowHeadNums), "L"+itoa(rowHeadNums), st.headNum); err != nil {
		return err
	}

	for _, r := range []struct {
		row    int
		height float64
	}{{rowHead, 30}, {rowHead + 1, 18}, {rowHead + 2, 34}, {rowHeadNums, 13}} {
		if err := f.SetRowHeight(sheet, r.row, r.height); err != nil {
			return err
		}
	}
	return nil
}

// --- строки вагонов ---

// writeVagons пишет по строке на вагон и возвращает номер последней строки
// таблицы (или строки нумерации колонок, если вагонов не пришло вовсе).
func writeVagons(f *excelize.File, sheet string, st gu45Styles, vagons []domain.PamyatkaDocVagon) (int, error) {
	row := rowHeadNums
	for i := range vagons {
		v := &vagons[i]
		row = rowFirst + i

		values := []struct {
			col   string
			value string
			style int
		}{
			{"A", v.Order, st.cell},
			{"B", joinNonEmpty("\n", v.Vagon, v.CargoName), st.cellLeft},
			{"C", v.AdmCode, st.cell},
			{"D", v.OwnerCode, st.cell},
			{"E", v.GrOperationType, st.cell},
			{"F", stampCell(v.GetIn), st.cell},
			{"G", stampCell(v.Report), st.cell},
			{"H", stampCell(v.GetOut), st.cell},
			{"I", "", st.cell}, // задержка: в источнике нет
			{"J", "", st.cell}, // № акта ГУ-23: в источнике нет
			{"K", "", st.cell}, // кол-во взвешиваний: в источнике нет
			{"L", v.NumberMemo, st.cell},
		}
		for _, c := range values {
			cell := c.col + itoa(row)
			if err := f.SetCellValue(sheet, cell, c.value); err != nil {
				return 0, err
			}
			if err := f.SetCellStyle(sheet, cell, cell, c.style); err != nil {
				return 0, err
			}
		}
		if err := f.SetRowHeight(sheet, row, rowHeightFor(v)); err != nil {
			return 0, err
		}
	}
	return row, nil
}

// rowHeightFor — высота строки под самый длинный перенос. Явная высота нужна
// потому, что автоподбор по объединённым и переносимым ячейкам делают не все
// программы, открывающие xlsx.
func rowHeightFor(v *domain.PamyatkaDocVagon) float64 {
	const lineHeight = 11.5
	lines := 1 + wrapLines(v.CargoName, colWidthOf("B")) // номер вагона + наименование груза
	if n := wrapLines(v.NumberMemo, colWidthOf("L")); n > lines {
		lines = n
	}
	if lines < 2 {
		lines = 2 // время занимает две строки (дата и часы-минуты) всегда
	}
	return float64(lines)*lineHeight + 3
}

// wrapLines — сколько строк займёт текст в колонке заданной ширины. Оценка
// грубая (моноширинная), для высоты строки этого достаточно.
func wrapLines(text string, width float64) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	perLine := int(width) - 1
	if perLine < 1 {
		perLine = 1
	}
	lines := (len([]rune(text)) + perLine - 1) / perLine
	if lines < 1 {
		lines = 1
	}
	return lines
}

func colWidthOf(col string) float64 {
	for _, c := range colWidths {
		if c.col == col {
			return c.width
		}
	}
	return 10
}

// stampCell — время в ячейке бланка: «27.07» и «15:35» двумя строками. Пустое
// время — пустая ячейка (у памятки на подачу уборки ещё нет).
func stampCell(t *domain.LocalTime) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Time().Format("02.01") + "\n" + t.Time().Format("15:04")
}

// --- подвал ---

func writeFooter(f *excelize.File, sheet string, st gu45Styles, doc domain.PamyatkaDoc, row int) error {
	w := newWriter(f, sheet)

	w.set("A"+itoa(row), "Место для отметок", st.label)
	row += 2

	w.set("A"+itoa(row), "Вагон принял", st.label)
	w.merged("B"+itoa(row), "E"+itoa(row), "", st.field)
	w.set("G"+itoa(row), "Вагон сдал", st.label)
	w.merged("H"+itoa(row), "L"+itoa(row), "", st.field)
	row += 2

	// Подпись приемосдатчика РЖД стоит с той стороны, с какой он выступает:
	// на подаче вагоны сдаёт, на уборке — принимает (так и в обоих бланках).
	sdal, prinyal := "", ""
	if signedBy := signature(doc); signedBy != "" {
		if strings.Contains(operTypeWord(doc.OperType), "убор") {
			prinyal = signedBy
		} else {
			sdal = signedBy
		}
	}
	w.merged("A"+itoa(row), "B"+itoa(row), "Сдал приемосдатчик ж.д.", st.label)
	w.merged("C"+itoa(row), "E"+itoa(row), sdal, st.field)
	w.merged("G"+itoa(row), "H"+itoa(row), "Принял приемосдатчик ж.д.", st.label)
	w.merged("I"+itoa(row), "L"+itoa(row), prinyal, st.field)
	if err := f.SetRowHeight(sheet, row, 24); err != nil {
		return err
	}
	row += 2

	w.merged("A"+itoa(row), "D"+itoa(row), "Памятка проведена по ведомости подачи и уборки №", st.label)
	w.merged("E"+itoa(row), "H"+itoa(row), "", st.field)
	row += 2

	w.merged("A"+itoa(row), "C"+itoa(row), "Товарный кассир (агент станции)", st.label)
	w.merged("D"+itoa(row), "G"+itoa(row), "", st.field)
	row += 2

	if err := w.err; err != nil {
		return err
	}
	return writeSignBlock(f, sheet, st, doc, row)
}

// signature — кто подписал документ: подписанты приходят строкой источника
// «/Фамилия И. О.-дд.мм.гггг-чч:мм:сс», в бланке печатается только имя.
func signature(doc domain.PamyatkaDoc) string {
	s := strings.TrimSpace(doc.Signatories)
	if s == "" {
		return ""
	}
	names := make([]string, 0, 2)
	for _, part := range strings.Split(s, "/") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i := strings.Index(part, "-"); i > 0 {
			part = strings.TrimSpace(part[:i])
		}
		if part != "" {
			names = append(names, part)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "Подписано ЭП " + strings.Join(names, ", ")
}

// writeSignBlock — нижний блок бланка «Сведения об электронных подписях».
// Сертификатов у нас нет (ЭЦП в Docsigntab не разбираем), поэтому печатаем то,
// что источник действительно прислал: составителя, подписантов и времена.
func writeSignBlock(f *excelize.File, sheet string, st gu45Styles, doc domain.PamyatkaDoc, row int) error {
	w := newWriter(f, sheet)

	w.merged("A"+itoa(row), "L"+itoa(row), "Сведения о документе и электронных подписях", st.noteHead)
	row++

	lines := []string{
		noteLine("Документ составил", doc.Creator),
		noteLine("Подписи", doc.Signatories),
		noteLine("Составлен", stampFull(doc.ComposedAt)),
		noteLine("Зарегистрирован", stampFull(doc.DocDate)),
		noteLine("Состояние", doc.DocState),
		noteLine("Клиент провайдера", doc.Client),
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		w.merged("A"+itoa(row), "L"+itoa(row), line, st.note)
		row++
	}
	return w.err
}

// noteLine — строка блока сведений; пустой реквизит строки не даёт вовсе.
func noteLine(label, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return label + ": " + strings.TrimSpace(value)
}

func stampFull(t *domain.LocalTime) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Time().Format("02.01.2006 15:04:05")
}

// --- параметры печати ---

func printSetup(f *excelize.File, sheet string) error {
	enable := true
	size, fitWidth, fitHeight := 9, 1, 0 // 9 — A4
	orientation := "portrait"

	if err := f.SetSheetProps(sheet, &excelize.SheetPropsOptions{FitToPage: &enable}); err != nil {
		return err
	}
	if err := f.SetPageLayout(sheet, &excelize.PageLayoutOptions{
		Size:        &size,
		Orientation: &orientation,
		FitToWidth:  &fitWidth,
		FitToHeight: &fitHeight,
	}); err != nil {
		return err
	}
	if err := f.SetPageMargins(sheet, &excelize.PageLayoutMarginsOptions{
		Left: ptr(0.4), Right: ptr(0.4), Top: ptr(0.4), Bottom: ptr(0.4),
	}); err != nil {
		return err
	}
	// Шапка таблицы повторяется на каждой печатной странице — в бланке на
	// несколько листов вагоны идут под теми же графами.
	return f.SetDefinedName(&excelize.DefinedName{
		Name:     "_xlnm.Print_Titles",
		RefersTo: fmt.Sprintf("'%s'!$%d:$%d", sheet, rowHead, rowHeadNums),
		Scope:    sheet,
	})
}

func ptr[T any](v T) *T { return &v }

// --- мелкие помощники ---

// writer копит первую ошибку записи: ячеек много, проверять каждую по месту
// значит утопить разметку бланка в обработке ошибок.
type writer struct {
	f     *excelize.File
	sheet string
	err   error
}

func newWriter(f *excelize.File, sheet string) *writer { return &writer{f: f, sheet: sheet} }

func (w *writer) set(cell, value string, style int) {
	if w.err != nil {
		return
	}
	if w.err = w.f.SetCellValue(w.sheet, cell, value); w.err != nil {
		return
	}
	w.err = w.f.SetCellStyle(w.sheet, cell, cell, style)
}

func (w *writer) merged(from, to, value string, style int) {
	if w.err != nil {
		return
	}
	if w.err = w.f.MergeCell(w.sheet, from, to); w.err != nil {
		return
	}
	if w.err = w.f.SetCellValue(w.sheet, from, value); w.err != nil {
		return
	}
	w.err = w.f.SetCellStyle(w.sheet, from, to, style)
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// joinNonEmpty склеивает непустые части: пустой реквизит не должен оставлять
// в бланке висящий разделитель.
func joinNonEmpty(sep string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
