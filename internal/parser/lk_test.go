package parser_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/parser"
)

// buildLKBytes собирает in-memory xlsx: несколько строк-шапки, затем строка
// заголовка таблицы, затем строки данных. Возвращает байты книги.
func buildLKBytes(t *testing.T, header []string, dataRows [][]string) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := f.GetSheetName(0)
	// Пара строк «шапки» файла — парсер должен их проскочить и найти заголовок.
	require.NoError(t, f.SetSheetRow(sheet, "A1", &[]string{"Личный кабинет"}))
	require.NoError(t, f.SetSheetRow(sheet, "A2", &[]string{"Дислокация вагонов"}))
	require.NoError(t, f.SetSheetRow(sheet, "A3", &header))

	for i, row := range dataRows {
		cell, err := excelize.CoordinatesToCellName(1, 4+i)
		require.NoError(t, err)
		r := row
		require.NoError(t, f.SetSheetRow(sheet, cell, &r))
	}

	buf, err := f.WriteToBuffer()
	require.NoError(t, err)
	return buf.Bytes()
}

// заголовок с полным набором колонок, что покрывает основные преобразования.
var lkHeader = []string{
	"Номер вагона",               // vagon
	"Номер накладной",            // invoice
	"Индекс поезда",              // index
	"Дата и время начала рейса",  // date_nach
	"Станция отправления",        // code_station_nach (из скобок)
	"Станция операции",           // code_station_oper (из скобок)
	"Станция назначения",         // code_stan_nazn
	"Грузоотправитель (ОКПО)",    // gruzotpr_okpo
	"Грузополучатель (ОКПО)",     // gruzpol_okpo
	"Наименование груза",         // code_cargo (из скобок)
	"Вес груза (кг)",             // ves (кг→т)
	"Дата и время операции",      // time_op
	"Операция с вагоном",         // code_oper (из скобок)
	"Тип парка (П/Г)",            // porozh
	"Расстояние оставшееся (км)", // rasst_stan_nazn
	"Время простоя под последней операцией (сутки:часы:минуты)", // prost
	"Идентификатор накладной",                                   // uno (паддинг)
	"Номер вагона в составе поезда",                             // npp_vag
}

func TestLKParser_BasicRow_Mappings(t *testing.T) {
	// DATE_NACH 19:30 → час ≥ 18 → +1 сутки → только дата 2026-07-01.
	// Вес 68000 кг → 68.0 т. Простой «2:12:30» → часы 12, минуты 30.
	// UNO «12345» (число) → паддинг до 12 знаков. Порожний → «1».
	data := [][]string{{
		"12345678",           // Номер вагона
		"АВ123",              // Номер накладной
		"1234 567 8901",      // Индекс поезда (3 части)
		"30.06.2026 19:30",   // Дата и время начала рейса
		"Станция-А (123456)", // Станция отправления
		"Станция-Б (999999)", // Станция операции
		"Станция-В (654321)", // Станция назначения
		"10230304",           // Грузоотправитель (ОКПО)
		"1126022",            // Грузополучатель (ОКПО)
		"Уголь (161005)",     // Наименование груза
		"68000",              // Вес груза (кг)
		"30.06.2026 08:15",   // Дата и время операции
		"Выгрузка (26)",      // Операция с вагоном
		"Порожний",           // Тип парка
		"1000",               // Расстояние оставшееся
		"2:12:30",            // Простой сутки:часы:минуты
		"12345",              // Идентификатор накладной
		"5",                  // Номер в составе поезда
	}}

	recs, err := parser.NewLKParser(parser.DefaultProfile()).
		ParseBytes(buildLKBytes(t, lkHeader, data))
	require.NoError(t, err)
	require.Len(t, recs, 1)
	r := recs[0]

	assert.Equal(t, "12345678", r.Vagon)
	assert.Equal(t, "АВ123", r.Invoice)
	assert.Equal(t, "123456", r.CodeStationNach) // код из скобок
	assert.Equal(t, "999999", r.CodeStationOper)
	assert.Equal(t, "654321", r.CodeStanNazn)
	assert.Equal(t, "10230304", r.GruzotprOkpo)
	assert.Equal(t, "1126022", r.GruzpolOkpo)
	assert.Equal(t, "161005", r.CodeCargo) // код ЕТСНГ из скобок
	assert.Equal(t, "26", r.CodeOper)      // код операции из скобок
	assert.Equal(t, "1", r.PorozhPriznak)  // порожний → «1»
	assert.Equal(t, "1234-567-8901", r.Index)

	require.NotNil(t, r.Ves)
	assert.InDelta(t, 68.0, *r.Ves, 1e-9)

	require.NotNil(t, r.DateNach)
	assert.Equal(t, "2026-07-01T00:00:00", r.DateNach.String()) // 18:00→+1, только дата
	require.NotNil(t, r.TimeOp)
	assert.Equal(t, "2026-06-30T08:15:00", r.TimeOp.String())

	assert.Equal(t, "12345678/123456/01.07.2026", r.ID) // vagon/code/DD.MM.YYYY

	require.NotNil(t, r.RasstStanNazn)
	assert.Equal(t, 1000, *r.RasstStanNazn)
	require.NotNil(t, r.ProstCh)
	assert.Equal(t, 12, *r.ProstCh) // 2-й элемент «сутки:часы:минуты»
	require.NotNil(t, r.ProstMin)
	assert.Equal(t, 30, *r.ProstMin) // 3-й элемент
	require.NotNil(t, r.NppVag)
	assert.Equal(t, 5, *r.NppVag)

	assert.Equal(t, "000000012345", r.Uno) // паддинг до 12
}

func TestLKParser_IndexBezIndeksa(t *testing.T) {
	// Индекс пустой или менее 3 частей → «Б/И».
	header := []string{"Номер вагона", "Индекс поезда", "Станция отправления"}
	data := [][]string{
		{"1", "", "С (100000)"},        // пусто
		{"2", "123 456", "С (100000)"}, // только 2 части
	}
	recs, err := parser.NewLKParser(parser.DefaultProfile()).
		ParseBytes(buildLKBytes(t, header, data))
	require.NoError(t, err)
	require.Len(t, recs, 2)
	assert.Equal(t, "Б/И", recs[0].Index)
	assert.Equal(t, "Б/И", recs[1].Index)
}

func TestLKParser_CustomProfile_CutoffHour(t *testing.T) {
	// С порогом 20 час 19:30 НЕ переносится на следующие сутки.
	header := []string{"Номер вагона", "Станция отправления", "Дата и время начала рейса"}
	data := [][]string{{"77", "С (222222)", "30.06.2026 19:30"}}

	profile := parser.DefaultProfile()
	profile.DateCutoffHour = 20
	recs, err := parser.NewLKParser(profile).
		ParseBytes(buildLKBytes(t, header, data))
	require.NoError(t, err)
	require.Len(t, recs, 1)
	require.NotNil(t, recs[0].DateNach)
	assert.Equal(t, "2026-06-30T00:00:00", recs[0].DateNach.String()) // без сдвига
	assert.Equal(t, "77/222222/30.06.2026", recs[0].ID)
}

func TestLKParser_NoHeader_EmptyResult(t *testing.T) {
	// Лист без строки заголовка — не ошибка книги, просто нет записей.
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sheet := f.GetSheetName(0)
	require.NoError(t, f.SetSheetRow(sheet, "A1", &[]string{"какой-то текст"}))
	buf, err := f.WriteToBuffer()
	require.NoError(t, err)

	recs, err := parser.NewLKParser(parser.DefaultProfile()).ParseBytes(buf.Bytes())
	require.NoError(t, err)
	assert.Empty(t, recs)
}
