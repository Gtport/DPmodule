package service_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

func f64(v float64) *float64 { return &v }

func enricherWith(t *testing.T) *service.Enricher {
	t.Helper()
	dc := service.NewDirectoryCache(&stubDirRepo{
		stations: []domain.Station{
			{Kod: 985702, Kod4: 9857, Name: "МЫС АСТАФЬЕВА", Road: "ДВС", Latitude: f64(42.8), Longitude: f64(132.9)},
			{Kod: 984700, Kod4: 9847, Name: "НАХОДКА", Road: "ДВС"},
			{Kod: 770005, Kod4: 7700, Name: "УЛАК", Road: "ДВС"},
		},
		ops: []domain.CargoOperation{
			{Kod: 1, Oper: "ПРИБЫТИЕ НА СТАНЦИЮ НАЗНАЧЕНИЯ", OperS: "Приб"},
		},
	})
	require.NoError(t, dc.Load(context.Background()))
	return service.NewEnricher(dc)
}

// Полное обогащение: все три станции известны + операция.
func TestEnrichStage1_Full(t *testing.T) {
	e := enricherWith(t)
	recs := []domain.Dislocation{{
		CodeStationNach: "770005", CodeStanNazn: "985702", CodeStationOper: "984700", CodeOper: "1",
	}}

	st := e.Stage1(recs)
	r := recs[0]

	assert.Equal(t, "УЛАК", r.StationNach)
	assert.Equal(t, "ДВС", r.DorogaNach)
	assert.Equal(t, "МЫС АСТАФЬЕВА", r.StanNazn)
	assert.Equal(t, "9857", r.Code4StanNazn)
	assert.Equal(t, "НАХОДКА", r.StationOper)
	assert.Equal(t, "ПРИБЫТИЕ НА СТАНЦИЮ НАЗНАЧЕНИЯ", r.Oper)
	assert.Equal(t, "Приб", r.OperS)
	assert.Equal(t, 1, st.NaznEnriched)
	assert.Empty(t, st.StationsNotFound)
	assert.Empty(t, st.OperationsNotFound)
}

// Координаты станции операции — формат "%f" (6 знаков), только если заданы.
func TestEnrichStage1_Coords(t *testing.T) {
	e := enricherWith(t)
	recs := []domain.Dislocation{{CodeStationOper: "985702"}} // с координатами
	e.Stage1(recs)
	assert.Equal(t, "42.800000", recs[0].Latitude)
	assert.Equal(t, "132.900000", recs[0].Longitude)

	recs2 := []domain.Dislocation{{CodeStationOper: "984700"}} // без координат
	e.Stage1(recs2)
	assert.Empty(t, recs2[0].Latitude)
	assert.Empty(t, recs2[0].Longitude)
}

// Квирк паритета: ненайденная станция ОТПРАВЛЕНИЯ прерывает обогащение записи —
// назначение НЕ заполняется, хотя его код известен.
func TestEnrichStage1_MissingNachAborts(t *testing.T) {
	e := enricherWith(t)
	recs := []domain.Dislocation{{
		CodeStationNach: "111111", // нет в справочнике
		CodeStanNazn:    "985702", // известен, но не должен примениться
	}}

	st := e.Stage1(recs)

	assert.Empty(t, recs[0].StationNach)
	assert.Empty(t, recs[0].StanNazn) // прерывание сработало
	assert.Equal(t, []int{111111}, st.StationsNotFound)
	assert.Equal(t, 0, st.NaznEnriched)
}

// Ненайденная операция попадает в диагностику, имя не заполняется.
func TestEnrichStage1_OperationNotFound(t *testing.T) {
	e := enricherWith(t)
	recs := []domain.Dislocation{{CodeOper: "999"}}

	st := e.Stage1(recs)

	assert.Empty(t, recs[0].Oper)
	assert.Equal(t, []int{999}, st.OperationsNotFound)
}
