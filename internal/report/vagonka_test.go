package report_test

// Тест «Повагонки»: значения колонок по одной заполненной записи (страховка
// раскладки — колонка/подпись/значение) + фильтр терминала и пустые указатели.

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/report"
)

func vagLT(day, hour int) *domain.LocalTime {
	lt := domain.LocalTime(time.Date(2026, 7, day, hour, 30, 0, 0, time.UTC))
	return &lt
}

func vagTestRecord() domain.Dislocation {
	ves := 69.5
	npp := 12
	rasst := 350
	status := 5
	prostDn := 2
	return domain.Dislocation{
		Vagon: "60000001", Invoice: "ЭА123456", InvoiceMain: "ЭА000001",
		Index: "12340-111-98050", IndexMain: "12340-999-98050", IndexLast: "12340-110-98050",
		IndexPp:  "12340-555-98050",
		DateNach: vagLT(1, 8), DorogaNach: "ЗСБ", StationNach: "ЕРУНАКОВО",
		GruzotprOkpo: "12345678", Gruzotpr: "РАЗРЕЗ",
		StanNazn: "МЫС АСТАФЬЕВА", Gruzpol: "АО ПОРТ", GruzpolS: "АЭ", Naznach: "ГУТ-2",
		CargoS: "УГОЛЬ", Ves: &ves,
		StationOper: "РУЖИНО", DorogaOper: "ДВС", CodeStationOper: "98010",
		OperS: "Брошен", CodeOper: "92", TimeOp: vagLT(28, 14),
		NppVag: &npp, DateDostav: vagLT(30, 0), RasstStanNazn: &rasst,
		PlanMsk: vagLT(30, 9), PlanJd: vagLT(30, 18), RaschJd: vagLT(30, 20), ProgJd: vagLT(30, 21),
		ProstDn: &prostDn, Status: &status,
		CargoGroup: "УГОЛЬ", Sms1: "СМС1", Sms2: "СМС2", Client: "КЛЦ МАРИС",
		Param1: "брос 28.07", Owner: "СОБСТВЕННИК ООО", RodVagUch: "20",
	}
}

func TestVagonkaXLSX_Values(t *testing.T) {
	data, name, err := report.VagonkaXLSX([]domain.Dislocation{vagTestRecord()}, "")
	require.NoError(t, err)
	assert.Contains(t, name, "Полная повагонка")

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows("Повагонка")
	require.NoError(t, err)
	require.Len(t, rows, 2)

	byHeader := map[string]string{}
	for i, h := range rows[0] {
		if i < len(rows[1]) {
			byHeader[h] = rows[1][i]
		} else {
			byHeader[h] = ""
		}
	}

	want := map[string]string{
		"Номер вагона":        "60000001",
		"Индекс поезда":       "12340-111-98050",
		"Родительский индекс": "12340-999-98050",
		"Индекс ПП":           "12340-555-98050",
		"Начало рейса":        "01.07.2026",
		"Станция отправления": "ЕРУНАКОВО",
		"Вес (тн)":            "69.5",
		"Вес (кг)":            "69500",
		"Операция с вагоном":  "Брошен",
		"Дата операции":       "28.07.2026 14:30",
		"Номер в поезде":      "12",
		"Расстояние ост(км)":  "350",
		"Получатель":          "АЭ",
		"Назначение":          "ГУТ-2",
		"Перестановка":        "АЭ/ГУТ-2", // вычисляется: gruzpol_s ≠ naznach
		"План МСК":            "30.07.2026 09:30",
		"План ЖД":             "30.07.2026 18:30",
		"Прогноз":             "30.07.2026 21:30",
		"Статус":              "5",
		"Клиент":              "КЛЦ МАРИС",
		"Собственник":         "СОБСТВЕННИК ООО", // owner, НЕ rod_vag_uch (в gtport была ошибка подписи)
	}
	for header, exp := range want {
		assert.Equal(t, exp, byHeader[header], "колонка %q", header)
	}
}

// Фильтр терминала: чужая запись выпадает; пустые указатели дают пустые ячейки.
func TestVagonkaXLSX_TerminalFilterAndNils(t *testing.T) {
	own := vagTestRecord()
	foreign := vagTestRecord()
	foreign.Vagon, foreign.GruzpolS = "61000001", "УТ-1"
	empty := domain.Dislocation{Vagon: "62000001", GruzpolS: "АЭ", Naznach: "АЭ"}

	data, name, err := report.VagonkaXLSX([]domain.Dislocation{own, foreign, empty}, "АЭ")
	require.NoError(t, err)
	assert.Contains(t, name, "Повагонка АЭ")

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows("Повагонка")
	require.NoError(t, err)
	require.Len(t, rows, 3) // шапка + своя + пустая (чужая выпала)
	assert.Equal(t, "60000001", rows[1][0])
	assert.Equal(t, "62000001", rows[2][0])
	// У пустой записи перестановки нет (назначение совпадает с получателем).
	got := rows[2]
	for i, h := range rows[0] {
		if h == "Перестановка" && i < len(got) {
			assert.Equal(t, "", got[i])
		}
	}
}
