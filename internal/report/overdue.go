package report

// «Просрочка доставки» — Excel для претензионной работы с перевозчиком по
// ст. 97 УЖТ (пени за просрочку доставки: 6% провозной платы за каждые сутки,
// но не более 50%). Два листа: «Накладные» — единица претензии (пени считаются
// от провозной платы по накладной), агрегаты по вагонам накладной (даты — min,
// прибытие и просрочка — max: даты внутри накладной могут расходиться по
// вагонам, повагонная фактура — на втором листе «Вагоны»). Провозной платы в
// наших данных нет — под неё и под пени оставлены пустые графы для ручного
// заполнения (решение владельца 11.08.2026).
//
// Строки приходят курсором репозитория, отсортированные по накладной; лист
// «Вагоны» пишется StreamWriter'ом по ходу обхода, агрегаты «Накладных»
// копятся в памяти (счёт на сотни накладных) и пишутся вторым проходом.
// Оформление — как report/history.go: тонкие границы, серая шапка E0E0E0,
// даты дд.ММ.гггг, закреплённая шапка.

import (
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/domain"
)

const overdueNoInvoice = "(без накладной)"

// overdueInvoiceAgg — агрегат накладной для листа «Накладные».
type overdueInvoiceAgg struct {
	Invoice     string
	StationNach string
	Gruzotpr    string
	StanNazn    string
	GruzpolS    string
	CargoS      string
	VagonCount  int
	DateNachD   *domain.LocalTime // min — дата приёма к перевозке
	DateDostav  *domain.LocalTime // min — нормативный срок доставки
	DatePribD   *domain.LocalTime // max — фактическое прибытие
	MaxDelay    int
}

// OverdueClaimXLSX собирает книгу «Просрочка доставки» из строк vagon_history
// (delay > 0), которые iterate отдаёт по одной в порядке сортировки по накладной.
func OverdueClaimXLSX(iterate func(fn func(domain.VagonHistory) error) error) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	const invoicesSheet = "Накладные"
	const vagonsSheet = "Вагоны"
	if err := f.SetSheetName(f.GetSheetName(0), invoicesSheet); err != nil {
		return nil, fmt.Errorf("просрочка: имя листа: %w", err)
	}
	if _, err := f.NewSheet(vagonsSheet); err != nil {
		return nil, fmt.Errorf("просрочка: лист вагонов: %w", err)
	}

	thin := []excelize.Border{
		{Type: "left", Style: 1, Color: "000000"},
		{Type: "right", Style: 1, Color: "000000"},
		{Type: "top", Style: 1, Color: "000000"},
		{Type: "bottom", Style: 1, Color: "000000"},
	}
	center := &excelize.Alignment{Horizontal: "center", Vertical: "center"}
	headStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true}, Border: thin, Alignment: center,
		Fill: excelize.Fill{Type: "pattern", Color: []string{"E0E0E0"}, Pattern: 1},
	})
	if err != nil {
		return nil, fmt.Errorf("просрочка: стиль шапки: %w", err)
	}
	cellStyle, err := f.NewStyle(&excelize.Style{Border: thin, Alignment: center})
	if err != nil {
		return nil, fmt.Errorf("просрочка: стиль ячейки: %w", err)
	}
	vesFmt := "0.00"
	vesStyle, err := f.NewStyle(&excelize.Style{Border: thin, Alignment: center, CustomNumFmt: &vesFmt})
	if err != nil {
		return nil, fmt.Errorf("просрочка: стиль веса: %w", err)
	}

	// ── Лист «Вагоны»: повагонная фактура, пишется по ходу обхода ───────────
	type vagonColumn struct {
		Header string
		Width  float64
		Value  func(h *domain.VagonHistory) any
		Ves    bool
	}
	vagCols := []vagonColumn{
		{"Накладная", 16, func(h *domain.VagonHistory) any { return overdueInvoiceOf(h) }, false},
		{"Вагон", 13, func(h *domain.VagonHistory) any { return h.Vagon }, false},
		{"Ст. отправления", 20, func(h *domain.VagonHistory) any { return h.StationNach }, false},
		{"Грузоотправитель", 20, func(h *domain.VagonHistory) any { return h.Gruzotpr }, false},
		{"Ст. назначения", 18, func(h *domain.VagonHistory) any { return h.StanNazn }, false},
		{"Грузополучатель", 14, func(h *domain.VagonHistory) any { return h.GruzpolS }, false},
		{"Груз", 20, func(h *domain.VagonHistory) any { return h.CargoS }, false},
		{"Вес", 10, func(h *domain.VagonHistory) any { return vagF64(h.Ves) }, true},
		{"Дата приёма к перевозке", 14, func(h *domain.VagonHistory) any { return vagDate(h.DateNachD, vagDay) }, false},
		{"Срок доставки", 13, func(h *domain.VagonHistory) any { return vagDate(h.DateDostav, vagDay) }, false},
		{"Прибытие", 11, func(h *domain.VagonHistory) any { return vagDate(h.DatePribD, vagDay) }, false},
		{"Суток просрочки", 11, func(h *domain.VagonHistory) any { return vagInt(h.Delay) }, false},
	}

	vw, err := f.NewStreamWriter(vagonsSheet)
	if err != nil {
		return nil, fmt.Errorf("просрочка: stream writer вагонов: %w", err)
	}
	for i, col := range vagCols {
		if err := vw.SetColWidth(i+1, i+1, col.Width); err != nil {
			return nil, fmt.Errorf("просрочка: ширина колонки %s: %w", col.Header, err)
		}
	}
	if err := vw.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return nil, fmt.Errorf("просрочка: закрепление шапки вагонов: %w", err)
	}
	head := make([]any, len(vagCols))
	for i, col := range vagCols {
		head[i] = excelize.Cell{StyleID: headStyle, Value: col.Header}
	}
	if err := vw.SetRow("A1", head); err != nil {
		return nil, fmt.Errorf("просрочка: шапка вагонов: %w", err)
	}

	aggs := map[string]*overdueInvoiceAgg{}
	var order []string
	row := 2
	err = iterate(func(h domain.VagonHistory) error {
		key := overdueInvoiceOf(&h)
		a, ok := aggs[key]
		if !ok {
			a = &overdueInvoiceAgg{
				Invoice: key, StationNach: h.StationNach, Gruzotpr: h.Gruzotpr,
				StanNazn: h.StanNazn, GruzpolS: h.GruzpolS, CargoS: h.CargoS,
			}
			aggs[key] = a
			order = append(order, key)
		}
		a.VagonCount++
		a.DateNachD = minLocal(a.DateNachD, h.DateNachD)
		a.DateDostav = minLocal(a.DateDostav, h.DateDostav)
		a.DatePribD = maxLocal(a.DatePribD, h.DatePribD)
		if h.Delay != nil && *h.Delay > a.MaxDelay {
			a.MaxDelay = *h.Delay
		}

		values := make([]any, len(vagCols))
		for i, col := range vagCols {
			style := cellStyle
			if col.Ves {
				style = vesStyle
			}
			values[i] = excelize.Cell{StyleID: style, Value: col.Value(&h)}
		}
		cell, _ := excelize.CoordinatesToCellName(1, row)
		if err := vw.SetRow(cell, values); err != nil {
			return fmt.Errorf("просрочка: строка вагона %d: %w", row, err)
		}
		row++
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := vw.Flush(); err != nil {
		return nil, fmt.Errorf("просрочка: flush вагонов: %w", err)
	}

	// ── Лист «Накладные»: агрегаты + пустые графы платы и пеней ─────────────
	type invColumn struct {
		Header string
		Width  float64
		Value  func(a *overdueInvoiceAgg) any
	}
	invCols := []invColumn{
		{"Накладная", 16, func(a *overdueInvoiceAgg) any { return a.Invoice }},
		{"Вагонов", 9, func(a *overdueInvoiceAgg) any { return a.VagonCount }},
		{"Ст. отправления", 20, func(a *overdueInvoiceAgg) any { return a.StationNach }},
		{"Грузоотправитель", 20, func(a *overdueInvoiceAgg) any { return a.Gruzotpr }},
		{"Ст. назначения", 18, func(a *overdueInvoiceAgg) any { return a.StanNazn }},
		{"Грузополучатель", 14, func(a *overdueInvoiceAgg) any { return a.GruzpolS }},
		{"Груз", 20, func(a *overdueInvoiceAgg) any { return a.CargoS }},
		{"Дата приёма к перевозке", 14, func(a *overdueInvoiceAgg) any { return vagDate(a.DateNachD, vagDay) }},
		{"Срок доставки", 13, func(a *overdueInvoiceAgg) any { return vagDate(a.DateDostav, vagDay) }},
		{"Прибытие", 11, func(a *overdueInvoiceAgg) any { return vagDate(a.DatePribD, vagDay) }},
		{"Суток просрочки", 11, func(a *overdueInvoiceAgg) any { return a.MaxDelay }},
		{"Провозная плата, руб.", 16, func(a *overdueInvoiceAgg) any { return nil }},
		{"Пени, руб. (6%/сут, не более 50%)", 22, func(a *overdueInvoiceAgg) any { return nil }},
	}

	iw, err := f.NewStreamWriter(invoicesSheet)
	if err != nil {
		return nil, fmt.Errorf("просрочка: stream writer накладных: %w", err)
	}
	for i, col := range invCols {
		if err := iw.SetColWidth(i+1, i+1, col.Width); err != nil {
			return nil, fmt.Errorf("просрочка: ширина колонки %s: %w", col.Header, err)
		}
	}
	if err := iw.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return nil, fmt.Errorf("просрочка: закрепление шапки накладных: %w", err)
	}
	head = make([]any, len(invCols))
	for i, col := range invCols {
		head[i] = excelize.Cell{StyleID: headStyle, Value: col.Header}
	}
	if err := iw.SetRow("A1", head); err != nil {
		return nil, fmt.Errorf("просрочка: шапка накладных: %w", err)
	}
	for n, key := range order {
		a := aggs[key]
		values := make([]any, len(invCols))
		for i, col := range invCols {
			values[i] = excelize.Cell{StyleID: cellStyle, Value: col.Value(a)}
		}
		cell, _ := excelize.CoordinatesToCellName(1, n+2)
		if err := iw.SetRow(cell, values); err != nil {
			return nil, fmt.Errorf("просрочка: строка накладной %d: %w", n+2, err)
		}
	}
	if err := iw.Flush(); err != nil {
		return nil, fmt.Errorf("просрочка: flush накладных: %w", err)
	}

	f.SetActiveSheet(0)
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("просрочка: запись книги: %w", err)
	}
	return buf.Bytes(), nil
}

// overdueInvoiceOf — ключ накладной строки истории: invoice, при пустом —
// invoice_main, обе пустые — маркер «(без накладной)».
func overdueInvoiceOf(h *domain.VagonHistory) string {
	if h.Invoice != "" {
		return h.Invoice
	}
	if h.InvoiceMain != "" {
		return h.InvoiceMain
	}
	return overdueNoInvoice
}

func minLocal(a, b *domain.LocalTime) *domain.LocalTime {
	if b == nil {
		return a
	}
	if a == nil || time.Time(*b).Before(time.Time(*a)) {
		return b
	}
	return a
}

func maxLocal(a, b *domain.LocalTime) *domain.LocalTime {
	if b == nil {
		return a
	}
	if a == nil || time.Time(*b).After(time.Time(*a)) {
		return b
	}
	return a
}
