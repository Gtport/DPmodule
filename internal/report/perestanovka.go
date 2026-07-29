package report

// «Перестановка на {терминал}» — выгрузка текущих перестановок книгой .xlsx
// (перенос gtport TransferExportButton). В gtport были две кнопки с зашитыми
// направлениями (ГУТ-2 → АЭ и АЭ → ГУТ-2); здесь терминал-цель приходит
// параметром из реестра ports, а строки берутся из снимка по признаку
// перестановки: naznach = цель, gruzpol_s ≠ цели (и непустой). Прибывшие
// исключаются: в gtport отсекался только статус 10, в DPmodule 12 наступает
// после 10 (тоже прибывший), поэтому фильтр — статус < 10.
//
// Книга в два листа, как в оригинале:
//   - «Поезда» — группировка по индексу поезда, «Состав» многострочно
//     (род. индекс | станция погрузки, получатель → назначение, вагонов);
//   - «Вагоны» — повагонная раскладка (порядок и ширины gtport).
// Отходы от gtport: «Собственник» = owner (в gtport — rod_vag_uch, код рода
// вагона, подпись была ошибочной — то же решение, что в повагонке); стиль
// ячеек — как в нашей повагонке (шапка + закрепление, без сплошных рамок).

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
)

// ErrPerestanovkaEmpty — на терминал сейчас ничего не переставляется.
var ErrPerestanovkaEmpty = fmt.Errorf("нет вагонов на перестановку")

// perestanovkaProstoy — «Задержка» gtport: "N дн M ч" из пары простоев.
func perestanovkaProstoy(dn, ch *int) string {
	var parts []string
	if dn != nil && *dn != 0 {
		parts = append(parts, fmt.Sprintf("%d дн", *dn))
	}
	if ch != nil && *ch != 0 {
		parts = append(parts, fmt.Sprintf("%d ч", *ch))
	}
	return strings.Join(parts, " ")
}

// perestanovkaTrain — один поезд листа «Поезда» с накопленным составом.
type perestanovkaTrain struct {
	rec      *domain.Dislocation // реквизиты поезда — из первого вагона
	subCount map[string]int      // ключ подгруппы состава → вагонов
	subOrder []string            // порядок появления подгрупп
}

// PerestanovkaXLSX собирает книгу «Перестановка на {terminal}» из снимка.
// Терминал обязателен (краткое имя ports.name_s); нет подходящих вагонов —
// ErrPerestanovkaEmpty.
func PerestanovkaXLSX(records []domain.Dislocation, terminal string) ([]byte, string, error) {
	// Отбор: переставляемые на терминал, ещё не прибывшие.
	var rows []*domain.Dislocation
	for i := range records {
		rec := &records[i]
		if rec.Naznach != terminal || rec.GruzpolS == "" || rec.GruzpolS == terminal {
			continue
		}
		if rec.Status != nil && *rec.Status >= 10 {
			continue
		}
		rows = append(rows, rec)
	}
	if len(rows) == 0 {
		return nil, "", ErrPerestanovkaEmpty
	}
	// Повагонный порядок: индекс поезда, внутри — номер в поезде (пустые в конец).
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Index != rows[j].Index {
			return rows[i].Index < rows[j].Index
		}
		ni, nj := rows[i].NppVag, rows[j].NppVag
		switch {
		case ni == nil:
			return false
		case nj == nil:
			return true
		default:
			return *ni < *nj
		}
	})

	// Группировка листа «Поезда» (порядок — по отсортированным вагонам).
	trains := map[string]*perestanovkaTrain{}
	var trainOrder []string
	for _, rec := range rows {
		tr, ok := trains[rec.Index]
		if !ok {
			tr = &perestanovkaTrain{rec: rec, subCount: map[string]int{}}
			trains[rec.Index] = tr
			trainOrder = append(trainOrder, rec.Index)
		}
		sub := fmt.Sprintf("%s | %s %s → %s", rec.IndexMain, rec.StationNach, rec.GruzpolS, rec.Naznach)
		if _, ok := tr.subCount[sub]; !ok {
			tr.subOrder = append(tr.subOrder, sub)
		}
		tr.subCount[sub]++
	}

	f := excelize.NewFile()
	defer f.Close()

	// ── Лист «Поезда» ────────────────────────────────────────────────────────
	const trainsSheet = "Поезда"
	if err := f.SetSheetName(f.GetSheetName(0), trainsSheet); err != nil {
		return nil, "", fmt.Errorf("перестановка: имя листа: %w", err)
	}
	trainCols := []struct {
		Header string
		Width  float64
	}{
		{"Индекс", 15}, {"Станция операции", 25}, {"Дорога операции", 15},
		{"Операция", 30}, {"План", 20}, {"Прогноз", 20}, {"Задержка", 15}, {"Состав", 80},
	}
	for i, col := range trainCols {
		name, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(trainsSheet, name, name, col.Width); err != nil {
			return nil, "", fmt.Errorf("перестановка: ширина колонки %s: %w", col.Header, err)
		}
	}
	headStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"F5F5F5"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	})
	if err != nil {
		return nil, "", fmt.Errorf("перестановка: стиль шапки: %w", err)
	}
	wrapStyle, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	if err != nil {
		return nil, "", fmt.Errorf("перестановка: стиль состава: %w", err)
	}

	trainHeaders := make([]any, len(trainCols))
	for i, col := range trainCols {
		trainHeaders[i] = col.Header
	}
	if err := f.SetSheetRow(trainsSheet, "A1", &trainHeaders); err != nil {
		return nil, "", fmt.Errorf("перестановка: шапка поездов: %w", err)
	}
	if err := f.SetCellStyle(trainsSheet, "A1", "H1", headStyle); err != nil {
		return nil, "", fmt.Errorf("перестановка: стиль шапки поездов: %w", err)
	}
	for i, idx := range trainOrder {
		tr := trains[idx]
		var sostav []string
		for _, sub := range tr.subOrder {
			sostav = append(sostav, fmt.Sprintf("%s (%d)", sub, tr.subCount[sub]))
		}
		values := []any{
			tr.rec.Index, tr.rec.StationOper, tr.rec.DorogaOper, tr.rec.Oper,
			vagDate(tr.rec.PlanJd, vagMin), vagDate(tr.rec.ProgJd, vagMin),
			perestanovkaProstoy(tr.rec.ProstDn, tr.rec.ProstCh),
			strings.Join(sostav, "\n"),
		}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := f.SetSheetRow(trainsSheet, cell, &values); err != nil {
			return nil, "", fmt.Errorf("перестановка: поезд %s: %w", idx, err)
		}
		sostavCell, _ := excelize.CoordinatesToCellName(8, i+2)
		if err := f.SetCellStyle(trainsSheet, sostavCell, sostavCell, wrapStyle); err != nil {
			return nil, "", fmt.Errorf("перестановка: стиль состава %s: %w", idx, err)
		}
	}

	// ── Лист «Вагоны» ────────────────────────────────────────────────────────
	const vagonsSheet = "Вагоны"
	if _, err := f.NewSheet(vagonsSheet); err != nil {
		return nil, "", fmt.Errorf("перестановка: лист вагонов: %w", err)
	}
	vagonCols := []vagonkaColumn{
		{"№", 6, func(d *domain.Dislocation) any { return vagInt(d.NppVag) }},
		{"Вагон", 13, func(d *domain.Dislocation) any { return d.Vagon }},
		{"Накладная", 14, func(d *domain.Dislocation) any { return d.Invoice }},
		{"Индекс", 15, func(d *domain.Dislocation) any { return d.Index }},
		{"Род. индекс", 18, func(d *domain.Dislocation) any { return d.IndexMain }},
		{"Станция погрузки", 38, func(d *domain.Dislocation) any { return d.StationNach }},
		{"Отправитель", 32, func(d *domain.Dislocation) any { return d.Gruzotpr }},
		{"Груз", 22, func(d *domain.Dislocation) any { return d.CargoS }},
		{"Вес", 10, func(d *domain.Dislocation) any { return vagF64(d.Ves) }},
		{"Дата погрузки", 14, func(d *domain.Dislocation) any { return vagDate(d.DateNach, vagDay) }},
		{"Срок доставки", 14, func(d *domain.Dislocation) any { return vagDate(d.DateDostav, vagDay) }},
		{"Получатель", 11, func(d *domain.Dislocation) any { return d.GruzpolS }},
		{"Назначение", 11, func(d *domain.Dislocation) any { return d.Naznach }},
		{"Станция операции", 25, func(d *domain.Dislocation) any { return d.StationOper }},
		{"Дорога операции", 15, func(d *domain.Dislocation) any { return d.DorogaOper }},
		{"Операция", 30, func(d *domain.Dislocation) any { return d.Oper }},
		{"План", 20, func(d *domain.Dislocation) any { return vagDate(d.PlanJd, vagMin) }},
		{"Прогноз", 20, func(d *domain.Dislocation) any { return vagDate(d.ProgJd, vagMin) }},
		{"Задержка", 15, func(d *domain.Dislocation) any { return perestanovkaProstoy(d.ProstDn, d.ProstCh) }},
		{"Собственник", 42, func(d *domain.Dislocation) any { return d.Owner }},
	}
	for i, col := range vagonCols {
		name, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetColWidth(vagonsSheet, name, name, col.Width); err != nil {
			return nil, "", fmt.Errorf("перестановка: ширина колонки %s: %w", col.Header, err)
		}
	}
	vagonHeaders := make([]any, len(vagonCols))
	for i, col := range vagonCols {
		vagonHeaders[i] = col.Header
	}
	if err := f.SetSheetRow(vagonsSheet, "A1", &vagonHeaders); err != nil {
		return nil, "", fmt.Errorf("перестановка: шапка вагонов: %w", err)
	}
	lastCol, _ := excelize.ColumnNumberToName(len(vagonCols))
	if err := f.SetCellStyle(vagonsSheet, "A1", lastCol+"1", headStyle); err != nil {
		return nil, "", fmt.Errorf("перестановка: стиль шапки вагонов: %w", err)
	}
	if err := f.SetPanes(vagonsSheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return nil, "", fmt.Errorf("перестановка: закрепление шапки: %w", err)
	}
	for i, rec := range rows {
		values := make([]any, len(vagonCols))
		for j, col := range vagonCols {
			values[j] = col.Value(rec)
		}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := f.SetSheetRow(vagonsSheet, cell, &values); err != nil {
			return nil, "", fmt.Errorf("перестановка: строка %d: %w", i+2, err)
		}
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, "", fmt.Errorf("перестановка: запись книги: %w", err)
	}
	stamp := clock.Now().Time().Format("02.01.06")
	return buf.Bytes(), fmt.Sprintf("Перестановка на %s %s.xlsx", terminal, stamp), nil
}
