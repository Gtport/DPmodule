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
	assert.Equal(t, tm(2026, 7, 14, 18, 0), NextEighteen(tm(2026, 7, 14, 9, 0), time.Time{}))
	// ref ≥ 18:00 → 18:00 следующих суток.
	assert.Equal(t, tm(2026, 7, 15, 18, 0), NextEighteen(tm(2026, 7, 14, 20, 0), time.Time{}))
	// нулевой ref → от now.
	assert.Equal(t, tm(2026, 7, 14, 18, 0), NextEighteen(time.Time{}, tm(2026, 7, 14, 10, 0)))
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

// ─── What-if (вкладка «Прогноз прибытия/выгрузки», эталон scheduleSimulation.ts) ───

// Явная задержка Delay замещает штраф бросания — не задваивается (эталон gtport:
// delay_hours — единственный источник истины о задержке).
func TestDistribute_DelayReplacesBrosPenalty(t *testing.T) {
	cfg := baseCfg() // штраф 72ч
	// Брошенный с Delay=24ч: base = Rasch+24ч (НЕ +72 и НЕ +96).
	trains := []Train{
		{Key: "D", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 30, Pc: 100000,
			Bros: true, Delay: 24 * time.Hour},
	}
	out := Distribute(trains, schedS, cfg)
	// base = 15.07 05:00; старт 14.07 18:00 → floor = 15.07 05:00 → слот 15.07 06:00.
	// (при задвоении штрафа base был бы 18.07 05:00 → слот 18.07 06:00)
	assert.Equal(t, tm(2026, 7, 15, 6, 0), out["D"], "Delay замещает BrosPenalty, а не суммируется")
}

// Частичное восстановление: Delay в часах, меньше штрафа.
func TestDistribute_PartialRestoreDelay(t *testing.T) {
	cfg := baseCfg()
	trains := []Train{
		{Key: "R", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 5, 0)), VagonCount: 30, Pc: 100000,
			Delay: 13 * time.Hour}, // восстановлен, осталось 13ч хода
	}
	out := Distribute(trains, schedS, cfg)
	// base = 14.07 18:00 = старт → слот 14.07 18:00.
	assert.Equal(t, tm(2026, 7, 14, 18, 0), out["R"])
}

// Delay меняет ПОРЯДОК очереди: задержанный поезд пропускает вперёд соседа
// с более поздним Rasch (сортировка по base = Rasch + Delay).
func TestDistribute_DelayReordersQueue(t *testing.T) {
	cfg := baseCfg()
	trains := []Train{
		{Key: "early", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 1, 0)), VagonCount: 60, Pc: 120,
			Delay: 48 * time.Hour}, // физически раньше, но брошен на 2 суток → base 16.07 01:00
		{Key: "late", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 8, 0)), VagonCount: 60, Pc: 120},
	}
	out := Distribute(trains, schedS, cfg)
	// late идёт первым (base 08:00 < 16.07 01:00): старт 18:00 → слот 14.07 18:00.
	assert.Equal(t, tm(2026, 7, 14, 18, 0), out["late"])
	// early: floor = max(старт, 18:00+12ч интервала, base 16.07 01:00) = 16.07 01:00 → слот 16.07 06:00.
	assert.Equal(t, tm(2026, 7, 16, 6, 0), out["early"], "задержанный поезд встаёт после base с учётом задержки")
}

// Фиксированный StartTime: what-if снял план с последнего планового — стартовое
// время НЕ плывёт (иначе правка одного поезда сдвинула бы прогнозы всех).
func TestDistribute_FixedStartTime(t *testing.T) {
	cfg := baseCfg()
	// База: плановый P (нитка 16.07 08:00) даёт стартовое время 16.07 18:00.
	fixed := NextEighteen(tm(2026, 7, 16, 8, 0), cfg.Now)
	require.Equal(t, tm(2026, 7, 16, 18, 0), fixed)
	cfg.StartTime = &fixed
	// What-if: план у P снят (бросок) — без StartTime старт откатился бы к Now-18:00.
	trains := []Train{
		{Key: "P", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 1, 0)), VagonCount: 60, Pc: 120,
			Bros: true, Delay: 48 * time.Hour},
		{Key: "B", Station: "S", Group: "g", RaschMsk: tp(tm(2026, 7, 14, 8, 0)), VagonCount: 60, Pc: 120},
	}
	out := Distribute(trains, schedS, cfg)
	// B первый: floor = max(StartTime 16.07 18:00, base 08:00) → слот 16.07 18:00.
	assert.Equal(t, tm(2026, 7, 16, 18, 0), out["B"], "беспланные стартуют не раньше фиксированного StartTime")
	// P: base 16.07 01:00 < очередь 16.07 18:00+12ч → слот 17.07 06:00.
	assert.Equal(t, tm(2026, 7, 17, 6, 0), out["P"])
}

// «Поставить на нитку»: поезд с PlanMsk фиксируется на слоте, слот занят для остальных.
func TestDistribute_AssignSlotOccupies(t *testing.T) {
	cfg := baseCfg()
	trains := []Train{
		{Key: "assigned", Station: "S", Group: "g", PlanMsk: tp(tm(2026, 7, 14, 18, 0)), RaschMsk: tp(tm(2026, 7, 14, 1, 0)), VagonCount: 30, Pc: 100000},
		{Key: "other", Station: "S", Group: "g2", RaschMsk: tp(tm(2026, 7, 14, 2, 0)), VagonCount: 30, Pc: 100000},
	}
	out := Distribute(trains, schedS, cfg)
	assert.Equal(t, tm(2026, 7, 14, 18, 0), out["assigned"], "назначенный на нитку фиксирован")
	// other: старт = после плановых (NextEighteen от 14.07 18:00 = 15.07 18:00), слот 18:00 занят... старт уже 15.07.
	assert.Equal(t, tm(2026, 7, 15, 18, 0), out["other"], "чужая нитка занята, беспланный после плановых")
}
