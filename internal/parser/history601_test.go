package parser

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Golden: живой ответ провайдера (вагон 60848066, 30 операций, боевой контракт).
func TestParse601_Golden(t *testing.T) {
	raw, err := os.ReadFile("testdata/history601.json")
	require.NoError(t, err)

	ops, err := Parse601(raw)
	require.NoError(t, err)
	require.Len(t, ops, 30)

	first := ops[0]
	assert.Equal(t, "2", first.KopVmd)
	assert.Equal(t, "2026-06-27T01:20:00", first.DateOp.String())
	assert.Equal(t, "963402", first.StanOp, "ведущие нули кода станции сохранены")
	assert.Equal(t, "913102657985702", first.IndexPoezd)
	assert.Zero(t, first.TripKey, "trip_key проставляет вызывающий")
}

func TestParse601_Errors(t *testing.T) {
	_, err := Parse601([]byte(`{"status":"error","message":"нет данных"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "нет данных")

	_, err = Parse601([]byte(`не json`))
	require.Error(t, err)
}

func TestParse601_NullIndexAndBadTime(t *testing.T) {
	raw := []byte(`{"status":"success","data":{"operations":[
		{"KOP_VMD":" 36 ","DATE_OP":"2026-07-01T05:52:01","STAN_OP":"917207","INDEX_POEZD":"000000000000000"},
		{"KOP_VMD":"2","DATE_OP":"мусор","STAN_OP":"917207","INDEX_POEZD":"917207289967808"}]}}`)
	ops, err := Parse601(raw)
	require.NoError(t, err)
	require.Len(t, ops, 1, "операция с нечитаемым временем пропущена")
	assert.Equal(t, "36", ops[0].KopVmd)
	assert.Equal(t, "", ops[0].IndexPoezd, "нулевой индекс → пусто")
}
