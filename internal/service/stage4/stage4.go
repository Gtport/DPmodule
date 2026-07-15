// Package stage4 — ядро прогноза прибытия на порт (ProgMsk): раскладка поездов по
// ниткам станции. Чистая доменная логика без БД/часов — все входы (поезда, расписания,
// пороги, «сейчас») приходят параметрами; тестируется на синтетике.
//
// Универсализация эталона GTport (enrich_stage4.go): вместо хардкода станций/имён и
// фиксированных интервалов (4/10.5/16ч) — интервал по формуле `вагонов × 24 / pc_рода`,
// станция задаёт общий пул слотов, «наши» терминалы/род — из настроек. Два разных пути
// эталона (УТ-1 vs АЭ/ГУТ-2) сведены в ОДИН: пул слотов на станцию, интервальные группы
// = (терминал+род), допуск слота настраиваемый (перенос квирка «−6ч» УТ-1 в данные).
package stage4

import (
	"sort"
	"time"
)

// Методы раскладки беспланных поездов по слотам станции (перенос двух алгоритмов
// эталона GTport). Метод задаётся профилем станции (plan_profile.distribution_method).
const (
	// MethodExcel — общий пул + глобальная сортировка по Rasch + ближайшая свободная
	// нитка (distributeAEGUT2ByExcelMethod эталона). АЭ/ГУТ-2. Значение по умолчанию.
	MethodExcel = "excel"
	// MethodStaircase — последовательная «лестница» (distributeGroupWithInterval эталона):
	// currentTime ре-якорится на НАЗНАЧЕННУЮ нитку, следующий поезд ≥ currentTime +
	// интервал; допуск живёт ТОЛЬКО в Rasch-слагаемом, стартовая нитка — жёсткий низ. УТ-1.
	MethodStaircase = "staircase"
)

// HM — слот расписания (час:минута суток). Слоты повторяются каждые сутки.
type HM struct {
	H, M int
}

// Train — агрегированный поезд (вагоны с общим ключом IdDisl|StanNazn).
type Train struct {
	Key        string     // IdDisl|StanNazn — идентификатор поезда
	Station    string     // station_code — общий пул слотов станции
	Group      string     // терминал|род — интервальная группа внутри станции
	PlanMsk    *time.Time // плановое прибытие (нитка задана планом); nil — беспланный
	RaschMsk   *time.Time // расчётное прибытие (Stage 3)
	VagonCount int        // число вагонов (для формулы интервала)
	Pc         int        // перерабатывающая способность терминала по роду, ваг/сут; 0 — не спейсим
	Bros       bool       // статус 5 (брошен) — штраф + снижённый порог вагонов
}

// Config — пороги и допуски (из client_settings / plan_profile).
type Config struct {
	MinVagon     int                      // порог вагонов для беспланового прогноза (эталон 20)
	MinVagonBros int                      // порог для брошенных (эталон 10)
	BrosPenalty  time.Duration            // штраф бросания (эталон 72ч): сдвиг нитки и базы Mistake
	Tolerance    map[string]time.Duration // station_code → допуск: слот может быть ≥ Rasch − допуск (квирк «−6ч»)
	Method       map[string]string        // station_code → метод раскладки (MethodStaircase|MethodExcel); пусто → excel
	Now          time.Time                // «сейчас» (clock.Now) — старт распределения, если плана нет вовсе
}

// Distribute возвращает ProgMsk для каждого поезда: плановым — их PlanMsk (нитка задана
// планом), беспланным (вагонов ≥ порога, есть RaschMsk) — назначенный свободный слот
// станции. Поезда, не прошедшие порог, в результат не попадают (у них ProgMsk не будет).
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

	// 3. Беспланные поезда, сгруппированные по станции → интервальной группе.
	//    station → group → []train
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

	// 4. Распределяем по каждой станции независимо (свой пул слотов), методом из профиля.
	for station, groups := range byStation {
		if cfg.Method[station] == MethodStaircase {
			distributeStaircase(station, groups, schedules[station], cfg, startTime, out)
		} else {
			distributeStation(station, groups, schedules[station], cfg, startTime, trains, out)
		}
	}
	return out
}

// distributeStaircase — «лестница» эталона (distributeGroupWithInterval), метод УТ-1.
// Все беспланные поезда станции — в один пул (без разбивки по роду), по порядку Rasch
// (+штраф бросания). currentTime стартует со startTime и ре-якорится на КАЖДУЮ назначенную
// нитку; следующий поезд не раньше currentTime + интервал. Допуск (−6ч) применяется ТОЛЬКО
// к Rasch-слагаемому — стартовая нитка и интервальный пол остаются жёстким низом, поэтому
// поезд (в т.ч. первый) не может встать раньше стартовой нитки. Плановые нитки НЕ
// предзанимаются (как в эталоне УТ-1) — беспланные идут после startTime, пересечения нет.
func distributeStaircase(station string, groups map[string][]Train, slots []HM, cfg Config, startTime time.Time, out map[string]time.Time) {
	var trains []Train
	for _, g := range groups {
		trains = append(trains, g...)
	}
	base := func(t Train) time.Time {
		b := *t.RaschMsk
		if t.Bros {
			b = b.Add(cfg.BrosPenalty)
		}
		return b
	}
	sort.SliceStable(trains, func(i, j int) bool { return base(trains[i]).Before(base(trains[j])) })

	tol := cfg.Tolerance[station]
	occupied := map[time.Time]bool{}
	currentTime := startTime
	for _, t := range trains {
		minByRasch := base(t).Add(-tol)               // допуск −6ч — только к Rasch (штраф уже в base)
		minByInterval := currentTime.Add(interval(t)) // пол от предыдущей НАЗНАЧЕННОЙ нитки, без допуска
		slot := findSlot(maxTime(minByInterval, minByRasch), slots, occupied)
		out[t.Key] = slot
		occupied[slot] = true
		currentTime = slot // ре-якорь на назначенную нитку
	}
}

// distributeStation раскладывает беспланные поезда одной станции по её слотам.
// Пул слотов общий для всех терминалов станции; плановые нитки заняты заранее.
func distributeStation(station string, groups map[string][]Train, slots []HM, cfg Config, startTime time.Time, allTrains []Train, out map[string]time.Time) {
	// Занятые слоты: нитки плановых поездов ЭТОЙ станции.
	occupied := map[time.Time]bool{}
	for _, t := range allTrains {
		if t.Station == station && t.PlanMsk != nil && !t.PlanMsk.IsZero() {
			occupied[*t.PlanMsk] = true
		}
	}

	// Для каждой группы — последовательность Rasch с интервалом по формуле.
	var all []withRasch
	for _, gtrains := range groups {
		all = append(all, applyInterval(gtrains, cfg, startTime)...)
	}
	// Общая сортировка по Rasch и назначение на ближайший свободный слот.
	sort.SliceStable(all, func(i, j int) bool { return all[i].rasch.Before(all[j].rasch) })

	tol := cfg.Tolerance[station]
	for _, item := range all {
		slot := findSlot(item.rasch.Add(-tol), slots, occupied)
		out[item.key] = slot
		occupied[slot] = true
	}
}

type withRasch struct {
	key   string
	rasch time.Time
}

// applyInterval выстраивает поезда группы по Rasch с минимальным интервалом:
//
//	Rasch[0] = max(base[0], startTime)
//	Rasch[i] = max(base[i], Rasch[i-1] + интервал(поезд i-1))
//
// base = RaschMsk (+штраф, если брошен); интервал(поезд) = вагонов × 24ч / pc_рода.
func applyInterval(trains []Train, cfg Config, startTime time.Time) []withRasch {
	if len(trains) == 0 {
		return nil
	}
	base := func(t Train) time.Time {
		b := *t.RaschMsk
		if t.Bros {
			b = b.Add(cfg.BrosPenalty)
		}
		return b
	}
	sort.SliceStable(trains, func(i, j int) bool { return base(trains[i]).Before(base(trains[j])) })

	out := make([]withRasch, len(trains))
	var prev time.Time
	for i, t := range trains {
		var rasch time.Time
		if i == 0 {
			rasch = maxTime(base(t), startTime)
		} else {
			rasch = maxTime(base(t), prev.Add(interval(trains[i-1])))
		}
		out[i] = withRasch{key: t.Key, rasch: rasch}
		prev = rasch
	}
	return out
}

// interval — время «переваривания» поезда станцией: вагонов × 24ч / pc_рода.
// pc ≤ 0 (ёмкость неизвестна) → 0 (не спейсим, поезда разведёт только занятость слотов).
func interval(t Train) time.Duration {
	if t.Pc <= 0 {
		return 0
	}
	return time.Duration(float64(t.VagonCount) * 24.0 / float64(t.Pc) * float64(time.Hour))
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
