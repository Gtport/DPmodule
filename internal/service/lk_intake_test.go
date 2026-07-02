package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/Gtport/DPmodule/internal/service"
)

// buildLKWorkbook собирает in-memory xlsx в раскладке ЛК: маркер «Личный кабинет»
// в B1, дата формирования в A2 (col-1, row+1 от маркера), ниже — строка заголовка
// с колонкой ОКПО и значением ОКПО в данных.
func buildLKWorkbook(t *testing.T, marker, formation, okpo string) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	sh := f.GetSheetName(0)

	require.NoError(t, f.SetCellValue(sh, "B1", marker))    // маркер (первая непустая)
	require.NoError(t, f.SetCellValue(sh, "A2", formation)) // дата формирования (col-1,row+1)
	require.NoError(t, f.SetCellValue(sh, "B2", "Дислокация вагонов"))
	require.NoError(t, f.SetCellValue(sh, "A4", "Номер вагона")) // строка заголовка
	require.NoError(t, f.SetCellValue(sh, "B4", "Грузополучатель (ОКПО)"))
	require.NoError(t, f.SetCellValue(sh, "A5", "52275476")) // данные
	require.NoError(t, f.SetCellValue(sh, "B5", okpo))

	buf, err := f.WriteToBuffer()
	require.NoError(t, err)
	return buf.Bytes()
}

func newIntake(t *testing.T) (*service.LKIntake, string) {
	t.Helper()
	c := service.NewConfigCache(sampleConfig())
	require.NoError(t, c.Load(context.Background()))
	dir := t.TempDir()
	return service.NewLKIntake(c, dir), dir
}

func TestLKIntake_Store_OK(t *testing.T) {
	intake, dir := newIntake(t)

	res, err := intake.Store("attis.xlsx", buildLKWorkbook(t, "Личный кабинет", "02.07.2026 06:04", "10230304"))
	require.NoError(t, err)

	assert.Equal(t, "AT", res.Port)
	assert.Equal(t, "2026-07-02T06:04:00", res.FormationTS.String()) // московское naive, без Z/сдвига
	assert.Equal(t, "AT_020726-0604.xlsx", res.Filename)
	assert.False(t, res.Replaced)

	// файл действительно лежит в <dir>/lk/
	_, statErr := os.Stat(filepath.Join(dir, "lk", res.Filename))
	require.NoError(t, statErr)
}

func TestLKIntake_Store_Versioning(t *testing.T) {
	intake, dir := newIntake(t)

	_, err := intake.Store("a.xlsx", buildLKWorkbook(t, "Личный кабинет", "02.07.2026 06:00", "10230304"))
	require.NoError(t, err)

	// более старый того же порта → отказ (конфликт версий)
	_, err = intake.Store("a.xlsx", buildLKWorkbook(t, "Личный кабинет", "02.07.2026 05:00", "10230304"))
	require.ErrorIs(t, err, service.ErrOlderThanExisting)

	// более новый того же порта → заменяет старый
	res, err := intake.Store("a.xlsx", buildLKWorkbook(t, "Личный кабинет", "02.07.2026 07:00", "10230304"))
	require.NoError(t, err)
	assert.True(t, res.Replaced)

	// осталась одна актуальная версия порта
	matches, _ := filepath.Glob(filepath.Join(dir, "lk", "AT_*.xlsx"))
	assert.Len(t, matches, 1)
	assert.Equal(t, "AT_020726-0700.xlsx", filepath.Base(matches[0]))
}

func TestLKIntake_Store_NotLK(t *testing.T) {
	intake, _ := newIntake(t)
	_, err := intake.Store("x.xlsx", buildLKWorkbook(t, "Какой-то отчёт", "02.07.2026 06:04", "10230304"))
	require.ErrorIs(t, err, service.ErrNotLK)
}

func TestLKIntake_Store_UnknownOkpo(t *testing.T) {
	intake, _ := newIntake(t)
	_, err := intake.Store("x.xlsx", buildLKWorkbook(t, "Личный кабинет", "02.07.2026 06:04", "99999999"))
	require.ErrorIs(t, err, service.ErrUnknownOkpo)
}

func TestLKIntake_Store_BadExt(t *testing.T) {
	intake, _ := newIntake(t)
	_, err := intake.Store("x.pdf", buildLKWorkbook(t, "Личный кабинет", "02.07.2026 06:04", "10230304"))
	require.ErrorIs(t, err, service.ErrBadExt)
}
