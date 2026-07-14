package service

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

func TestCheckFreshness(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	pol := domain.CategoryPolicy{MaxStalenessMinutes: 60, RejectOlderThanCurrent: true, RejectOlderRoleExempt: "administrator"}
	lt := func(m int) *domain.LocalTime { v := domain.LocalTime(now.Add(time.Duration(m) * time.Minute)); return &v }

	t.Run("нет метки — пропускаем", func(t *testing.T) {
		assert.NoError(t, checkFreshness(time.Time{}, now, pol, nil, false))
	})
	t.Run("свежие и новее текущей — ок", func(t *testing.T) {
		assert.NoError(t, checkFreshness(now.Add(-10*time.Minute), now, pol, lt(-30), false))
	})
	t.Run("старше 60 мин — ErrDislTooStale", func(t *testing.T) {
		err := checkFreshness(now.Add(-90*time.Minute), now, pol, nil, false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDislTooStale))
	})
	t.Run("старше текущей дислокации — ErrDislOlderThanCurrent", func(t *testing.T) {
		// файлы now-30 (проходят свежесть), текущая now-10 (новее) → откат
		err := checkFreshness(now.Add(-30*time.Minute), now, pol, lt(-10), false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDislOlderThanCurrent))
	})
	t.Run("старше текущей, но роль-исключение — ок", func(t *testing.T) {
		assert.NoError(t, checkFreshness(now.Add(-30*time.Minute), now, pol, lt(-10), true))
	})
	t.Run("reject_older выключен — старее текущей разрешено", func(t *testing.T) {
		p := pol
		p.RejectOlderThanCurrent = false
		assert.NoError(t, checkFreshness(now.Add(-30*time.Minute), now, p, lt(-10), false))
	})
	t.Run("порог свежести 0 — гард выключен", func(t *testing.T) {
		p := pol
		p.MaxStalenessMinutes = 0
		p.RejectOlderThanCurrent = false
		assert.NoError(t, checkFreshness(now.Add(-500*time.Minute), now, p, nil, false))
	})
}

func TestOldestFormation(t *testing.T) {
	mk := func(y, mo, d, h, mi int) domain.LocalTime {
		return domain.LocalTime(time.Date(y, time.Month(mo), d, h, mi, 0, 0, time.UTC))
	}
	files := []LKFileInfo{
		{FormationTS: mk(2026, 7, 15, 10, 30)},
		{FormationTS: mk(2026, 7, 15, 9, 15)}, // самая старая
		{FormationTS: mk(2026, 7, 15, 11, 0)},
		{}, // нулевая — пропускаем
	}
	assert.Equal(t, time.Date(2026, 7, 15, 9, 15, 0, 0, time.UTC), oldestFormation(files))
	assert.True(t, oldestFormation(nil).IsZero())
}
