package report

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Smoke-тест книги «История вагонов»: колонки в порядке gtport, правило статуса
// «выгружен» поверх кода, даты дд.ММ.гггг, книга открывается excelize'ом.
func TestHistoryXLSX(t *testing.T) {
	lt := func(y int, m time.Month, d int) *domain.LocalTime {
		v := domain.LocalTime(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
		return &v
	}
	ves := 69.5
	status := 2
	rows := []domain.VagonHistory{
		{ID: "a", Vagon: "62158654", Invoice: "ЭЛ123456", StationNach: "ЕРУНАКОВО",
			GruzpolS: "АЭ", Naznach: "АЭ", PlaceVigr: "АЭ", Ves: &ves, Status: &status,
			DateNachD: lt(2026, 7, 1), DateVigr: lt(2026, 7, 9),
			Owner: "СОБСТВЕННИК", FreightExactName: "МАРКА-Г", Peregruz: "11111111"},
		{ID: "b", Vagon: "62158655", Status: &status},
	}

	data, err := HistoryXLSX(func(fn func(domain.VagonHistory) error) error {
		for _, r := range rows {
			if err := fn(r); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	got, err := f.GetRows("История вагонов")
	require.NoError(t, err)
	require.Len(t, got, 3)

	assert.Equal(t, "Вагон", got[0][0])
	assert.Equal(t, "Перегруз", got[0][25], "последняя колонка — «Перегруз», не «Отгружен» gtport")

	assert.Equal(t, "62158654", got[1][0])
	assert.Equal(t, "01.07.2026", got[1][5], "дата погрузки дд.ММ.гггг")
	assert.Equal(t, "выгружен", got[1][13], "место+дата выгрузки сильнее кода статуса")
	assert.Equal(t, "69.50", got[1][11], "вес числом с форматом 0.00")

	assert.Equal(t, "в пути", got[2][13], "без выгрузки — подпись кода статуса")
}
