package report

// «Подход вагонов» по форме порта (НМТП) — книга .xlsx из domain.NmtpReport.
// Раскладка повторяет файл порта («ПОДХОД ВАГОНОВ ГУТ-2/УТ-1»): шапка в три
// уровня (клиент / станции погрузки / марка), слева реквизиты поезда, секции
// строк по станциям терминалов и дорогам (восток → запад) с итогами, ниже —
// секция «БРОШЕННЫЕ ПОЕЗДА» (в колонке прибытия — дата бросания, как в файле),
// подвал: вагоны и тоннаж по колонкам, счётчики поездов, «Прогноз выгрузки по
// подходу», «Нагрузка на ж/д сеть», свод тоннажа по клиентам.
//
// Отличия от файла порта (осознанные): колонка «СУДНО» называется «ПРИМЕЧАНИЕ»
// (в файле в ней жили пометки перестановок — они и выводятся: «НА {терминал}» /
// «С {терминал}»); колонки «ПРИОРИТЕТ» нет (ручное поле порта — решение
// владельца 29.07.2026: правки убираем); служебные формульные колонки порта
// (п/н/м/у, «Столбец1…») не переносятся.

import (
	"fmt"
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

	// ── Стили ────────────────────────────────────────────────────────────────
	styles := map[string]int{}
	mk := func(name string, st *excelize.Style) error {
		id, err := f.NewStyle(st)
		if err != nil {
			return fmt.Errorf("нмтп: стиль %s: %w", name, err)
		}
		styles[name] = id
		return nil
	}
	border := []excelize.Border{
		{Type: "left", Style: 1, Color: "000000"}, {Type: "right", Style: 1, Color: "000000"},
		{Type: "top", Style: 1, Color: "000000"}, {Type: "bottom", Style: 1, Color: "000000"},
	}
	center := &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true}
	if err := mk("title", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 12}, Alignment: center}); err != nil {
		return nil, "", err
	}
	if err := mk("head", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 8}, Border: border,
		Fill: excelize.Fill{Type: "pattern", Color: []string{"F5F5F5"}, Pattern: 1}, Alignment: center}); err != nil {
		return nil, "", err
	}
	if err := mk("cell", &excelize.Style{Font: &excelize.Font{Size: 8}, Border: border,
		Alignment: &excelize.Alignment{Vertical: "center"}}); err != nil {
		return nil, "", err
	}
	if err := mk("cellC", &excelize.Style{Font: &excelize.Font{Size: 8}, Border: border, Alignment: center}); err != nil {
		return nil, "", err
	}
	if err := mk("section", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 8}, Border: border,
		Fill: excelize.Fill{Type: "pattern", Color: []string{"E8E8E8"}, Pattern: 1}, Alignment: center}); err != nil {
		return nil, "", err
	}
	if err := mk("sectionAb", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 8}, Border: border,
		Fill: excelize.Fill{Type: "pattern", Color: []string{"CFF0FC"}, Pattern: 1}, Alignment: center}); err != nil {
		return nil, "", err
	}
	if err := mk("total", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 8}, Border: border,
		Fill: excelize.Fill{Type: "pattern", Color: []string{"F5F5F5"}, Pattern: 1}, Alignment: center}); err != nil {
		return nil, "", err
	}
	if err := mk("other", &excelize.Style{Font: &excelize.Font{Bold: true, Size: 8}, Border: border,
		Fill: excelize.Fill{Type: "pattern", Color: []string{"FFE7BA"}, Pattern: 1}, Alignment: center}); err != nil {
		return nil, "", err
	}

	// ── Геометрия ────────────────────────────────────────────────────────────
	// Слева 6 фиксированных колонок, дальше матрица груза, последняя — «итого».
	fixed := []struct {
		Header string
		Width  float64
	}{
		{"ПОЕЗД", 15}, {"СТАНЦИЯ", 18}, {"ДАТА\n(принято к перевозке)", 11},
		{"ПРИМЕЧАНИЕ", 11}, {"ВАГОН\n(для контроля)", 11}, {"ожид. дата приб.", 10}, {"ожид. время приб. (влад.)", 10},
	}
	nFixed := len(fixed)
	cargoCols := len(r.Columns)
	hasOther := r.HasOther
	if hasOther {
		cargoCols++
	}
	lastCol := nFixed + cargoCols + 1 // +«итого»

	for i, c := range fixed {
		name, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(sheet, name, name, c.Width); err != nil {
			return nil, "", fmt.Errorf("нмтп: ширина: %w", err)
		}
	}
	firstCargo, _ := excelize.ColumnNumberToName(nFixed + 1)
	lastCargo, _ := excelize.ColumnNumberToName(nFixed + cargoCols)
	if err := f.SetColWidth(sheet, firstCargo, lastCargo, 7); err != nil {
		return nil, "", fmt.Errorf("нмтп: ширина матрицы: %w", err)
	}
	totalName, _ := excelize.ColumnNumberToName(lastCol)
	if err := f.SetColWidth(sheet, totalName, totalName, 8); err != nil {
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
	merge := func(c1, r1, c2, r2 int) { _ = f.MergeCell(sheet, cellName(c1, r1), cellName(c2, r2)) }

	// ── Заголовок и шапка ────────────────────────────────────────────────────
	row := 1
	title := fmt.Sprintf("Подход вагонов для %s на %s", r.Terminal, clock.Now().Time().Format("02.01.2006"))
	set(1, row, title, "title")
	merge(1, row, lastCol, row)
	row++

	headTop := row
	for i, c := range fixed {
		set(i+1, row, c.Header, "head")
		merge(i+1, row, i+1, row+2)
	}
	// Уровень 1: клиенты (merge по одинаковым подписям подряд).
	col := nFixed + 1
	for i := 0; i < len(r.Columns); {
		j := i
		for j < len(r.Columns) && r.Columns[j].Group == r.Columns[i].Group {
			j++
		}
		set(col, row, r.Columns[i].Group, "head")
		if j-i > 1 {
			merge(col, row, col+j-i-1, row)
		}
		col += j - i
		i = j
	}
	if hasOther {
		set(nFixed+cargoCols, row, "ПРОЧЕЕ", "other")
		merge(nFixed+cargoCols, row, nFixed+cargoCols, row+2)
	}
	set(lastCol, row, "итого", "head")
	merge(lastCol, row, lastCol, row+2)
	// Уровни 2–3: станции и марки.
	for i, c := range r.Columns {
		set(nFixed+1+i, row+1, c.Station, "head")
		set(nFixed+1+i, row+2, c.Mark, "head")
	}
	_ = headTop
	row += 3

	// ── Секции строк ─────────────────────────────────────────────────────────
	writeRow := func(t domain.NmtpTrainRow, abandoned bool) {
		set(1, row, t.Index, "cell")
		set(2, row, t.StationOper, "cell")
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
		} else if t.Prog != nil {
			v := vladTime(t.Prog)
			set(6, row, v.Format("02.01.06"), "cellC")
			set(7, row, v.Format("15:04"), "cellC")
		} else {
			set(6, row, "", "cellC")
			set(7, row, "", "cellC")
		}
		for c := 0; c < cargoCols; c++ {
			idx := c
			if !hasOther && c >= len(r.Columns) {
				break
			}
			v := any("")
			if t.Counts[idx] > 0 {
				v = t.Counts[idx]
			}
			style := "cellC"
			if hasOther && c == cargoCols-1 && t.Counts[len(r.Columns)] > 0 {
				style = "other"
			}
			// Последняя колонка матрицы при hasOther — «прочее» (индекс len(Columns)).
			if hasOther && c == cargoCols-1 {
				if t.Counts[len(r.Columns)] > 0 {
					v = t.Counts[len(r.Columns)]
				} else {
					v = ""
				}
			}
			set(nFixed+1+c, row, v, style)
		}
		set(lastCol, row, t.Total, "cellC")
		row++
	}
	writeSections := func(secs []domain.NmtpSection, abandoned bool) {
		style := "section"
		if abandoned {
			style = "sectionAb"
		}
		for _, sec := range secs {
			if abandoned && len(sec.Rows) == 0 {
				continue // пустые секции брошенных не печатаем
			}
			set(1, row, sec.Label, style)
			merge(1, row, lastCol-1, row)
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

	writeSections(r.Sections, false)
	set(1, row, "кол-во поездов в движении:", "total")
	merge(1, row, lastCol-1, row)
	set(lastCol, row, r.TrainsActive, "total")
	row++

	set(1, row, "БРОШЕННЫЕ ПОЕЗДА (в колонке прибытия — дата бросания)", "sectionAb")
	merge(1, row, lastCol, row)
	row++
	writeSections(r.Abandoned, true)
	set(1, row, "кол-во брошенных поездов:", "total")
	merge(1, row, lastCol-1, row)
	set(lastCol, row, r.TrainsAbandoned, "total")
	row++

	// ── Подвал: вагоны и тоннаж по колонкам ─────────────────────────────────
	set(1, row, "ВСЕГО вагонов", "total")
	merge(1, row, nFixed, row)
	for c := 0; c < cargoCols; c++ {
		idx := c
		if hasOther && c == cargoCols-1 {
			idx = len(r.Columns)
		}
		v := any("")
		if r.ColCounts[idx] > 0 {
			v = r.ColCounts[idx]
		}
		set(nFixed+1+c, row, v, "total")
	}
	set(lastCol, row, r.TotalVagons, "total")
	row++
	set(1, row, "тоннаж (тыс. т.)", "total")
	merge(1, row, nFixed, row)
	for c := 0; c < cargoCols; c++ {
		idx := c
		if hasOther && c == cargoCols-1 {
			idx = len(r.Columns)
		}
		v := any("")
		if r.ColTons[idx] > 0 {
			v = fmt.Sprintf("%.3f", r.ColTons[idx])
		}
		set(nFixed+1+c, row, v, "total")
	}
	set(lastCol, row, fmt.Sprintf("%.3f", r.TotalTons), "total")
	row += 2

	// ── Прогноз, нагрузка, свод по клиентам ─────────────────────────────────
	set(1, row, "Прогноз выгрузки по подходу (ваг/сут)", "cell")
	merge(1, row, 4, row)
	set(5, row, fmt.Sprintf("%.1f", r.UnloadForecast), "cellC")
	row++
	if r.Norm > 0 {
		set(1, row, "Нагрузка на ж/д сеть: загрузка (тыс. ваг)", "cell")
		merge(1, row, 4, row)
		set(5, row, fmt.Sprintf("%.3f", float64(r.TotalVagons)/1000), "cellC")
		row++
		set(1, row, "Норма (вагонов)", "cell")
		merge(1, row, 4, row)
		set(5, row, r.Norm, "cellC")
		row++
		set(1, row, "% ниже нормы", "cell")
		merge(1, row, 4, row)
		set(5, row, fmt.Sprintf("%.1f%%", (1-float64(r.TotalVagons)/float64(r.Norm))*100), "cellC")
		row++
	}
	if len(r.ClientTons) > 0 {
		row++
		set(1, row, "Тоннаж по клиентам (тыс. т.)", "head")
		merge(1, row, 2, row)
		row++
		for _, ct := range r.ClientTons {
			set(1, row, ct.Client, "cell")
			set(2, row, fmt.Sprintf("%.3f", ct.Tons), "cellC")
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
