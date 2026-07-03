package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Gtport/DPmodule/internal/clock"
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

// ─────────────────────────── Stage 1b: статусы ───────────────────────────

var stage1bCfg = service.Stage1bConfig{CutoffHour: 18, ProstDnMin: 1, ProstChMin: 12}

func lt(y, mo, d, h, mi int) *domain.LocalTime {
	v := domain.LocalTime(time.Date(y, time.Month(mo), d, h, mi, 0, 0, time.UTC))
	return &v
}

// statusOf прогоняет одну запись через Stage1b и возвращает статус.
func statusOf(t *testing.T, r domain.Dislocation) domain.Dislocation {
	t.Helper()
	recs := []domain.Dislocation{r}
	enricherWith(t).Stage1b(recs, stage1bCfg)
	return recs[0]
}

func TestStage1b_StatusTree(t *testing.T) {
	cases := []struct {
		name string
		rec  domain.Dislocation
		want int
	}{
		{"12 порожний в порту", domain.Dislocation{PorozhPriznak: "1", StationOper: "МЫС АСТАФЬЕВА", StanNazn: "МЫС АСТАФЬЕВА"}, 12},
		{"6 порожний в пути", domain.Dislocation{PorozhPriznak: "1", StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА"}, 6},
		{"6 порожний раньше 0/1 (на ст. отправления)", domain.Dislocation{PorozhPriznak: "1", CodeStationNach: "111", CodeStationOper: "111", StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", Index: "Б/И"}, 6},
		{"10 прибыл (date_prib есть)", domain.Dislocation{StationOper: "МЫС АСТАФЬЕВА", StanNazn: "МЫС АСТАФЬЕВА", DatePrib: lt(2026, 7, 2, 5, 0)}, 10},
		{"9 кандидат (date_prib пусто)", domain.Dislocation{StationOper: "МЫС АСТАФЬЕВА", StanNazn: "МЫС АСТАФЬЕВА"}, 9},
		{"0 на ст. отправления Б/И", domain.Dislocation{CodeStationNach: "111", CodeStationOper: "111", Index: "Б/И", StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА"}, 0},
		{"1 на ст. отправления с индексом", domain.Dislocation{CodeStationNach: "111", CodeStationOper: "111", Index: "1234", StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА"}, 1},
		{"5 брошен", domain.Dislocation{CodeOper: "92", StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА"}, 5},
		{"4 долгий простой (сутки)", domain.Dislocation{StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА", ProstDn: intp(1)}, 4},
		{"4 долгий простой (часы)", domain.Dislocation{StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА", ProstCh: intp(12)}, 4},
		{"2 в пути (простой ниже порога)", domain.Dislocation{StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА", ProstCh: intp(11)}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := statusOf(t, c.rec)
			require.NotNil(t, r.Status)
			assert.Equal(t, c.want, *r.Status)
		})
	}
}

func intp(v int) *int { return &v }

// Производные поля даты: date_op = дата; date_op_jd = +1 сутки при часе ≥ 18.
func TestStage1b_Dates(t *testing.T) {
	r := statusOf(t, domain.Dislocation{TimeOp: lt(2026, 7, 2, 19, 30)}) // час 19 ≥ 18
	require.NotNil(t, r.DateOp)
	assert.Equal(t, "2026-07-02T00:00:00", r.DateOp.String())
	require.NotNil(t, r.DateOpJd)
	assert.Equal(t, "2026-07-03T19:30:00", r.DateOpJd.String()) // +1 сутки

	r2 := statusOf(t, domain.Dislocation{TimeOp: lt(2026, 7, 2, 10, 0)}) // час 10 < 18
	assert.Equal(t, "2026-07-02T10:00:00", r2.DateOpJd.String())         // без сдвига
}

// id_disl и ключи агрегации id_status5/id_status4.
func TestStage1b_KeysAndIdDisl(t *testing.T) {
	// брошен (5) → id_status5 = index|code_station_oper|time_op
	r := statusOf(t, domain.Dislocation{
		CodeOper: "92", Index: "1234", CodeStationOper: "770005", OperS: "Брош",
		TimeOp: lt(2026, 7, 2, 8, 5), StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА",
	})
	assert.Equal(t, 5, *r.Status)
	assert.Equal(t, "1234|770005|2026-07-02 08:05:00", r.IdStatus5)
	assert.Empty(t, r.IdStatus4)
	assert.Equal(t, "1234/770005/Брош/02.07.2026", r.IdDisl)

	// долгий простой (4) → id_status4 той же формулой
	r2 := statusOf(t, domain.Dislocation{
		Index: "5678", CodeStationOper: "984700", TimeOp: lt(2026, 7, 2, 9, 0),
		StationOper: "НАХОДКА", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА", ProstDn: intp(2),
	})
	assert.Equal(t, 4, *r2.Status)
	assert.Equal(t, "5678|984700|2026-07-02 09:00:00", r2.IdStatus4)
	assert.Empty(t, r2.IdStatus5)
}

// date_kon: 10 → date_op_jd; 12 → nil; иначе time_op.
func TestStage1b_DateKon(t *testing.T) {
	r10 := statusOf(t, domain.Dislocation{StationOper: "МЫС АСТАФЬЕВА", StanNazn: "МЫС АСТАФЬЕВА", DatePrib: lt(2026, 7, 2, 5, 0), TimeOp: lt(2026, 7, 2, 20, 0)})
	require.Equal(t, 10, *r10.Status)
	require.NotNil(t, r10.DateKon)
	assert.Equal(t, r10.DateOpJd.String(), r10.DateKon.String()) // = date_op_jd

	r12 := statusOf(t, domain.Dislocation{PorozhPriznak: "1", StationOper: "МЫС АСТАФЬЕВА", StanNazn: "МЫС АСТАФЬЕВА", TimeOp: lt(2026, 7, 2, 20, 0)})
	require.Equal(t, 12, *r12.Status)
	assert.Nil(t, r12.DateKon) // порожний в порту — nil

	r2 := statusOf(t, domain.Dislocation{StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА", TimeOp: lt(2026, 7, 2, 10, 0)})
	require.Equal(t, 2, *r2.Status)
	require.NotNil(t, r2.DateKon)
	assert.Equal(t, "2026-07-02T10:00:00", r2.DateKon.String()) // = time_op
}

// delay: норматив доставки в прошлом → просрочка в сутках (по «сейчас» МСК).
func TestStage1b_Delay(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	defer restore()

	past := statusOf(t, domain.Dislocation{DateDostav: lt(2026, 7, 7, 0, 0)}) // 3 суток назад
	require.NotNil(t, past.Delay)
	assert.Equal(t, 3, *past.Delay)

	future := statusOf(t, domain.Dislocation{DateDostav: lt(2026, 7, 15, 0, 0)})
	assert.Nil(t, future.Delay)
}
