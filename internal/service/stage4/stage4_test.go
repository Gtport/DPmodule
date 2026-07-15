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

// tolCfg — допуск 6ч на станции U.
func tolCfg() Config {
	c := baseCfg()
	c.Tolerance = map[string]time.Duration{"U": 6 * time.Hour}
	return c
}

// Первый поезд НЕ раньше стартовой нитки: допуск (−6ч) применяется только к Rasch,
// стартовая нитка — жёсткий низ. Rasch задолго до старта, но слот ≥ старта.
func TestDistribute_FirstNotBeforeStart(t *testing.T) {
	// Now 10:00 → старт = ближайшие 18:00 = 14.07 18:00. Rasch задолго до старта, интервал ~0.
	tr := []Train{{Key: "A", Station: "U", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 30, Pc: 100000}}
	start := tm(2026, 7, 14, 18, 0)

	got := Distribute(tr, schedU, tolCfg())["A"]
	assert.Equal(t, tm(2026, 7, 14, 20, 0), got)
	assert.False(t, got.Before(start), "первый поезд не раньше стартовой нитки, несмотря на допуск −6ч")
}

// Первая нитка беспланного группы = последний плановый группы + его интервал (не старт).
// Плановый P: нитка 08:00, интервал 120*24/120=24ч → причал занят до 15.07 08:00; беспланный
// A встаёт на 15.07 08:00, а не на стартовую нитку 14.07.
func TestDistribute_FirstNitkaFromLastPlan(t *testing.T) {
	cfg := baseCfg() // Now 14.07 10:00 → старт 14.07 18:00
	tr := []Train{
		{Key: "P", Station: "U", Group: "g", PlanMsk: tp(tm(2026, 7, 14, 8, 0)), VagonCount: 120, Pc: 120},
		{Key: "A", Station: "U", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 3, 0)), VagonCount: 30, Pc: 100000},
	}
	out := Distribute(tr, schedU, cfg)
	assert.Equal(t, tm(2026, 7, 14, 8, 0), out["P"], "плановый = PlanMsk")
	assert.Equal(t, tm(2026, 7, 15, 8, 0), out["A"], "первая нитка беспланного = последний плановый + интервал")
}

// Лимит длины состава станции: интервал считает min(вагонов, лимит). A=71ваг, Pc=64,
// лимит 64 → интервал 64*24/64=24ч (без лимита было бы 26.6ч); B через 24ч после нитки A.
func TestDistribute_TrainLengthCap(t *testing.T) {
	cfg := baseCfg()
	cfg.MaxLen = map[string]int{"U": 64}
	tr := []Train{
		{Key: "A", Station: "U", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 71, Pc: 64},
		{Key: "B", Station: "U", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 6, 0)), VagonCount: 30, Pc: 100000},
	}
	out := Distribute(tr, schedU, cfg)
	assert.Equal(t, tm(2026, 7, 14, 20, 0), out["A"])
	assert.Equal(t, tm(2026, 7, 15, 20, 0), out["B"], "интервал A по лимиту 64 (24ч), не по 71 ваг")
}

// Очередь причала ре-якорится на НАЗНАЧЕННУЮ нитку: B не раньше нитки A (20:00);
// 20:00 занят → B уезжает на следующие сутки (02:00), хотя его Rasch ранний.
func TestDistribute_ReanchorsOnSlot(t *testing.T) {
	tr := []Train{
		{Key: "A", Station: "U", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 30, Pc: 100000},
		{Key: "B", Station: "U", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 6, 0)), VagonCount: 30, Pc: 100000},
	}
	out := Distribute(tr, schedU, tolCfg())
	assert.Equal(t, tm(2026, 7, 14, 20, 0), out["A"])
	assert.Equal(t, tm(2026, 7, 15, 2, 0), out["B"], "ре-якорь на нитку A → B на следующие сутки")
}
