package domain_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
)

func TestLocalTime_MarshalNoZ(t *testing.T) {
	lt := domain.LocalTime(time.Date(2026, 7, 1, 15, 4, 5, 0, time.UTC))
	b, err := json.Marshal(lt)
	require.NoError(t, err)
	assert.Equal(t, `"2026-07-01T15:04:05"`, string(b)) // без суффикса Z
}

func TestLocalTime_ZeroMarshalsNull(t *testing.T) {
	var lt domain.LocalTime
	b, err := json.Marshal(lt)
	require.NoError(t, err)
	assert.Equal(t, "null", string(b))
}

func TestLocalTime_UnmarshalVariants(t *testing.T) {
	cases := map[string]string{
		`"2026-07-01T15:04:05"`:  "2026-07-01T15:04:05", // без Z
		`"2026-07-01T15:04:05Z"`: "2026-07-01T15:04:05", // Z принимаем, храним без зоны
		`"2026-07-01 15:04:05"`:  "2026-07-01T15:04:05", // пробел вместо T
		`"2026-07-01"`:           "2026-07-01T00:00:00", // только дата
	}
	for in, want := range cases {
		var lt domain.LocalTime
		require.NoError(t, json.Unmarshal([]byte(in), &lt), in)
		assert.Equal(t, want, lt.String(), in)
	}
}

func TestLocalTime_NullUnmarshalsZero(t *testing.T) {
	lt := domain.LocalTime(time.Now())
	require.NoError(t, json.Unmarshal([]byte("null"), &lt))
	assert.True(t, lt.IsZero())
}

func TestLocalTime_DBValueAndScan(t *testing.T) {
	// Value: нулевое → NULL, ненулевое → time.Time
	var zero domain.LocalTime
	v, err := zero.Value()
	require.NoError(t, err)
	assert.Nil(t, v)

	tt := time.Date(2026, 7, 1, 8, 30, 0, 0, time.UTC)
	v, err = domain.LocalTime(tt).Value()
	require.NoError(t, err)
	assert.Equal(t, tt, v)

	// Scan: time.Time / nil / string
	var lt domain.LocalTime
	require.NoError(t, lt.Scan(tt))
	assert.Equal(t, "2026-07-01T08:30:00", lt.String())

	require.NoError(t, lt.Scan(nil))
	assert.True(t, lt.IsZero())

	require.NoError(t, lt.Scan("2026-07-01T09:00:00"))
	assert.Equal(t, "2026-07-01T09:00:00", lt.String())
}

// Round-trip самой Dislocation: время без Z, nil-время → null, alternative_move на месте.
func TestDislocation_JSONRoundTrip(t *testing.T) {
	d := domain.Dislocation{
		Vagon:           "12345678",
		AlternativeMove: 0,
		DateNach:        domain.NewLocalTime(time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)),
	}
	b, err := json.Marshal(d)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"date_nach":"2026-06-30T12:00:00"`) // без Z
	assert.Contains(t, string(b), `"alternative_move":0`)
	assert.Contains(t, string(b), `"time_op":null`) // nil *LocalTime → null

	var back domain.Dislocation
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, "12345678", back.Vagon)
	require.NotNil(t, back.DateNach)
	assert.Equal(t, "2026-06-30T12:00:00", back.DateNach.String())
}
