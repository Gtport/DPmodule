package report

// «Повагонка» — выгрузка текущего снимка дислокации в Excel (перенос gtport
// export_handler.go). Полный снимок до фронта не доезжает (на экранах — только
// агрегаты), поэтому книга собирается на сервере — ровно случай политики пакета.
//
// Две раскладки (декларативно, без рефлексии: колонка = подпись + ширина +
// функция значения):
//   - полная (без терминала) — gtport GetExportColumns("all") с правками
//     владельца от 29.07.2026: убраны «Для СМС1»/«Для СМС2»/«Данные броса»,
//     добавлены «Заявка» (zayavka), «Марка груза» (freight_exact_name —
//     точное наименование груза из источника, с маркой) и «ГТД» (gtd_number);
//     «Операция с вагоном» — полное имя (oper), не краткое;
//   - терминальная (?terminal=) — короткий набор gtport «Повагонка Аттис»
//     (GetExportColumns("at")), обобщённый на любой терминал (решение
//     владельца 29.07.2026): рабочая выгрузка порта без служебных полей.
// Общие отличия от gtport:
//   - «Собственник» = owner (в gtport колонку заполнял rod_vag_uch — код рода
//     вагона, подпись была ошибочной);
//   - «Перестановка» вычисляется на месте (gruzpol_s/naznach при расхождении) —
//     отдельного поля в DPmodule нет;
//   - подписи «План гр»/«План МСК» выправлены в «План МСК»/«План ЖД» (в gtport
//     подписи стояли наоборот к содержимому полей).

import (
	"fmt"

	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// vagonkaColumn — одна колонка выгрузки: подпись, ширина и значение из записи.
type vagonkaColumn struct {
	Header string
	Width  float64
	Value  func(d *domain.Dislocation) any
}

// Хелперы значений: nil-указатели → пустая ячейка (nil), не ноль.
func vagInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func vagF64(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func vagDate(t *domain.LocalTime, layout string) any {
	if t == nil {
		return nil
	}
	return t.Time().Format(layout)
}

const (
	vagDay = "02.01.2006"
	vagMin = "02.01.2006 15:04"
)

// vagonkaTerminalColumns — короткая раскладка повагонки терминала (порядок и
// ширины gtport «Повагонка Аттис»; «Операция с вагоном» — полное имя операции,
// как в gtport at-наборе).
func vagonkaTerminalColumns() []vagonkaColumn {
	return []vagonkaColumn{
		{"Номер вагона", 13, func(d *domain.Dislocation) any { return d.Vagon }},
		{"Накладная", 13, func(d *domain.Dislocation) any { return d.Invoice }},
		{"Нач. накладная", 15, func(d *domain.Dislocation) any { return d.InvoiceMain }},
		{"Индекс поезда", 18, func(d *domain.Dislocation) any { return d.Index }},
		{"Родительский индекс", 18, func(d *domain.Dislocation) any { return d.IndexMain }},
		{"Начало рейса", 13, func(d *domain.Dislocation) any { return vagDate(d.DateNach, vagDay) }},
		{"Дорога отправления", 20, func(d *domain.Dislocation) any { return d.DorogaNach }},
		{"Станция отправления", 30, func(d *domain.Dislocation) any { return d.StationNach }},
		{"Грузоотправитель", 28, func(d *domain.Dislocation) any { return d.Gruzotpr }},
		{"Грузополучатель", 28, func(d *domain.Dislocation) any { return d.Gruzpol }},
		{"Наименование груза", 22, func(d *domain.Dislocation) any { return d.CargoS }},
		{"Вес (тн)", 9, func(d *domain.Dislocation) any { return vagF64(d.Ves) }},
		{"Станция операции", 30, func(d *domain.Dislocation) any { return d.StationOper }},
		{"Дорога операции", 20, func(d *domain.Dislocation) any { return d.DorogaOper }},
		{"Операция с вагоном", 47, func(d *domain.Dislocation) any { return d.Oper }},
		{"Дата операции", 16, func(d *domain.Dislocation) any { return vagDate(d.TimeOp, vagMin) }},
		{"Номер в поезде", 15, func(d *domain.Dislocation) any { return vagInt(d.NppVag) }},
		{"Срок доставки", 13, func(d *domain.Dislocation) any { return vagDate(d.DateDostav, vagDay) }},
		{"Расстояние ост(км)", 18.5, func(d *domain.Dislocation) any { return vagInt(d.RasstStanNazn) }},
		{"Собственник", 40, func(d *domain.Dislocation) any { return d.Owner }},
	}
}

// vagonkaColumns — полная раскладка повагонки (порядок и ширины gtport "all").
func vagonkaColumns() []vagonkaColumn {
	return []vagonkaColumn{
		{"Номер вагона", 11.7, func(d *domain.Dislocation) any { return d.Vagon }},
		{"Накладная", 13, func(d *domain.Dislocation) any { return d.Invoice }},
		{"Нач. накладная", 15, func(d *domain.Dislocation) any { return d.InvoiceMain }},
		{"Индекс поезда", 16.6, func(d *domain.Dislocation) any { return d.Index }},
		{"Родительский индекс", 17.5, func(d *domain.Dislocation) any { return d.IndexMain }},
		{"Индекс вчера", 16.6, func(d *domain.Dislocation) any { return d.IndexLast }},
		{"Начало рейса", 13, func(d *domain.Dislocation) any { return vagDate(d.DateNach, vagDay) }},
		{"Дорога отправления", 20, func(d *domain.Dislocation) any { return d.DorogaNach }},
		{"Станция отправления", 30, func(d *domain.Dislocation) any { return d.StationNach }},
		{"Заявка", 14, func(d *domain.Dislocation) any { return d.Zayavka }},
		{"Грузоотправитель (ОКПО)", 24, func(d *domain.Dislocation) any { return d.GruzotprOkpo }},
		{"Грузоотправитель", 28, func(d *domain.Dislocation) any { return d.Gruzotpr }},
		{"Станция назначения", 19, func(d *domain.Dislocation) any { return d.StanNazn }},
		{"Грузополучатель", 28, func(d *domain.Dislocation) any { return d.Gruzpol }},
		{"Наименование груза", 22, func(d *domain.Dislocation) any { return d.CargoS }},
		{"Марка груза", 40, func(d *domain.Dislocation) any { return d.FreightExactName }},
		{"ГТД", 14, func(d *domain.Dislocation) any { return d.GtdNumber }},
		{"Вес (тн)", 9, func(d *domain.Dislocation) any { return vagF64(d.Ves) }},
		{"Вес (кг)", 9, func(d *domain.Dislocation) any {
			if d.Ves == nil {
				return nil
			}
			return *d.Ves * 1000
		}},
		{"Станция операции", 30, func(d *domain.Dislocation) any { return d.StationOper }},
		{"Дорога операции", 20, func(d *domain.Dislocation) any { return d.DorogaOper }},
		{"Код ст операции", 13, func(d *domain.Dislocation) any { return d.CodeStationOper }},
		{"Операция с вагоном", 47, func(d *domain.Dislocation) any { return d.Oper }},
		{"Код операции", 14, func(d *domain.Dislocation) any { return d.CodeOper }},
		{"Дата операции", 16, func(d *domain.Dislocation) any { return vagDate(d.TimeOp, vagMin) }},
		{"Номер в поезде", 15, func(d *domain.Dislocation) any { return vagInt(d.NppVag) }},
		{"Срок доставки", 13, func(d *domain.Dislocation) any { return vagDate(d.DateDostav, vagDay) }},
		{"Расстояние ост(км)", 18.5, func(d *domain.Dislocation) any { return vagInt(d.RasstStanNazn) }},
		{"Получатель", 12, func(d *domain.Dislocation) any { return d.GruzpolS }},
		{"Назначение", 12, func(d *domain.Dislocation) any { return d.Naznach }},
		{"Перестановка", 12, func(d *domain.Dislocation) any {
			if d.GruzpolS != "" && d.Naznach != "" && d.GruzpolS != d.Naznach {
				return d.GruzpolS + "/" + d.Naznach
			}
			return ""
		}},
		{"Индекс ПП", 17, func(d *domain.Dislocation) any { return d.IndexPp }},
		{"План МСК", 16, func(d *domain.Dislocation) any { return vagDate(d.PlanMsk, vagMin) }},
		{"План ЖД", 16, func(d *domain.Dislocation) any { return vagDate(d.PlanJd, vagMin) }},
		{"Расчет", 16, func(d *domain.Dislocation) any { return vagDate(d.RaschJd, vagMin) }},
		{"Прогноз", 16, func(d *domain.Dislocation) any { return vagDate(d.ProgJd, vagMin) }},
		{"Смещение", 10, func(d *domain.Dislocation) any { return vagF64(d.Mistake) }},
		{"Простой дн", 10, func(d *domain.Dislocation) any { return vagInt(d.ProstDn) }},
		{"Простой час", 10, func(d *domain.Dislocation) any { return vagInt(d.ProstCh) }},
		{"Статус", 7, func(d *domain.Dislocation) any { return vagInt(d.Status) }},
		{"Сегмент", 10, func(d *domain.Dislocation) any { return d.CargoGroup }},
		{"Клиент", 12, func(d *domain.Dislocation) any { return d.Client }},
		{"Собственник", 40, func(d *domain.Dislocation) any { return d.Owner }},
	}
}

// VagonkaXLSX собирает книгу «Повагонка» из снимка. terminal — краткое имя
// причала (gruzpol_s); пусто — весь снимок. Возвращает байты книги и имя файла
// (штамп — clock.Now(), московское naive; никакого Asia/Vladivostok из gtport).
func VagonkaXLSX(records []domain.Dislocation, terminal string) ([]byte, string, error) {
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "Повагонка"
	if err := f.SetSheetName(f.GetSheetName(0), sheet); err != nil {
		return nil, "", fmt.Errorf("повагонка: имя листа: %w", err)
	}

	// Терминальная выгрузка — короткий рабочий набор, полная — все колонки.
	cols := vagonkaColumns()
	if terminal != "" {
		cols = vagonkaTerminalColumns()
	}
	for i, col := range cols {
		name, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(sheet, name, name, col.Width); err != nil {
			return nil, "", fmt.Errorf("повагонка: ширина колонки %s: %w", col.Header, err)
		}
	}

	headStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"F5F5F5"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	})
	if err != nil {
		return nil, "", fmt.Errorf("повагонка: стиль шапки: %w", err)
	}

	headers := make([]any, len(cols))
	for i, col := range cols {
		headers[i] = col.Header
	}
	if err := f.SetSheetRow(sheet, "A1", &headers); err != nil {
		return nil, "", fmt.Errorf("повагонка: шапка: %w", err)
	}
	lastCol, _ := excelize.ColumnNumberToName(len(cols))
	if err := f.SetCellStyle(sheet, "A1", lastCol+"1", headStyle); err != nil {
		return nil, "", fmt.Errorf("повагонка: стиль шапки: %w", err)
	}
	// Шапка закреплена — снимок длинный.
	if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return nil, "", fmt.Errorf("повагонка: закрепление шапки: %w", err)
	}

	row := 2
	for i := range records {
		rec := &records[i]
		if terminal != "" && rec.GruzpolS != terminal {
			continue
		}
		values := make([]any, len(cols))
		for j, col := range cols {
			values[j] = col.Value(rec)
		}
		cell, _ := excelize.CoordinatesToCellName(1, row)
		if err := f.SetSheetRow(sheet, cell, &values); err != nil {
			return nil, "", fmt.Errorf("повагонка: строка %d: %w", row, err)
		}
		row++
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, "", fmt.Errorf("повагонка: запись книги: %w", err)
	}

	stamp := clock.Now().Time().Format("02.01.06 15-04")
	name := fmt.Sprintf("Полная повагонка %s.xlsx", stamp)
	if terminal != "" {
		name = fmt.Sprintf("Повагонка %s %s.xlsx", terminal, stamp)
	}
	return buf.Bytes(), name, nil
}
