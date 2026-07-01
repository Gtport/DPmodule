package parser_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/parser"
)

func TestJSONParser_FlatArray_KeyMappings(t *testing.T) {
	// NOM_NAK "AB123": A,B — латинские омоглифы → должны стать «АВ».
	// DATE_NACH 19:30 → час ≥ 18 → +1 сутки → 2026-07-01 (только дата).
	// VES_GRZ 68000 (кг) → 68.0 т. INDEX 15 цифр → XXXX-XXX-XXXX.
	raw := []byte(`[{
		"NOM_VAG":"12345678",
		"NOM_NAK":"AB123",
		"INDEX_POEZD":"123456789012345",
		"DATE_NACH":"2026-06-30T19:30:00",
		"DATE_OP":"2026-06-30T08:15:00",
		"STAN_NACH":"123456",
		"STAN_OP":"999999",
		"VES_GRZ":"68000",
		"KOP_VMD":"01",
		"NPP_VAG":"5",
		"RASST_STAN_NAZN":"1000",
		"PROST_CH":"12:30",
		"INV_CLAIM_NUMBER":"GU12-1",
		"CAR_OWNER_NAME":"КФС"
	}]`)

	recs, err := parser.NewJSONParser().ParseBytes(raw)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	r := recs[0]

	assert.Equal(t, "12345678", r.Vagon)
	assert.Equal(t, "АВ123", r.Invoice)       // омоглифы → кириллица
	assert.Equal(t, "1234-789-0123", r.Index) // формат индекса
	assert.Equal(t, "123456", r.CodeStationNach)
	assert.Equal(t, "999999", r.CodeStationOper)
	assert.Equal(t, "01", r.CodeOper)
	assert.Equal(t, "GU12-1", r.Zayavka)
	assert.Equal(t, "КФС", r.CarOwnerName)

	require.NotNil(t, r.Ves)
	assert.InDelta(t, 68.0, *r.Ves, 1e-9) // 68000 кг / 1000

	require.NotNil(t, r.DateNach)
	assert.Equal(t, "2026-07-01T00:00:00", r.DateNach.String()) // 18:00→+1, только дата, без Z
	require.NotNil(t, r.TimeOp)
	assert.Equal(t, "2026-06-30T08:15:00", r.TimeOp.String()) // как есть

	assert.Equal(t, "12345678/123456/01.07.2026", r.ID) // vagon/code/DD.MM.YYYY (дата после сдвига)

	require.NotNil(t, r.NppVag)
	assert.Equal(t, 5, *r.NppVag)
	require.NotNil(t, r.RasstStanNazn)
	assert.Equal(t, 1000, *r.RasstStanNazn)
	require.NotNil(t, r.ProstCh)
	assert.Equal(t, 12, *r.ProstCh) // «12:30» → 12
}

func TestJSONParser_IndexBezIndeksa(t *testing.T) {
	cases := map[string]string{
		`[{"NOM_VAG":"1","INDEX_POEZD":""}]`:      "Б/И", // пусто
		`[{"NOM_VAG":"1","INDEX_POEZD":"123"}]`:   "Б/И", // не 15 цифр
		`[{"NOM_VAG":"1","INDEX_POEZD":"12345"}]`: "Б/И",
	}
	p := parser.NewJSONParser()
	for raw, want := range cases {
		recs, err := p.ParseBytes([]byte(raw))
		require.NoError(t, err, raw)
		require.Len(t, recs, 1, raw)
		assert.Equal(t, want, recs[0].Index, raw)
	}
}

func TestJSONParser_WrapperFormat(t *testing.T) {
	raw := []byte(`{
		"status":"success",
		"data":{"getReferenceSPV4664Response":{"vagons":[
			{"NOM_VAG":"777","STAN_NACH":"100000"}
		]}}
	}`)
	recs, err := parser.NewJSONParser().ParseBytes(raw)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	assert.Equal(t, "777", recs[0].Vagon)
	assert.Equal(t, "100000", recs[0].CodeStationNach)
}

func TestJSONParser_WrapperNonSuccess(t *testing.T) {
	raw := []byte(`{"status":"error","data":{"getReferenceSPV4664Response":{"vagons":[]}}}`)
	_, err := parser.NewJSONParser().ParseBytes(raw)
	assert.Error(t, err)
}
