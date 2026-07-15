// Package stage4 — ядро прогноза прибытия на порт (ProgMsk): раскладка поездов по
// ниткам станции. Чистая доменная логика без БД/часов — все входы (поезда, расписания,
// пороги, «сейчас») приходят параметрами; тестируется на синтетике.
//
// ЕДИНЫЙ подход для всех станций (реш.: в GTport два пути — случайность, оптимизирован
// был один). Модель — «лестница»-очередь причала:
//   - плановый поезд → ProgMsk = PlanMsk (нитка задана планом);
//   - беспланные (вагонов ≥ порога, есть RaschMsk) станции раскладываются по общему пулу
//     слотов станции, но ПО ГРУППЕ-ПРИЧАЛУ (терминал+род) с минимальным интервалом
//     «переваривания» состава: интервал = min(вагонов, лимит станции) × 24 / pc_рода;
//   - ПЕРВАЯ нитка беспланных группы = max(startTime, последний_плановый_группы + его
//     интервал); если планового в группе нет — причал пуст, первый без прибавки интервала;
//   - допуск (slot_tolerance_h, квирк «−6ч») применяется ТОЛЬКО к Rasch — стартовая нитка
//     и интервальный пол остаются жёстким низом (поезд не встаёт раньше стартовой нитки);
//   - currentTime группы ре-якорится на КАЖДУЮ назначенную нитку (очередь причала).
// pc — договорная перерабатывающая способность причала (ваг/сут), фиксирована в справочнике.
package stage4

import (
	"sort"
	"time"
)

// HM — слот расписания (час:минута суток). Слоты повторяются каждые сутки.
type HM struct {
	H, M int
}

// Train — агрегированный поезд (вагоны с общим ключом IdDisl|StanNazn).
type Train struct {
	Key        string     // IdDisl|StanNazn — идентификатор поезда
	Station    string     // station_code — общий пул слотов станции
	Group      string     // терминал|род — интервальная группа (причал) внутри станции
	PlanMsk    *time.Time // плановое прибытие (нитка задана планом); nil — беспланный
	RaschMsk   *time.Time // расчётное прибытие (Stage 3)
	VagonCount int        // число вагонов (для формулы интервала, с учётом лимита станции)
	Pc         int        // перерабатывающая способность причала по роду, ваг/сут; 0 — не спейсим
	Bros       bool       // статус 5 (брошен) — штраф + снижённый порог вагонов
}

// Config — пороги, допуски и лимиты (из client_settings / plan_profile).
type Config struct {
	MinVagon     int                      // порог вагонов для беспланового прогноза (эталон 20)
	MinVagonBros int                      // порог для брошенных (эталон 10)
	BrosPenalty  time.Duration            // штраф бросания (эталон 72ч): сдвиг нитки и базы Mistake
	Tolerance    map[string]time.Duration // station_code → допуск: слот может быть ≥ Rasch − допуск (квирк «−6ч»)
	MaxLen       map[string]int           // station_code → лимит длины состава (ваг) для формулы интервала; 0 — без лимита
	Now          time.Time                // «сейчас» (clock.Now) — старт распределения, если плана нет вовсе
}

// Distribute возвращает ProgMsk для каждого поезда: плановым — их PlanMsk, беспланным —
// назначенный слот станции («лестница» причала). Поезда ниже порога вагонов в результат
// не попадают (у них ProgMsk не будет).
func Distribute(trains []Train, schedules map[string][]HM, cfg Config) map[string]time.Time {
	out := make(map[string]time.Time, len(trains))

	// 1. Плановые поезда → ProgMsk = PlanMsk (нитка зафиксирована планом).
	var maxPlan time.Time
	for _, t := range trains {
		if t.PlanMsk != nil && !t.PlanMsk.IsZero() {
			out[t.Key] = *t.PlanMsk
			if t.PlanMsk.After(maxPlan) {
				maxPlan = *t.PlanMsk
			}
		}
	}

	// 2. Стартовое время беспланового распределения: ближайшие 18:00 ПОСЛЕ последнего
	//    планового прибытия (беспланные идут строго после плановых). Плана нет → от «сейчас».
	startTime := nextEighteen(maxPlan, cfg.Now)

	// 3. Беспланные поезда, сгруппированные по станции → группе-причалу.
	byStation := map[string]map[string][]Train{}
	for _, t := range trains {
		if t.PlanMsk != nil && !t.PlanMsk.IsZero() {
			continue // плановый — уже зафиксирован
		}
		if t.RaschMsk == nil || t.RaschMsk.IsZero() {
			continue // без расчётного прибытия прогноз не строим
		}
		min := cfg.MinVagon
		if t.Bros {
			min = cfg.MinVagonBros
		}
		if t.VagonCount < min {
			continue
		}
		if byStation[t.Station] == nil {
			byStation[t.Station] = map[string][]Train{}
		}
		byStation[t.Station][t.Group] = append(byStation[t.Station][t.Group], t)
	}

	// 4. Раскладываем каждую станцию единой «лестницей» (свой пул слотов).
	for station, groups := range byStation {
		distributeStation(station, groups, schedules[station], cfg, startTime, trains, out)
	}
	return out
}

// distributeStation раскладывает беспланные поезда одной станции «лестницей»-очередью:
// общий пул слотов, но интервальная очередь ПО ГРУППЕ-ПРИЧАЛУ. Плановые нитки станции
// предзаняты; затравка первой нитки группы — последний плановый поезд группы + его интервал.
func distributeStation(station string, groups map[string][]Train, slots []HM, cfg Config, startTime time.Time, allTrains []Train, out map[string]time.Time) {
	tol := cfg.Tolerance[station]
	limit := cfg.MaxLen[station]

	// Занятые нитки станции — плановые поезда (их PlanMsk = нитка). Пул общий для всех причалов.
	// Затравка первой нитки по группе-причалу: последний плановый поезд группы + его интервал.
	occupied := map[time.Time]bool{}
	prevSlot := map[string]time.Time{}
	prevIv := map[string]time.Duration{}
	for _, t := range allTrains {
		if t.Station != station || t.PlanMsk == nil || t.PlanMsk.IsZero() {
			continue
		}
		occupied[*t.PlanMsk] = true
		if cur, ok := prevSlot[t.Group]; !ok || t.PlanMsk.After(cur) {
			prevSlot[t.Group] = *t.PlanMsk
			prevIv[t.Group] = interval(t, limit)
		}
	}

	// Беспланные станции в порядке базового времени (Rasch + штраф бросания).
	var trains []Train
	for _, g := range groups {
		trains = append(trains, g...)
	}
	sort.SliceStable(trains, func(i, j int) bool { return base(trains[i], cfg).Before(base(trains[j], cfg)) })

	for _, t := range trains {
		g := t.Group
		lower := startTime // причал пуст (нет планового предшественника) → без прибавки интервала
		if s, ok := prevSlot[g]; ok {
			lower = s.Add(prevIv[g]) // причал освободится после «переваривания» предыдущего состава
		}
		// допуск −6ч — только к Rasch; стартовая нитка и интервальный пол = жёсткий низ.
		floor := maxTime(maxTime(startTime, lower), base(t, cfg).Add(-tol))
		slot := findSlot(floor, slots, occupied)
		out[t.Key] = slot
		occupied[slot] = true
		prevSlot[g] = slot // ре-якорь очереди причала на назначенную нитку
		prevIv[g] = interval(t, limit)
	}
}

// base — базовое расчётное время поезда: RaschMsk (+штраф, если брошен).
func base(t Train, cfg Config) time.Time {
	b := *t.RaschMsk
	if t.Bros {
		b = b.Add(cfg.BrosPenalty)
	}
	return b
}

// interval — время «переваривания» состава причалом: min(вагонов, лимит) × 24ч / pc_рода.
// Лимит длины состава — станционная настройка (наши причалы 64 ваг): в дислокации поезд
// до 71 ваг, но причал ограничен по длине. pc ≤ 0 (ёмкость неизвестна) → 0 (не спейсим).
func interval(t Train, limit int) time.Duration {
	if t.Pc <= 0 {
		return 0
	}
	v := t.VagonCount
	if limit > 0 && v > limit {
		v = limit
	}
	return time.Duration(float64(v) * 24.0 / float64(t.Pc) * float64(time.Hour))
}

// findSlot — ближайший свободный слот ≥ minTime. Сначала слоты текущих суток, затем
// последующих (перенос findAvailableSlotFromTime эталона); fallback — слот через 7 суток.
func findSlot(minTime time.Time, slots []HM, occupied map[time.Time]bool) time.Time {
	if len(slots) == 0 {
		return minTime // расписания нет — ставим на само минимальное время
	}
	day := time.Date(minTime.Year(), minTime.Month(), minTime.Day(), 0, 0, 0, 0, minTime.Location())
	// текущие сутки: слот ≥ minTime и свободен.
	for _, s := range slots {
		t := at(day, s)
		if !t.Before(minTime) && !occupied[t] {
			return t
		}
	}
	// последующие сутки: первый свободный слот.
	for d := 1; d <= 14; d++ {
		nd := day.Add(time.Duration(d) * 24 * time.Hour)
		for _, s := range slots {
			t := at(nd, s)
			if !occupied[t] {
				return t
			}
		}
	}
	return at(day.Add(7*24*time.Hour), slots[0]) // fallback (как эталон)
}

// nextEighteen — ближайшие 18:00 ПОСЛЕ ref (операционная граница суток). Нулевой ref →
// ближайшие 18:00 от now. Перенос startTime из calculateProgMskWithSchedule эталона.
func nextEighteen(ref, now time.Time) time.Time {
	if !ref.IsZero() {
		if ref.Hour() < 18 {
			return time.Date(ref.Year(), ref.Month(), ref.Day(), 18, 0, 0, 0, ref.Location())
		}
		d := time.Date(ref.Year(), ref.Month(), ref.Day(), 0, 0, 0, 0, ref.Location())
		return d.Add(24 * time.Hour).Add(18 * time.Hour)
	}
	st := time.Date(now.Year(), now.Month(), now.Day(), 18, 0, 0, 0, now.Location())
	if st.Before(now) {
		st = st.Add(24 * time.Hour)
	}
	return st
}

func at(day time.Time, s HM) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), s.H, s.M, 0, 0, day.Location())
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
