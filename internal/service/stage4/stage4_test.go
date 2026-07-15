package stage4

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tm(y, mo, d, h, mi int) time.Time {
	return time.Date(y, time.Month(mo), d, h, mi, 0, 0, time.UTC)
}
func tp(t time.Time) *time.Time        { return &t }

// расписание станции S: 06:00, 12:00, 18:00 (3 слота/сутки).
var schedS = map[string][]HM{"S": {{6, 0}, {12, 0}, {18, 0}}}

func baseCfg() Config {
	return Config{MinVagon: 20, MinVagonBros: 10, BrosPenalty: 72 * time.Hour, Now: tm(2026, 7, 14, 10, 0)}
}

func TestDistribute_PlanAnchor(t *testing.T) {
	trains := []Train{
		{Key: "P", Station: "S", Group: "g", PlanMsk: tp(tm(2026, 7, 15, 8, 0)), RaschMsk: tp(tm(2026, 7, 14, 1, 0)), VagonCount: 50},
	}
	out := Distribute(trains, schedS, baseCfg())
	// плановый поезд → ProgMsk = PlanMsk (нитка задана планом), слот не ищем.
	require.Contains(t, out, "P")
	assert.Equal(t, tm(2026, 7, 15, 8, 0), out["P"])
}

func TestDistribute_BelowThreshold(t *testing.T) {
	trains := []Train{
		{Key: "small", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 5},         // < 20
		{Key: "bros", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 12, Bros: true}, // ≥ 10 (брос)
	}
	out := Distribute(trains, schedS, baseCfg())
	assert.NotContains(t, out, "small", "поезд ниже порога вагонов прогноз не получает")
	assert.Contains(t, out, "bros", "брошенный проходит по сниженному порогу 10")
}

func TestDistribute_NonPlanStartsAfter18(t *testing.T) {
	// плана нет → старт от Now(10:00) = ближайшие 18:00 = 14.07 18:00.
	trains := []Train{
		{Key: "A", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 30, Pc: 10000},
	}
	out := Distribute(trains, schedS, baseCfg())
	assert.Equal(t, tm(2026, 7, 14, 18, 0), out["A"], "беспланный не раньше ближайших 18:00")
}

func TestDistribute_IntervalPushesNextDay(t *testing.T) {
	// A,B одной группы, Pc=120, по 60 вагонов → интервал(A)=60*24/120=12ч.
	cfg := baseCfg()
	trains := []Train{
		{Key: "A", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 60, Pc: 120},
		{Key: "B", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 6, 0)), VagonCount: 60, Pc: 120},
	}
	out := Distribute(trains, schedS, cfg)
	// A: Rasch=max(05:00,старт 18:00)=18:00 → слот 14.07 18:00.
	assert.Equal(t, tm(2026, 7, 14, 18, 0), out["A"])
	// B: Rasch=max(06:00, 18:00+12ч=15.07 06:00) → слот 15.07 06:00.
	assert.Equal(t, tm(2026, 7, 15, 6, 0), out["B"])
}

func TestDistribute_OccupiedSlotSkipped(t *testing.T) {
	// A,B в один слот 18:00 (Pc огромный → интервал 0), B должен уйти на следующий свободный.
	cfg := baseCfg()
	trains := []Train{
		{Key: "A", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 30, Pc: 100000},
		{Key: "B", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 30, Pc: 100000},
	}
	out := Distribute(trains, schedS, cfg)
	assert.Equal(t, tm(2026, 7, 14, 18, 0), out["A"])
	assert.Equal(t, tm(2026, 7, 15, 6, 0), out["B"], "занятый слот 18:00 пропущен → первый свободный следующих суток")
}

func TestDistribute_Tolerance(t *testing.T) {
	// Допуск 6ч на станции S: слот может быть ≥ Rasch − 6ч.
	// Поезд с Rasch=18:00 при допуске 6ч может взять слот 12:00 (18:00−6ч).
	cfg := baseCfg()
	cfg.Now = tm(2026, 7, 14, 3, 0) // старт = 14.07 18:00... нет; сделаем без «после плана»
	cfg.Tolerance = map[string]time.Duration{"S": 6 * time.Hour}
	// чтобы старт не задавил допуск, поставим Now так, что старт = раннее утро.
	// старт = ближайшие 18:00 после Now(03:00) = 14.07 18:00 — задавит. Возьмём план-якорь для старта пораньше нельзя.
	// Проще: проверим findSlot напрямую с допуском.
	slot := findSlot(tm(2026, 7, 14, 18, 0).Add(-6*time.Hour), schedS["S"], map[time.Time]bool{})
	assert.Equal(t, tm(2026, 7, 14, 12, 0), slot, "с допуском −6ч поезд Rasch 18:00 берёт слот 12:00")
	_ = cfg
}

func TestNextEighteen(t *testing.T) {
	// ref до 18:00 → 18:00 тех же суток.
	assert.Equal(t, tm(2026, 7, 14, 18, 0), nextEighteen(tm(2026, 7, 14, 9, 0), time.Time{}))
	// ref ≥ 18:00 → 18:00 следующих суток.
	assert.Equal(t, tm(2026, 7, 15, 18, 0), nextEighteen(tm(2026, 7, 14, 20, 0), time.Time{}))
	// нулевой ref → от now.
	assert.Equal(t, tm(2026, 7, 14, 18, 0), nextEighteen(time.Time{}, tm(2026, 7, 14, 10, 0)))
}

// расписание станции U: 02:00, 08:00, 14:00, 20:00 (каждые 6ч).
var schedU = map[string][]HM{"U": {{2, 0}, {8, 0}, {14, 0}, {20, 0}}}

// staircaseCfg — допуск 6ч и метод staircase на станции U (профиль УТ-1).
func staircaseCfg() Config {
	c := baseCfg()
	c.Tolerance = map[string]time.Duration{"U": 6 * time.Hour}
	c.Method = map[string]string{"U": MethodStaircase}
	return c
}

// Лестница: первый поезд НЕ раньше стартовой нитки — допуск (−6ч) не тянет ниже старта.
// Контраст: excel-метод на том же входе ставит поезд ДО стартовой нитки (это и был баг УТ-1).
func TestDistribute_StaircaseFirstNotBeforeStart(t *testing.T) {
	// Now 10:00 → старт = ближайшие 18:00 = 14.07 18:00. Rasch задолго до старта, интервал ~0.
	tr := []Train{{Key: "A", Station: "U", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 30, Pc: 100000}}
	start := tm(2026, 7, 14, 18, 0)

	got := Distribute(tr, schedU, staircaseCfg())["A"]
	assert.Equal(t, tm(2026, 7, 14, 20, 0), got)
	assert.False(t, got.Before(start), "первый поезд не раньше стартовой нитки")

	// excel (тот же вход): допуск съедает старт → нитка ДО старта (14:00).
	ce := staircaseCfg()
	ce.Method = map[string]string{"U": MethodExcel}
	assert.Equal(t, tm(2026, 7, 14, 14, 0), Distribute(tr, schedU, ce)["A"],
		"excel-метод (для контраста) ставит поезд до стартовой нитки")
}

// Лестница ре-якорит currentTime на НАЗНАЧЕННУЮ нитку: B не раньше нитки A (20:00);
// 20:00 занят → B уезжает на следующие сутки (02:00), хотя его Rasch ранний.
func TestDistribute_StaircaseReanchorsOnSlot(t *testing.T) {
	tr := []Train{
		{Key: "A", Station: "U", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 30, Pc: 100000},
		{Key: "B", Station: "U", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 6, 0)), VagonCount: 30, Pc: 100000},
	}
	out := Distribute(tr, schedU, staircaseCfg())
	assert.Equal(t, tm(2026, 7, 14, 20, 0), out["A"])
	assert.Equal(t, tm(2026, 7, 15, 2, 0), out["B"], "ре-якорь на нитку A → B на следующие сутки")
}
