package service_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/service"
)

// stageLK кладёт пустышку-файл ЛК с готовым именем <ОКПО>_<ДДММГГ-ЧЧММ>.xlsx —
// Status читает только имя, содержимое неважно.
func stageLK(t *testing.T, baseDir, name string) {
	t.Helper()
	dir := filepath.Join(baseDir, "lk")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644))
}

func codes(st service.LKStatus, level string) []string {
	var out []string
	for _, i := range st.Issues {
		if i.Level == level {
			out = append(out, i.Code)
		}
	}
	return out
}

// Оба ожидаемых грузополучателя на месте, метки близки → готово, без замечаний.
func TestLKStatus_ReadyBothPresent(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 2, 6, 10, 0, 0, time.UTC))
	defer restore()

	intake, dir := newIntake(t)
	stageLK(t, dir, "10230304_020726-0600.xlsx")
	stageLK(t, dir, "1126022_020726-0605.xlsx")

	st, err := intake.Status()
	require.NoError(t, err)

	assert.True(t, st.Ready)
	assert.Equal(t, "dislocation", st.CoArrivalGroup)
	require.Len(t, st.Files, 2)
	assert.Empty(t, st.Issues)
	// files отсортированы по ОКПО, метаданные обогащены
	assert.Equal(t, "1126022", st.Files[0].Okpo)
	assert.ElementsMatch(t, []string{"ГУТ-2", "УТ-1"}, st.Files[0].Terminals)
	assert.Equal(t, 5, st.Files[0].AgeMinutes) // 06:10 − 06:05
}

// Нет файла одного из ожидаемых грузополучателей → блок missing, не готово.
func TestLKStatus_MissingBlocks(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 2, 6, 10, 0, 0, time.UTC))
	defer restore()

	intake, dir := newIntake(t)
	stageLK(t, dir, "10230304_020726-0600.xlsx") // только Аттис, НМТП нет

	st, err := intake.Status()
	require.NoError(t, err)

	assert.False(t, st.Ready)
	assert.Contains(t, codes(st, service.LKIssueBlock), service.LKCodeMissing)
	// замечание указывает на отсутствующий ОКПО НМТП
	var missingOkpo string
	for _, i := range st.Issues {
		if i.Code == service.LKCodeMissing {
			missingOkpo = i.Okpo
		}
	}
	assert.Equal(t, "1126022", missingOkpo)
}

// Разрыв меток формирования > max_gap_minutes → блок gap (парадокс разных срезов).
func TestLKStatus_GapBlocks(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 2, 7, 0, 0, 0, time.UTC))
	defer restore()

	intake, dir := newIntake(t)
	stageLK(t, dir, "10230304_020726-0600.xlsx")
	stageLK(t, dir, "1126022_020726-0630.xlsx") // +30 мин > 15

	st, err := intake.Status()
	require.NoError(t, err)

	assert.False(t, st.Ready)
	assert.Contains(t, codes(st, service.LKIssueBlock), service.LKCodeGap)
}

// Устаревание — это предупреждение, не блок: набор полон и без разрыва → готово.
func TestLKStatus_StaleWarnsButReady(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 2, 6, 10, 0, 0, time.UTC))
	defer restore()

	intake, dir := newIntake(t)
	stageLK(t, dir, "10230304_020726-0400.xlsx") // возраст ~130 мин > 60
	stageLK(t, dir, "1126022_020726-0405.xlsx")

	st, err := intake.Status()
	require.NoError(t, err)

	assert.True(t, st.Ready) // stale не блокирует
	assert.Contains(t, codes(st, service.LKIssueWarning), service.LKCodeStale)
	assert.Empty(t, codes(st, service.LKIssueBlock))
}

// Пустая папка → нет файлов, оба ожидаемых отсутствуют → не готово.
func TestLKStatus_EmptyNotReady(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 2, 6, 10, 0, 0, time.UTC))
	defer restore()

	intake, _ := newIntake(t)

	st, err := intake.Status()
	require.NoError(t, err)

	assert.False(t, st.Ready)
	assert.Empty(t, st.Files)
	assert.Len(t, codes(st, service.LKIssueBlock), 2) // оба грузополучателя missing
}
