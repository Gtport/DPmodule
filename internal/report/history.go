package report

// «История вагонов» — Excel экрана «Работа с историческими данными» (перенос
// gtport exportToExcel из OperatorToolsHistory.tsx). Отличие от оригинала:
// gtport выгружал то, что уже загружено на клиент (максимум 5000 строк), здесь
// книга собирается на сервере по ВСЕМУ отфильтрованному набору (решение
// владельца 04.08.2026). Потолок строк сторожит сервис (HistoryExportMax);
// сборка — StreamWriter: 100 тыс. строк × 26 колонок = 2,6 млн ячеек, обычный
// путь (SetSheetRow) держал бы всё в RAM. Строки приходят курсором репозитория
// через iterate — набор в память целиком не поднимается.
//
// Оформление — по gtport: тонкие границы всюду, серая жирная шапка (E0E0E0),
// всё по центру, «Вес» числом с форматом 0.00, даты дд.ММ.гггг. Колонки и
// ширины — порядок gtport, с заменами DPmodule: Собст = owner, Марка =
// freight_exact_name, ГТД = gtd_number, «Перегруз» = peregruz (в gtport
// «Отгружен» ← info_3); статусы — полный набор DPmodule (docs/STATUSES.md).

import (
	"fmt"

	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/domain"
)

// historyStatusLabels — подписи статусов DPmodule (зеркало STATUS_LABELS
// экрана «Поезда в движении»).
var historyStatusLabels = map[int]string{
	0: "на отправлении (Б/И)", 1: "на станции отправления", 2: "в пути",
	4: "долгий простой", 5: "брошен", 6: "порожний в пути",
	9: "кандидат в прибывшие", 10: "прибыл", 12: "выгружен",
}

// historyStatusText — текст колонки «Статус»: правило gtport «есть место и дата
// выгрузки → „выгружен“» поверх кода статуса. Ручная пометка «недоехал»
// (удаление из пропавших) сильнее обоих: рейс закрыт человеком.
func historyStatusText(h *domain.VagonHistory) string {
	if h.NotArrived {
		return "недоехал"
	}
	if h.PlaceVigr != "" && h.DateVigr != nil {
		return "выгружен"
	}
	if h.Status == nil {
		return ""
	}
	if label, ok := historyStatusLabels[*h.Status]; ok {
		return label
	}
	return fmt.Sprintf("статус %d", *h.Status)
}

// historyColumn — колонка книги: подпись, ширина (единицы Excel, ширины gtport)
// и значение из строки истории.
type historyColumn struct {
	Header string
	Width  float64
	Value  func(h *domain.VagonHistory) any
	Ves    bool // числовой формат 0.00 (колонка «Вес»)
}

func historyColumns() []historyColumn {
	return []historyColumn{
		{"Вагон", 13, func(h *domain.VagonHistory) any { return h.Vagon }, false},
		{"Накладная Р", 16, func(h *domain.VagonHistory) any { return h.InvoiceMain }, false},
		{"Накладная", 14, func(h *domain.VagonHistory) any { return h.Invoice }, false},
		{"Индекс Р", 16, func(h *domain.VagonHistory) any { return h.IndexMain }, false},
		{"Индекс ПП", 12, func(h *domain.VagonHistory) any { return h.IndexPp }, false},
		{"Дата погр", 11, func(h *domain.VagonHistory) any { return vagDate(h.DateNachD, vagDay) }, false},
		{"Ст. погр", 20, func(h *domain.VagonHistory) any { return h.StationNach }, false},
		{"Грузоотпр", 20, func(h *domain.VagonHistory) any { return h.Gruzotpr }, false},
		{"Грузопол", 12, func(h *domain.VagonHistory) any { return h.GruzpolS }, false},
		{"Назначение", 12, func(h *domain.VagonHistory) any { return h.Naznach }, false},
		{"Груз", 20, func(h *domain.VagonHistory) any { return h.CargoS }, false},
		{"Вес", 10, func(h *domain.VagonHistory) any { return vagF64(h.Ves) }, true},
		{"Клиент", 15, func(h *domain.VagonHistory) any { return h.Client }, false},
		{"Статус", 15, func(h *domain.VagonHistory) any { return historyStatusText(h) }, false},
		{"Срок доставки", 13, func(h *domain.VagonHistory) any { return vagDate(h.DateDostav, vagDay) }, false},
		{"Прибыл", 11, func(h *domain.VagonHistory) any { return vagDate(h.DatePribD, vagDay) }, false},
		// «ПП» убрана (решение владельца 14.08.2026): рейсы истории в основном
		// закрыты, колонка почти всегда пустовала. Как на экране.
		{"Просрочка", 11, func(h *domain.VagonHistory) any { return vagInt(h.Delay) }, false},
		// Две колонки выгрузки (решение владельца 14.08.2026): факт — МСК
		// целиком со временем (уточнение 14.08.2026), ЖД — учётные сутки
		// (после 18:00 факт и учёт расходятся на день; фильтр экрана и
		// счётчики считают по ЖД).
		{"Выгружен (факт)", 16, func(h *domain.VagonHistory) any { return vagDate(h.DateVigr, vagMin) }, false},
		{"Выгружен (ЖД)", 13, func(h *domain.VagonHistory) any { return vagDate(h.DateVigrD, vagDay) }, false},
		{"Терминал", 11, func(h *domain.VagonHistory) any { return h.PlaceVigr }, false},
		{"Смерзаемость", 12, func(h *domain.VagonHistory) any { return vagInt(h.Frost) }, false},
		{"Собст", 10, func(h *domain.VagonHistory) any { return h.Owner }, false},
		{"Марка", 15, func(h *domain.VagonHistory) any { return h.FreightExactName }, false},
		{"ГТД", 15, func(h *domain.VagonHistory) any { return h.GtdNumber }, false},
		{"Судовая", 15, func(h *domain.VagonHistory) any { return h.Shipments }, false},
		{"Перегруз", 12, func(h *domain.VagonHistory) any { return h.Peregruz }, false},
	}
}

// HistoryXLSX собирает книгу «История вагонов» из строк, которые iterate отдаёт
// по одной в порядке сортировки (курсор репозитория). Возвращает байты книги.
func HistoryXLSX(iterate func(fn func(domain.VagonHistory) error) error) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "История вагонов"
	if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
		return nil, fmt.Errorf("история: имя листа: %w", err)
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
		return nil, fmt.Errorf("история: стиль шапки: %w", err)
	}
	cellStyle, err := f.NewStyle(&excelize.Style{Border: thin, Alignment: center})
	if err != nil {
		return nil, fmt.Errorf("история: стиль ячейки: %w", err)
	}
	vesFmt := "0.00"
	vesStyle, err := f.NewStyle(&excelize.Style{Border: thin, Alignment: center, CustomNumFmt: &vesFmt})
	if err != nil {
		return nil, fmt.Errorf("история: стиль веса: %w", err)
	}

	sw, err := f.NewStreamWriter(sheet)
	if err != nil {
		return nil, fmt.Errorf("история: stream writer: %w", err)
	}
	cols := historyColumns()
	for i, col := range cols {
		if err := sw.SetColWidth(i+1, i+1, col.Width); err != nil {
			return nil, fmt.Errorf("история: ширина колонки %s: %w", col.Header, err)
		}
	}
	// Шапка закреплена — выгрузка длинная.
	if err := sw.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return nil, fmt.Errorf("история: закрепление шапки: %w", err)
	}

	head := make([]any, len(cols))
	for i, col := range cols {
		head[i] = excelize.Cell{StyleID: headStyle, Value: col.Header}
	}
	if err := sw.SetRow("A1", head); err != nil {
		return nil, fmt.Errorf("история: шапка: %w", err)
	}

	row := 2
	err = iterate(func(h domain.VagonHistory) error {
		values := make([]any, len(cols))
		for i, col := range cols {
			style := cellStyle
			if col.Ves {
				style = vesStyle
			}
			values[i] = excelize.Cell{StyleID: style, Value: col.Value(&h)}
		}
		cell, _ := excelize.CoordinatesToCellName(1, row)
		if err := sw.SetRow(cell, values); err != nil {
			return fmt.Errorf("история: строка %d: %w", row, err)
		}
		row++
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := sw.Flush(); err != nil {
		return nil, fmt.Errorf("история: flush: %w", err)
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("история: запись книги: %w", err)
	}
	return buf.Bytes(), nil
}
