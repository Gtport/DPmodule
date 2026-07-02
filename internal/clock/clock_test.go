package clock_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/clock"
)

// clock.Now() должен отдавать московские настенные часы (UTC+3) как naive-время.
func TestNow_IsMoscowWallClock(t *testing.T) {
	utc := time.Now().UTC()
	got := clock.Now().Time()

	// got — носитель UTC с московскими настенными значениями; разница настенных
	// часов с UTC ≈ 3 часа (Москва без летнего времени). Допуск на секунды теста.
	utcWall := time.Date(utc.Year(), utc.Month(), utc.Day(), utc.Hour(), utc.Minute(), utc.Second(), 0, time.UTC)
	diff := got.Sub(utcWall)
	assert.InDelta(t, (3 * time.Hour).Seconds(), diff.Seconds(), 5,
		"ожидали смещение ~+3ч от UTC, получили %v", diff)
}

// Значение naive: носитель — time.UTC (как у time.Parse без зоны) и в JSON без Z.
func TestNow_NaiveNoZone(t *testing.T) {
	now := clock.Now()
	assert.Equal(t, time.UTC, now.Time().Location(), "носитель должен быть time.UTC (naive)")

	b, err := now.MarshalJSON()
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(b), "Z"), "в JSON не должно быть Z: %s", b)
	assert.False(t, strings.Contains(string(b), "+"), "в JSON не должно быть смещения: %s", b)
}

// SetForTest фиксирует «сейчас» и откатывается.
func TestSetForTest_FreezesAndRestores(t *testing.T) {
	fixed := time.Date(2026, 7, 2, 15, 30, 0, 0, time.UTC)
	restore := clock.SetForTest(fixed)

	assert.Equal(t, "2026-07-02T15:30:00", clock.Now().String())

	restore()
	// после отката — снова реальное «сейчас», не зафиксированное значение.
	assert.NotEqual(t, "2026-07-02T15:30:00", clock.Now().String())
}
