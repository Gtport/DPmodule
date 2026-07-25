package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mskDailyOffset: «HH:MM» МСК → offset от UTC-полуночи (МСК = UTC+3).
func TestMskDailyOffset(t *testing.T) {
	cases := map[string]time.Duration{
		"01:00": 22 * time.Hour,                // 22:00 UTC
		"18:00": 15 * time.Hour,                // 15:00 UTC
		"03:00": 0,                             // 00:00 UTC
		"00:30": 21*time.Hour + 30*time.Minute, // 21:30 UTC
		"23:59": 20*time.Hour + 59*time.Minute, // 20:59 UTC
	}
	for in, want := range cases {
		got, err := mskDailyOffset(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
		assert.GreaterOrEqual(t, got, time.Duration(0))
		assert.Less(t, got, 24*time.Hour)
	}
}

func TestMskDailyOffset_Invalid(t *testing.T) {
	_, err := mskDailyOffset("25:00")
	assert.Error(t, err)
	_, err = mskDailyOffset("abc")
	assert.Error(t, err)
}
