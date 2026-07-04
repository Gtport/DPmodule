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
func intp(v int) *int        { return &v }

var stage1Cfg = service.Stage1Config{CutoffHour: 18, ProstDnMin: 1, ProstChMin: 12}

// fullEnricher — справочники для сквозного Stage 1: станции + порты + операции.
func fullEnricher(t *testing.T) *service.Enricher {
	t.Helper()
	dc := service.NewDirectoryCache(&stubDirRepo{
		stations: []domain.Station{
			{Kod: 985702, Kod4: 9857, Name: "МЫС АСТАФЬЕВА", Road: "ДВС", Latitude: f64(42.8), Longitude: f64(132.9)},
			{Kod: 984700, Kod4: 9847, Name: "НАХОДКА", Road: "ДВС"},
			{Kod: 770005, Kod4: 7700, Name: "УЛАК", Road: "ДВС"},
			{Kod: 985100, Kod4: 9851, Name: "РЫБНИКИ", Road: "ДВС"},
		},
		ports: []domain.Ports{
			{Okpo: 1126022, Location: "МЫС АСТАФЬЕВА", Organisation: `АО "НАХОДКИНСКИЙ МТП"`, NameS: "ГУТ-2", Enabled: true},
			{Okpo: 1126022, Location: "НАХОДКА", Organisation: `АО "НАХОДКИНСКИЙ МТП"`, NameS: "УТ-1", Enabled: true},
			{Okpo: 777, Location: "РЫБНИКИ", Organisation: "ООО Выкл", NameS: "PZ", Enabled: false},
		},
		ops: []domain.CargoOperation{
			{Kod: 1, Oper: "ПРИБЫТИЕ НА СТАНЦИЮ НАЗНАЧЕНИЯ", OperS: "Приб"},
		},
	})
	require.NoError(t, dc.Load(context.Background()))
	return service.NewEnricher(dc)
}

// ─────────────────── Stage 1: станции → идентификация+фильтр → операции ───────────────────

// Полный проход: имена станций, идентификация порта, операция.
func TestStage1_EnrichAndIdentify(t *testing.T) {
	recs := []domain.Dislocation{{
		GruzpolOkpo:     "1126022",
		CodeStationNach: "770005", CodeStanNazn: "985702", CodeStationOper: "984700", CodeOper: "1",
	}}

	kept, st := fullEnricher(t).Stage1(recs, stage1Cfg)

	require.Len(t, kept, 1)
	r := kept[0]
	assert.Equal(t, "УЛАК", r.StationNach)
	assert.Equal(t, "МЫС АСТАФЬЕВА", r.StanNazn)
	assert.Equal(t, "9857", r.Code4StanNazn)
	assert.Equal(t, "НАХОДКА", r.StationOper)
	assert.Equal(t, "ПРИБЫТИЕ НА СТАНЦИЮ НАЗНАЧЕНИЯ", r.Oper)
	assert.Equal(t, "Приб", r.OperS)
	assert.Equal(t, "ГУТ-2", r.GruzpolS) // (1126022 + МЫС АСТАФЬЕВА)
	assert.Equal(t, `АО "НАХОДКИНСКИЙ МТП"`, r.Gruzpol)
	assert.Equal(t, 1, st.Kept)
	assert.Equal(t, 1, st.NaznEnriched)
}

// Координаты станции операции — формат "%f", только если заданы.
func TestStage1_Coords(t *testing.T) {
	kept, _ := fullEnricher(t).Stage1([]domain.Dislocation{
		{GruzpolOkpo: "1126022", CodeStanNazn: "985702", CodeStationOper: "985702"}, // 985702 с координатами
	}, stage1Cfg)
	require.Len(t, kept, 1)
	assert.Equal(t, "42.800000", kept[0].Latitude)
	assert.Equal(t, "132.900000", kept[0].Longitude)

	kept2, _ := fullEnricher(t).Stage1([]domain.Dislocation{
		{GruzpolOkpo: "1126022", CodeStanNazn: "985702", CodeStationOper: "984700"}, // без координат
	}, stage1Cfg)
	require.Len(t, kept2, 1)
	assert.Empty(t, kept2[0].Latitude)
}

// Квирк паритета: ненайденная станция ОТПРАВЛЕНИЯ прерывает обогащение → StanNazn
// пусто → запись не резолвится и ОТБРАСЫВАЕТСЯ фильтром.
func TestStage1_MissingNachAborts(t *testing.T) {
	kept, st := fullEnricher(t).Stage1([]domain.Dislocation{
		{GruzpolOkpo: "1126022", CodeStationNach: "111111", CodeStanNazn: "985702"},
	}, stage1Cfg)

	assert.Empty(t, kept)
	assert.Equal(t, []int{111111}, st.StationsNotFound)
	assert.Equal(t, 1, st.PortUnresolved)
	assert.Equal(t, 0, st.Kept)
}

// Фильтр по включённым портам: один ОКПО разведён по терминалам; не резолвится и
// выключенный порт — выброшены.
func TestStage1_FilterByPort(t *testing.T) {
	recs := []domain.Dislocation{
		{Vagon: "A", GruzpolOkpo: "1126022", CodeStanNazn: "985702"}, // → ГУТ-2 (вкл)
		{Vagon: "B", GruzpolOkpo: "1126022", CodeStanNazn: "984700"}, // → УТ-1 (вкл)
		{Vagon: "C", GruzpolOkpo: "1126022", CodeStanNazn: "770005"}, // УЛАК: порта (1126022,УЛАК) нет → выброс
		{Vagon: "D", GruzpolOkpo: "777", CodeStanNazn: "985100"},     // РЫБНИКИ: порт выключен → выброс
	}

	kept, st := fullEnricher(t).Stage1(recs, stage1Cfg)

	require.Len(t, kept, 2)
	assert.Equal(t, 2, st.Kept)
	assert.Equal(t, 1, st.PortUnresolved) // C
	assert.Equal(t, 1, st.PortDisabled)   // D
	assert.Equal(t, "ГУТ-2", kept[0].GruzpolS)
	assert.Equal(t, "УТ-1", kept[1].GruzpolS)
}

// ─────────────────────────── Stage 1: статусы и производные ───────────────────────────

// statusOf прогоняет запись через Stage1 (добавив резолвящийся порт) и возвращает её.
func statusOf(t *testing.T, r domain.Dislocation) domain.Dislocation {
	t.Helper()
	r.GruzpolOkpo = "1126022"
	if r.StanNazn == "" {
		r.StanNazn = "МЫС АСТАФЬЕВА"
	}
	kept, _ := fullEnricher(t).Stage1([]domain.Dislocation{r}, stage1Cfg)
	require.Len(t, kept, 1)
	return kept[0]
}

func TestStage1_StatusTree(t *testing.T) {
	cases := []struct {
		name string
		rec  domain.Dislocation
		want int
	}{
		{"12 порожний в порту", domain.Dislocation{PorozhPriznak: "1", StationOper: "МЫС АСТАФЬЕВА", StanNazn: "МЫС АСТАФЬЕВА"}, 12},
		{"6 порожний в пути", domain.Dislocation{PorozhPriznak: "1", StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА"}, 6},
		{"6 порожний раньше 0/1", domain.Dislocation{PorozhPriznak: "1", CodeStationNach: "111", CodeStationOper: "111", StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", Index: "Б/И"}, 6},
		{"10 прибыл", domain.Dislocation{StationOper: "МЫС АСТАФЬЕВА", StanNazn: "МЫС АСТАФЬЕВА", DatePrib: lt(2026, 7, 2, 5, 0)}, 10},
		{"9 кандидат", domain.Dislocation{StationOper: "МЫС АСТАФЬЕВА", StanNazn: "МЫС АСТАФЬЕВА"}, 9},
		{"0 ст. отправления Б/И", domain.Dislocation{CodeStationNach: "111", CodeStationOper: "111", Index: "Б/И", StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА"}, 0},
		{"1 ст. отправления с индексом", domain.Dislocation{CodeStationNach: "111", CodeStationOper: "111", Index: "1234", StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА"}, 1},
		{"5 брошен", domain.Dislocation{CodeOper: "92", StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА"}, 5},
		{"4 долгий простой (сутки)", domain.Dislocation{StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА", ProstDn: intp(1)}, 4},
		{"4 долгий простой (часы)", domain.Dislocation{StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА", ProstCh: intp(12)}, 4},
		{"2 в пути", domain.Dislocation{StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА", ProstCh: intp(11)}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := statusOf(t, c.rec)
			require.NotNil(t, r.Status)
			assert.Equal(t, c.want, *r.Status)
		})
	}
}

func lt(y, mo, d, h, mi int) *domain.LocalTime {
	v := domain.LocalTime(time.Date(y, time.Month(mo), d, h, mi, 0, 0, time.UTC))
	return &v
}

// date_op = дата; date_op_jd = +1 сутки при часе ≥ 18.
func TestStage1_Dates(t *testing.T) {
	r := statusOf(t, domain.Dislocation{TimeOp: lt(2026, 7, 2, 19, 30)})
	require.NotNil(t, r.DateOp)
	assert.Equal(t, "2026-07-02T00:00:00", r.DateOp.String())
	require.NotNil(t, r.DateOpJd)
	assert.Equal(t, "2026-07-03T19:30:00", r.DateOpJd.String())

	r2 := statusOf(t, domain.Dislocation{TimeOp: lt(2026, 7, 2, 10, 0)})
	assert.Equal(t, "2026-07-02T10:00:00", r2.DateOpJd.String())
}

// id_disl и ключи агрегации id_status5/id_status4.
func TestStage1_KeysAndIdDisl(t *testing.T) {
	r := statusOf(t, domain.Dislocation{
		CodeOper: "92", Index: "1234", CodeStationOper: "770005", OperS: "Брош",
		TimeOp: lt(2026, 7, 2, 8, 5), StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА",
	})
	assert.Equal(t, 5, *r.Status)
	assert.Equal(t, "1234|770005|2026-07-02 08:05:00", r.IdStatus5)
	assert.Empty(t, r.IdStatus4)
	assert.Equal(t, "1234/770005/Брош/02.07.2026", r.IdDisl)

	r2 := statusOf(t, domain.Dislocation{
		Index: "5678", CodeStationOper: "984700", TimeOp: lt(2026, 7, 2, 9, 0),
		StationOper: "НАХОДКА", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА", ProstDn: intp(2),
	})
	assert.Equal(t, 4, *r2.Status)
	assert.Equal(t, "5678|984700|2026-07-02 09:00:00", r2.IdStatus4)
	assert.Empty(t, r2.IdStatus5)
}

// date_kon: 10 → date_op_jd; 12 → nil; иначе time_op.
func TestStage1_DateKon(t *testing.T) {
	r10 := statusOf(t, domain.Dislocation{StationOper: "МЫС АСТАФЬЕВА", StanNazn: "МЫС АСТАФЬЕВА", DatePrib: lt(2026, 7, 2, 5, 0), TimeOp: lt(2026, 7, 2, 20, 0)})
	require.Equal(t, 10, *r10.Status)
	require.NotNil(t, r10.DateKon)
	assert.Equal(t, r10.DateOpJd.String(), r10.DateKon.String())

	r12 := statusOf(t, domain.Dislocation{PorozhPriznak: "1", StationOper: "МЫС АСТАФЬЕВА", StanNazn: "МЫС АСТАФЬЕВА", TimeOp: lt(2026, 7, 2, 20, 0)})
	require.Equal(t, 12, *r12.Status)
	require.NotNil(t, r12.DateKon)
	assert.Equal(t, "2026-07-02T20:00:00", r12.DateKon.String()) // 12 → time_op (выгружен)

	r2 := statusOf(t, domain.Dislocation{StationOper: "УЛАК", StanNazn: "МЫС АСТАФЬЕВА", StationNach: "СМЫЧКА", TimeOp: lt(2026, 7, 2, 10, 0)})
	require.Equal(t, 2, *r2.Status)
	require.NotNil(t, r2.DateKon)
	assert.Equal(t, "2026-07-02T10:00:00", r2.DateKon.String())
}

// delay: норматив доставки в прошлом → просрочка в сутках (по «сейчас» МСК).
func TestStage1_Delay(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	defer restore()

	past := statusOf(t, domain.Dislocation{DateDostav: lt(2026, 7, 7, 0, 0)})
	require.NotNil(t, past.Delay)
	assert.Equal(t, 3, *past.Delay)

	future := statusOf(t, domain.Dislocation{DateDostav: lt(2026, 7, 15, 0, 0)})
	assert.Nil(t, future.Delay)
}
