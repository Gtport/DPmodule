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

// Книга «Просрочка доставки»: лист «Накладные» — агрегаты по накладной
// (min даты, max прибытие/просрочка) + пустые графы платы и пеней; лист
// «Вагоны» — повагонная фактура. Строки приходят отсортированными по накладной.
func TestOverdueClaimXLSX(t *testing.T) {
	lt := func(y int, m time.Month, d int) *domain.LocalTime {
		v := domain.LocalTime(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
		return &v
	}
	di := func(v int) *int { return &v }
	ves := 69.5
	rows := []domain.VagonHistory{
		{Vagon: "62158654", Invoice: "ЭЛ111", StationNach: "ЕРУНАКОВО", Gruzotpr: "ОТПР",
			StanNazn: "НАХОДКА", GruzpolS: "АЭ", CargoS: "УГОЛЬ Г", Ves: &ves,
			DateNachD: lt(2026, 7, 1), DateDostav: lt(2026, 7, 20),
			DatePribD: lt(2026, 7, 25), Delay: di(5)},
		{Vagon: "62158655", Invoice: "ЭЛ111", StationNach: "ЕРУНАКОВО", Gruzotpr: "ОТПР",
			StanNazn: "НАХОДКА", GruzpolS: "АЭ", CargoS: "УГОЛЬ Г", Ves: &ves,
			DateNachD: lt(2026, 6, 30), DateDostav: lt(2026, 7, 19),
			DatePribD: lt(2026, 7, 26), Delay: di(7)},
		{Vagon: "70000001", Invoice: "ЭЛ222", StationNach: "ЧЕЛУТАЙ",
			DateNachD: lt(2026, 7, 10), DateDostav: lt(2026, 7, 28),
			DatePribD: lt(2026, 7, 30), Delay: di(2)},
		// Без накладной вовсе — маркер.
		{Vagon: "70000002", DatePribD: lt(2026, 7, 30), Delay: di(1)},
	}

	data, err := OverdueClaimXLSX(func(fn func(domain.VagonHistory) error) error {
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

	inv, err := f.GetRows("Накладные")
	require.NoError(t, err)
	require.Len(t, inv, 4) // шапка + 3 накладных

	assert.Equal(t, "Накладная", inv[0][0])
	assert.Equal(t, "Провозная плата, руб.", inv[0][11])
	assert.Equal(t, "Пени, руб. (6%/сут, не более 50%)", inv[0][12])

	// Агрегаты ЭЛ111: 2 вагона, min даты, max прибытие и просрочка.
	assert.Equal(t, "ЭЛ111", inv[1][0])
	assert.Equal(t, "2", inv[1][1])
	assert.Equal(t, "ЕРУНАКОВО", inv[1][2])
	assert.Equal(t, "30.06.2026", inv[1][7])
	assert.Equal(t, "19.07.2026", inv[1][8])
	assert.Equal(t, "26.07.2026", inv[1][9])
	assert.Equal(t, "7", inv[1][10])
	// Графы платы и пеней пустые (GetRows обрезает пустой хвост строки).
	assert.LessOrEqual(t, len(inv[1]), 11)

	assert.Equal(t, "ЭЛ222", inv[2][0])
	assert.Equal(t, "(без накладной)", inv[3][0])

	vag, err := f.GetRows("Вагоны")
	require.NoError(t, err)
	require.Len(t, vag, 5) // шапка + 4 вагона
	assert.Equal(t, "Накладная", vag[0][0])
	assert.Equal(t, "Вагон", vag[0][1])
	assert.Equal(t, "ЭЛ111", vag[1][0])
	assert.Equal(t, "62158654", vag[1][1])
	assert.Equal(t, "69.50", vag[1][7])
	assert.Equal(t, "5", vag[1][11])
	assert.Equal(t, "(без накладной)", vag[4][0])
}
