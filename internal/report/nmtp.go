package report

// «Подход вагонов» по форме порта (НМТП) — книга .xlsx из domain.NmtpReport.
// Раскладка повторяет файл порта («ПОДХОД ВАГОНОВ ГУТ-2/УТ-1»): шапка в три
// уровня (клиент / станции погрузки / марка), слева реквизиты поезда, секции
// строк по станциям терминалов и дорогам (восток → запад) с итогами, ниже —
// секция «БРОШЕННЫЕ ПОЕЗДА» (в колонке прибытия — дата бросания, как в файле),
// низ — как на экране (решение владельца 31.07.2026): счётчики «Итого …
// в ходу/брошенных/на сети», блок «Итог по грузовым колонкам» с повторением
// шапки, затем «Прогноз выгрузки по подходу», «Нагрузка на ж/д сеть», свод
// тоннажа по клиентам.
//
// Оформление: белая жирная шапка и серая полоса марок — по файлу порта; секции
// дорог зелёные (A9D08E) и брошенных голубые (CFF0FC) — цвета экрана gtport
// (решение владельца 31.07.2026, в файле порта были серые/голубые ABE9FF);
// жёлтая колонка «итого» (FFFFCC), утолщённые границы на стыках групп клиентов,
// шапка закреплена. Ручную раскраску отдельных клиентов из файла не переносим.
//
// Отличия от файла порта (осознанные): колонка «СУДНО» называется «ПРИМЕЧАНИЕ»
// (в файле в ней жили пометки перестановок и смен индекса — они и выводятся:
// «был NNN» + «НА {терминал}» / «С {терминал}»); колонки «ПРИОРИТЕТ» нет (ручное поле порта — решение
// владельца 29.07.2026: правки убираем); служебные формульные колонки порта
// (п/н/м/у, «Столбец1…») не переносятся.

import (
	"fmt"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// vladTime — владивостокское представление московского времени (+7) ТОЛЬКО для
// этой печатной формы: порт живёт в местном времени, и в его файле «ожид. время
// приб.» — местное (решение владельца 29.07.2026). Единственное место сдвига во
// всём проекте; данные и все остальные экраны — Московские naive (TARGET §3.11).
func vladTime(t *domain.LocalTime) time.Time {
	return t.Time().Add(7 * time.Hour)
}

// NmtpXLSX собирает книгу «Подход вагонов для {терминал}» по форме порта.
func NmtpXLSX(r domain.NmtpReport) ([]byte, string, error) {
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "Подход"
	if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
		return nil, "", fmt.Errorf("нмтп: имя листа: %w", err)
	}

	// ── Стили (веса — из файла порта; цвета секций — экранные, владелец 31.07) ──
	const (
		fillGray   = "E4E4E4" // марки в шапке
		fillGreen  = "A9D08E" // секции дорог в ходу (цвет экрана gtport)
		fillBlue   = "CFF0FC" // секции брошенных (цвет экрана gtport)
		fillYellow = "FFFFCC" // колонка «итого»
		fillOrange = "FFE7BA" // «прочее» — несматченный груз, громко
	)
	thin := []excelize.Border{
		{Type: "left", Style: 1, Color: "000000"}, {Type: "right", Style: 1, Color: "000000"},
		{Type: "top", Style: 1, Color: "000000"}, {Type: "bottom", Style: 1, Color: "000000"},
	}
	// Утолщённая левая граница — стык групп клиентов в матрице груза.
	thinGL := []excelize.Border{
		{Type: "left", Style: 2, Color: "000000"}, {Type: "right", Style: 1, Color: "000000"},
		{Type: "top", Style: 1, Color: "000000"}, {Type: "bottom", Style: 1, Color: "000000"},
	}
	// Секционная полоса: утолщённые верх/низ, как у строк дорог в файле порта.
	sectionBd := []excelize.Border{
		{Type: "left", Style: 1, Color: "000000"}, {Type: "right", Style: 1, Color: "000000"},
		{Type: "top", Style: 2, Color: "000000"}, {Type: "bottom", Style: 2, Color: "000000"},
	}
	center := &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}
	// Вертикальный текст (снизу вверх) — подписи станций/марок компактного режима.
	centerV := &excelize.Alignment{Horizontal: "center", Vertical: "center", TextRotation: 90}
	solid := func(color string) excelize.Fill {
		return excelize.Fill{Type: "pattern", Color: []string{color}, Pattern: 1}
	}

	styles := map[string]int{}
	for _, s := range []struct {
		name string
		st   *excelize.Style
	}{
		{"title", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}, Alignment: center}},
		{"head", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 10}, Border: thin, Alignment: center}},
		{"headG", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 10}, Border: thinGL, Alignment: center}},
		{"headMark", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 9}, Border: thin,
			Fill: solid(fillGray), Alignment: center}},
		{"headMarkG", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 9}, Border: thinGL,
			Fill: solid(fillGray), Alignment: center}},
		{"headMarkY", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 9}, Border: thin,
			Fill: solid(fillYellow), Alignment: center}},
		{"headMarkGY", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 9}, Border: thinGL,
			Fill: solid(fillYellow), Alignment: center}},
		{"headV", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 10}, Border: thin, Alignment: centerV}},
		{"headGV", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 10}, Border: thinGL, Alignment: centerV}},
		{"headMarkV", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 9}, Border: thin,
			Fill: solid(fillGray), Alignment: centerV}},
		{"headMarkGV", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 9}, Border: thinGL,
			Fill: solid(fillGray), Alignment: centerV}},
		{"headMarkYV", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 9}, Border: thin,
			Fill: solid(fillYellow), Alignment: centerV}},
		{"headMarkGYV", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 9}, Border: thinGL,
			Fill: solid(fillYellow), Alignment: centerV}},
		{"headTotal", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 11}, Border: thinGL,
			Fill: solid(fillYellow), Alignment: center}},
		{"headOther", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 10}, Border: thinGL,
			Fill: solid(fillOrange), Alignment: center}},
		{"cellC", &excelize.Style{Font: &excelize.Font{Size: 11}, Border: thin,
			Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"}}},
		{"num", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 12}, Border: thin,
			Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"}}},
		{"numG", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 12}, Border: thinGL,
			Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"}}},
		{"numOther", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 12}, Border: thinGL,
			Fill: solid(fillOrange), Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"}}},
		{"totalCol", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 12}, Border: thinGL,
			Fill: solid(fillYellow), Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"}}},
		{"section", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 11}, Border: sectionBd,
			Fill: solid(fillGreen), Alignment: center}},
		{"sectionAb", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 11}, Border: sectionBd,
			Fill: solid(fillBlue), Alignment: center}},
		{"banner", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 12}, Alignment: center}},
		{"counterLbl", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 11},
			Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"}}},
		{"counterVal", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 12}, Border: thin,
			Fill: solid(fillYellow), Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"}}},
		{"footLbl", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 11}, Border: thin,
			Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"}}},
		{"plain", &excelize.Style{Font: &excelize.Font{Size: 11},
			Alignment: &excelize.Alignment{Vertical: "center"}}},
		{"plainB", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 11},
			Alignment: &excelize.Alignment{Vertical: "center"}}},
		{"plainC", &excelize.Style{Font: &excelize.Font{Size: 11},
			Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"}}},
	} {
		id, err := f.NewStyle(s.st)
		if err != nil {
			return nil, "", fmt.Errorf("нмтп: стиль %s: %w", s.name, err)
		}
		styles[s.name] = id
	}

	// ── Геометрия ────────────────────────────────────────────────────────────
	// Слева 7 фиксированных колонок, дальше матрица груза, последняя — «итого».
	fixed := []struct {
		Header string
		Width  float64
	}{
		{"ПОЕЗД", 20}, {"СТАНЦИЯ", 22}, {"ДАТА\n(принято к перевозке)", 12},
		{"ПРИМЕЧАНИЕ", 14}, {"ВАГОН\n(для контроля)", 13}, {"ожид. дата приб.", 12}, {"ожид. время приб. (влад.)", 11},
	}
	nFixed := len(fixed)
	cargoCols := len(r.Columns)
	hasOther := r.HasOther
	if hasOther {
		cargoCols++
	}
	lastCol := nFixed + cargoCols + 1 // +«итого»

	// Стык групп клиентов: у первой колонки группы — утолщённая левая граница.
	groupStart := make([]bool, cargoCols)
	for c := range groupStart {
		switch {
		case c == 0:
			groupStart[c] = true
		case hasOther && c == cargoCols-1:
			groupStart[c] = true
		default:
			groupStart[c] = r.Columns[c].Group != r.Columns[c-1].Group
		}
	}

	for i, c := range fixed {
		name, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(sheet, name, name, c.Width); err != nil {
			return nil, "", fmt.Errorf("нмтп: ширина: %w", err)
		}
	}
	// Компактный режим (владелец, 31.07.2026): колонок больше 10 — подписи
	// станций и марок вертикально, колонки узкие. У многоколоночных раскладок
	// (УТ-1/ГУТ-2: 25–30) подписи короткие и вертикально занимают немного,
	// у коротких (АЭ: 6) подписи-списки длинные и остаются горизонтальными.
	// Порог продублирован на экране (nmtp-modal.component.ts, compact).
	compact := cargoCols > 10
	// Ширина колонок матрицы: в файле порта 7.5, расширена до 10 — длинные
	// списки станций АЭ иначе заворачивались на десяток строк; в компактном
	// режиме 4 — два знака цифр плюс отступы.
	cargoColW := 10.0
	if compact {
		cargoColW = 4
	}
	firstCargo, _ := excelize.ColumnNumberToName(nFixed + 1)
	lastCargo, _ := excelize.ColumnNumberToName(nFixed + cargoCols)
	if err := f.SetColWidth(sheet, firstCargo, lastCargo, cargoColW); err != nil {
		return nil, "", fmt.Errorf("нмтп: ширина матрицы: %w", err)
	}
	totalName, _ := excelize.ColumnNumberToName(lastCol)
	if err := f.SetColWidth(sheet, totalName, totalName, 12); err != nil {
		return nil, "", fmt.Errorf("нмтп: ширина итога: %w", err)
	}

	cellName := func(col, row int) string {
		n, _ := excelize.CoordinatesToCellName(col, row)
		return n
	}
	set := func(col, row int, v any, style string) {
		_ = f.SetCellValue(sheet, cellName(col, row), v)
		if style != "" {
			_ = f.SetCellStyle(sheet, cellName(col, row), cellName(col, row), styles[style])
		}
	}
	// Стиль на прямоугольник — чтобы заливка и внешние границы покрывали весь merge.
	styleRange := func(c1, r1, c2, r2 int, style string) {
		_ = f.SetCellStyle(sheet, cellName(c1, r1), cellName(c2, r2), styles[style])
	}
	merge := func(c1, r1, c2, r2 int) { _ = f.MergeCell(sheet, cellName(c1, r1), cellName(c2, r2)) }

	// ── Заголовок и шапка ────────────────────────────────────────────────────
	row := 1
	title := fmt.Sprintf("Подход вагонов для %s на %s", r.Terminal, clock.Now().Time().Format("02.01.2006"))
	set(1, row, title, "title")
	merge(1, row, lastCol, row)
	_ = f.SetRowHeight(sheet, row, 20)
	row++

	headTop := row
	for i, c := range fixed {
		set(i+1, row, c.Header, "head")
		merge(i+1, row, i+1, row+2)
		styleRange(i+1, row, i+1, row+2, "head")
	}
	// Уровень 1: клиенты (merge по одинаковым подписям подряд). В компактном
	// режиме узкая группа из 1–2 колонок заворачивает подпись — считаем высоту.
	groupH := 18.0
	col := nFixed + 1
	for i := 0; i < len(r.Columns); {
		j := i
		for j < len(r.Columns) && r.Columns[j].Group == r.Columns[i].Group {
			j++
		}
		set(col, row, r.Columns[i].Group, "headG")
		styleRange(col, row, col+j-i-1, row, "headG")
		if compact {
			if h := float64(wrapLines(r.Columns[i].Group, float64(j-i)*cargoColW))*13 + 4; h > groupH {
				groupH = h
			}
		}
		// Правая граница блока — утолщённая левая у следующего, тут ничего не надо.
		if j-i > 1 {
			merge(col, row, col+j-i-1, row)
		}
		col += j - i
		i = j
	}
	if hasOther {
		set(nFixed+cargoCols, row, "ПРОЧЕЕ", "headOther")
		merge(nFixed+cargoCols, row, nFixed+cargoCols, row+2)
		styleRange(nFixed+cargoCols, row, nFixed+cargoCols, row+2, "headOther")
	}
	set(lastCol, row, "итого", "headTotal")
	merge(lastCol, row, lastCol, row+2)
	styleRange(lastCol, row, lastCol, row+2, "headTotal")
	// Уровни 2–3: станции и марки. В компактном режиме — вертикальные стили.
	stationStyle := func(i int) (string, string) {
		st, stMark := "head", "headMark"
		if groupStart[i] {
			st, stMark = "headG", "headMarkG"
		}
		// Марки «…-ОТД» — мягко-жёлтая заливка подзаголовка (владелец, 31.07.2026).
		if strings.HasSuffix(r.Columns[i].Mark, "-ОТД") {
			stMark += "Y"
		}
		if compact {
			st, stMark = st+"V", stMark+"V"
		}
		return st, stMark
	}
	for i, c := range r.Columns {
		st, stMark := stationStyle(i)
		set(nFixed+1+i, row+1, c.Station, st)
		set(nFixed+1+i, row+2, c.Mark, stMark)
	}
	_ = f.SetRowHeight(sheet, headTop, groupH)
	// Высоты строк станций и марок. Горизонтальный режим: станции заворачиваются
	// в узкой колонке — высота по числу строк самой длинной подписи (у АЭ
	// «Терент, Байкаим, …» не влезал в фиксированные 30). Компактный режим:
	// текст вертикальный — высота по длине подписи (~5.5 пт на символ),
	// потолок 200, чтобы аномально длинная подпись не разнесла шапку.
	stationH, markH := 30.0, 26.0
	if compact {
		for _, c := range r.Columns {
			if h := float64(len([]rune(c.Station)))*5.5 + 8; h > stationH {
				stationH = h
			}
			if h := float64(len([]rune(c.Mark)))*5.5 + 8; h > markH {
				markH = h
			}
		}
		if stationH > 200 {
			stationH = 200
		}
	} else {
		for _, c := range r.Columns {
			if h := float64(wrapLines(c.Station, cargoColW))*13 + 4; h > stationH {
				stationH = h
			}
		}
	}
	_ = f.SetRowHeight(sheet, headTop+1, stationH)
	_ = f.SetRowHeight(sheet, headTop+2, markH)
	row += 3
	// Шапка закреплена, как в файле порта (там FREEZE над первой строкой данных).
	_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: headTop + 2,
		TopLeftCell: cellName(1, row), ActivePane: "bottomLeft"})

	// ── Секции строк ─────────────────────────────────────────────────────────
	writeRow := func(t domain.NmtpTrainRow, abandoned bool) {
		set(1, row, t.Index, "cellC")
		set(2, row, t.StationOper, "cellC")
		if t.DateNach != nil {
			set(3, row, t.DateNach.Time().Format("02.01.06"), "cellC")
		} else {
			set(3, row, "", "cellC")
		}
		set(4, row, t.Note, "cellC")
		set(5, row, t.ControlVagon, "cellC")
		// У брошенных в колонке прибытия — дата бросания (как в файле порта).
		if abandoned {
			if t.DateBros != nil {
				set(6, row, t.DateBros.Time().Format("02.01.06"), "cellC")
			} else {
				set(6, row, "", "cellC")
			}
			set(7, row, "", "cellC")
		} else if t.Prog != nil && t.Planned {
			// Прибытие печатается только плановым поездам (есть plan_jd) —
			// правило владельца 30.07.2026; бесплановым — пусто.
			v := vladTime(t.Prog)
			set(6, row, v.Format("02.01.06"), "cellC")
			set(7, row, v.Format("15:04"), "cellC")
		} else {
			set(6, row, "", "cellC")
			set(7, row, "", "cellC")
		}
		for c := 0; c < cargoCols; c++ {
			idx := c
			isOther := hasOther && c == cargoCols-1
			if isOther {
				idx = len(r.Columns)
			}
			v := any("")
			if t.Counts[idx] > 0 {
				v = t.Counts[idx]
			}
			style := "num"
			if groupStart[c] {
				style = "numG"
			}
			if isOther && t.Counts[idx] > 0 {
				style = "numOther"
			}
			set(nFixed+1+c, row, v, style)
		}
		set(lastCol, row, t.Total, "totalCol")
		row++
	}
	writeSections := func(secs []domain.NmtpSection, abandoned bool) {
		style := "section"
		if abandoned {
			style = "sectionAb"
		}
		for _, sec := range secs {
			// Пустые причальные станции не печатаем — как на экране (владелец,
			// 31.07.2026: в файле порта они стояли и пустыми, больше не держим);
			// пустые дороги печатаем — полный список, тоже как на экране.
			if sec.IsStation && len(sec.Rows) == 0 {
				continue
			}
			set(1, row, sec.Label, style)
			merge(1, row, lastCol-1, row)
			styleRange(1, row, lastCol-1, row, style)
			v := any("")
			if sec.Total > 0 {
				v = sec.Total
			}
			set(lastCol, row, v, style)
			row++
			for _, t := range sec.Rows {
				writeRow(t, abandoned)
			}
		}
	}

	// Счётчики — построчно, подписи и порядок как на экране (владелец, 31.07.2026:
	// низ книги приводим к экрану; в файле порта были «кол-во поездов…»).
	sumVagons := func(secs []domain.NmtpSection) int {
		n := 0
		for _, s := range secs {
			n += s.Total
		}
		return n
	}
	counter := func(label string, v int) {
		set(1, row, label, "counterLbl")
		merge(1, row, lastCol-1, row)
		set(lastCol, row, v, "counterVal")
		row++
	}

	writeSections(r.Sections, false)
	counter("Итого вагонов в ходу", sumVagons(r.Sections))
	counter("Итого поездов в ходу", r.TrainsActive)

	set(1, row, "БРОШЕННЫЕ ПОЕЗДА (в колонке прибытия — дата бросания)", "banner")
	merge(1, row, lastCol, row)
	row++
	writeSections(r.Abandoned, true)
	counter("Итого вагонов брошенных", sumVagons(r.Abandoned))
	counter("Итого поездов брошенных", r.TrainsAbandoned)
	counter("Итого поездов на сети", r.TrainsActive+r.TrainsAbandoned)

	// ── Подвал: «Итог по грузовым колонкам» — как на экране ─────────────────
	// Повторение шапки групп и станций перед цифрами: к низу книги шапка уже
	// уехала при прокрутке, без подписей колонки не прочитать.
	set(1, row, "Итог по грузовым колонкам", "footLbl")
	merge(1, row, nFixed, row)
	styleRange(1, row, nFixed, row, "footLbl")
	col = nFixed + 1
	for i := 0; i < len(r.Columns); {
		j := i
		for j < len(r.Columns) && r.Columns[j].Group == r.Columns[i].Group {
			j++
		}
		set(col, row, r.Columns[i].Group, "headG")
		styleRange(col, row, col+j-i-1, row, "headG")
		if j-i > 1 {
			merge(col, row, col+j-i-1, row)
		}
		col += j - i
		i = j
	}
	if hasOther {
		set(nFixed+cargoCols, row, "ПРОЧЕЕ", "headOther")
	}
	set(lastCol, row, "ИТОГО", "headTotal")
	_ = f.SetRowHeight(sheet, row, groupH)
	row++
	set(1, row, "Названия колонок", "footLbl")
	merge(1, row, nFixed, row)
	styleRange(1, row, nFixed, row, "footLbl")
	for i, c := range r.Columns {
		st, _ := stationStyle(i)
		set(nFixed+1+i, row, c.Station, st)
	}
	if hasOther {
		set(nFixed+cargoCols, row, "", "headOther")
	}
	set(lastCol, row, "", "headTotal")
	_ = f.SetRowHeight(sheet, row, stationH)
	row++

	writeFootRow := func(label string, cellVal func(idx int) any, total any) {
		set(1, row, label, "footLbl")
		merge(1, row, nFixed, row)
		styleRange(1, row, nFixed, row, "footLbl")
		for c := 0; c < cargoCols; c++ {
			idx := c
			if hasOther && c == cargoCols-1 {
				idx = len(r.Columns)
			}
			style := "num"
			if groupStart[c] {
				style = "numG"
			}
			set(nFixed+1+c, row, cellVal(idx), style)
		}
		set(lastCol, row, total, "totalCol")
		row++
	}
	writeFootRow("вагонов (шт.)", func(idx int) any {
		if r.ColCounts[idx] > 0 {
			return r.ColCounts[idx]
		}
		return ""
	}, r.TotalVagons)
	// Тоннаж — один знак после запятой (решение владельца 31.07.2026).
	writeFootRow("тонн (тыс. т.)", func(idx int) any {
		if r.ColTons[idx] > 0 {
			return fmt.Sprintf("%.1f", r.ColTons[idx])
		}
		return ""
	}, fmt.Sprintf("%.1f", r.TotalTons))
	row++

	// ── Прогноз, нагрузка, свод по клиентам ─────────────────────────────────
	set(1, row, "Прогноз выгрузки по подходу (ваг/сут)", "plain")
	merge(1, row, 4, row)
	set(5, row, fmt.Sprintf("%.1f", r.UnloadForecast), "plainC")
	row++
	if r.Norm > 0 {
		set(1, row, "Нагрузка на ж/д сеть: загрузка (тыс. ваг)", "plain")
		merge(1, row, 4, row)
		set(5, row, fmt.Sprintf("%.3f", float64(r.TotalVagons)/1000), "plainC")
		row++
		set(1, row, "Норма (вагонов)", "plain")
		merge(1, row, 4, row)
		set(5, row, r.Norm, "plainC")
		row++
		set(1, row, "% ниже нормы", "plain")
		merge(1, row, 4, row)
		set(5, row, fmt.Sprintf("%.1f%%", (1-float64(r.TotalVagons)/float64(r.Norm))*100), "plainC")
		row++
	}
	if len(r.ClientTons) > 0 {
		row++
		set(1, row, "Тоннаж по клиентам (тыс. т.)", "plainB")
		merge(1, row, 2, row)
		row++
		for _, ct := range r.ClientTons {
			set(1, row, ct.Client, "plain")
			set(2, row, fmt.Sprintf("%.1f", ct.Tons), "plainC")
			row++
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, "", fmt.Errorf("нмтп: запись книги: %w", err)
	}
	name := fmt.Sprintf("Подход вагонов %s %s.xlsx", r.Terminal, clock.Now().Time().Format("02.01.06"))
	return buf.Bytes(), name, nil
}
