package report_test

// Тест «Перестановки на терминал»: отбор (только переставляемые на цель, не
// прибывшие), группировка листа «Поезда» с составом, повагонная раскладка
// листа «Вагоны», пустой результат — ошибка.

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/report"
)

// perTestRecord — вагон, переставляемый АЭ → ГУТ-2 (база — запись повагонки).
func perTestRecord(vagon string, npp int) domain.Dislocation {
	d := vagTestRecord() // GruzpolS="АЭ", Naznach="ГУТ-2", Index="12340-111-98050"
	d.Vagon = vagon
	d.NppVag = &npp
	return d
}

func TestPerestanovkaXLSX(t *testing.T) {
	a := perTestRecord("60000002", 2)
	b := perTestRecord("60000001", 1)

	other := perTestRecord("61000001", 1) // другой поезд, другая подгруппа состава
	other.Index, other.IndexMain = "55550-222-98050", "55550-999-98050"
	other.GruzpolS = "УТ-1"

	arrived := perTestRecord("62000001", 3) // статус ≥ 10 — выпадает
	ten := 10
	arrived.Status = &ten

	noSwap := perTestRecord("63000001", 4) // получатель = назначение — не перестановка
	noSwap.GruzpolS = "ГУТ-2"

	foreign := perTestRecord("64000001", 5) // едет на другой терминал
	foreign.Naznach = "АЭ"
	foreign.GruzpolS = "ГУТ-2"

	records := []domain.Dislocation{a, b, other, arrived, noSwap, foreign}
	data, name, err := report.PerestanovkaXLSX(records, "ГУТ-2")
	require.NoError(t, err)
	assert.Contains(t, name, "Перестановка на ГУТ-2")

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// Лист «Поезда»: два поезда, состав с числом вагонов.
	trains, err := f.GetRows("Поезда")
	require.NoError(t, err)
	require.Len(t, trains, 3) // шапка + 2 поезда
	assert.Equal(t, []string{
		"Индекс", "Станция операции", "Дорога операции", "Операция",
		"План", "Прогноз", "Задержка", "Состав",
	}, trains[0])
	assert.Equal(t, "12340-111-98050", trains[1][0])
	assert.Equal(t, "12340-999-98050 | ЕРУНАКОВО АЭ → ГУТ-2 (2)", trains[1][7])
	assert.Equal(t, "2 дн", trains[1][6]) // prost_dn=2 из базовой записи
	assert.Equal(t, "55550-222-98050", trains[2][0])
	assert.Equal(t, "55550-999-98050 | ЕРУНАКОВО УТ-1 → ГУТ-2 (1)", trains[2][7])

	// Лист «Вагоны»: три строки в порядке индекс + номер в поезде.
	vagons, err := f.GetRows("Вагоны")
	require.NoError(t, err)
	require.Len(t, vagons, 4) // шапка + 3 вагона
	assert.Equal(t, []string{
		"№", "Вагон", "Накладная", "Индекс", "Род. индекс", "Станция погрузки",
		"Отправитель", "Груз", "Вес", "Дата погрузки", "Срок доставки",
		"Получатель", "Назначение", "Станция операции", "Дорога операции",
		"Операция", "План", "Прогноз", "Задержка", "Собственник",
	}, vagons[0])
	assert.Equal(t, "60000001", vagons[1][1]) // npp 1 раньше npp 2
	assert.Equal(t, "60000002", vagons[2][1])
	assert.Equal(t, "61000001", vagons[3][1])

	byHeader := map[string]string{}
	for i, h := range vagons[0] {
		if i < len(vagons[1]) {
			byHeader[h] = vagons[1][i]
		}
	}
	assert.Equal(t, "АЭ", byHeader["Получатель"])
	assert.Equal(t, "ГУТ-2", byHeader["Назначение"])
	assert.Equal(t, "01.07.2026", byHeader["Дата погрузки"])
	assert.Equal(t, "30.07.2026 18:30", byHeader["План"])
	assert.Equal(t, "СОБСТВЕННИК ООО", byHeader["Собственник"]) // owner, не rod_vag_uch
}

func TestPerestanovkaXLSX_Empty(t *testing.T) {
	_, _, err := report.PerestanovkaXLSX([]domain.Dislocation{vagTestRecord()}, "АЭ")
	assert.ErrorIs(t, err, report.ErrPerestanovkaEmpty)
}
