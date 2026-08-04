package unloadsim

import (
	"fmt"
	"math"
	"time"
)

// Цвета по типам грузов, если подгруппа не несёт цвет клиента
// (эталон: constants.ts CARGO_COLORS).
var cargoColors = map[string]string{
	"УГОЛЬ":  "#00B0F0",
	"МЕТАЛЛ": "#FF5722",
	"ЧУГУН":  "#9C27B0",
	"ОБЩИЙ":  "#4CAF50",
}

const remainderColor = "#9E9E9E" // безымянный остаток
const waitColor = "#E0E0E0"      // блок простоя

// Порог фиксации простоя, минут: короткие ожидания не считаются простоем.
const waitThresholdMin = 15

// SubGroup — подгруппа поезда в потоке выгрузки. Эталон дублирует поезд по
// подгруппам, поэтому в симуляции у поезда всегда ровно одна подгруппа.
type SubGroup struct {
	Key         string // ключ подгруппы (для дедупликации сборных поездов)
	VagonCount  int
	Color       string // цвет клиента; пусто → цвет типа груза
	StationNach string
	IndexMain   string
	GruzpolS    string
}

// Train — поезд в потоке выгрузки (одна подгруппа одного потока).
type Train struct {
	Index    string
	CalcTime time.Time // прибытие в расчётной шкале (RailwayToCalc от prog_jd)
	OrigJd   time.Time // исходное ЖД-прибытие до правок (для колонки «Ожид»); zero = нет
	Sub      SubGroup
}

// Flow — один поток выгрузки: пара (терминал, тип груза) с очередью поездов.
type Flow struct {
	Port             string // терминал (naznach)
	Cargo            string // "ОБЩИЙ" либо группа груза (ГУТ-2: УГОЛЬ/МЕТАЛЛ/ЧУГУН)
	Trains           []Train
	InitialRemainder int            // остаток на 18:00 перед первыми сутками
	PlanSpeed        int            // плановая скорость, ваг/сут
	PlanOverrides    map[string]int // "YYYY-MM-DD" → скорость на конкретные сутки
	NormSpeed        int            // нормативная скорость (полезное образование)
	MaxTrainWagons   int            // технологический максимум состава (отцепка хвоста)
}

// Operation — блок на диаграмме Ганта (выгрузка, остаток или простой).
type Operation struct {
	TrainIndex  string
	TrainName   string
	StationNach string
	IndexMain   string
	GruzpolS    string
	// OrigIndex — чей остаток (индекс поезда, породившего безымянный остаток).
	OrigIndex     string
	StartCalc     time.Time // расчётная шкала
	EndCalc       time.Time
	StartJd       time.Time // ЖД-шкала (для подписей)
	EndJd         time.Time
	Wagons        int
	TotalWagons   int
	Color         string
	IsRemainder   bool
	IsCarriedOver bool
	IsPartial     bool
	WaitMin       float64   // минуты простоя (только для блоков простоя)
	OrigArrivalJd time.Time // исходное ЖД-прибытие поезда (zero = нет)
}

// DayResult — итог одних расчётных суток потока.
type DayResult struct {
	Date            string // YYYY-MM-DD (расчётные сутки = ЖД-сутки)
	Port            string
	Cargo           string
	PlanSpeed       int
	NormSpeed       int
	IncomingTotal   int // вагонов с предыдущих суток (остаток + перенесённые поезда)
	Arrival         int // прибывает в эти сутки
	TotalFormation  int // полное образование
	UsefulFormation int // полезное образование (по норме, без простоев)
	Unloaded        int
	Remaining       int     // остаток на конец суток
	TotalWaitMin    float64 // суммарный простой, минут
	RemainderOut    int     // безымянный остаток, перешедший на завтра
	RemainderOutIdx string  // чей это остаток
	CarriedOver     []Train // поезда, перенесённые на завтра
	Operations      []Operation
}

// SimulateFlow прогоняет поток по суткам: день за днём, остатки и
// недовыгруженные поезда переходят на следующие сутки (эталон: useUnloading.ts).
// start — календарная дата первых расчётных суток (00:00), days ≥ 1.
func SimulateFlow(f Flow, start time.Time, days int) []DayResult {
	results := make([]DayResult, 0, days)
	st := carryState{
		remainder:    f.InitialRemainder,
		remainderIdx: "Остаток",
		remColor:     remainderColor,
	}
	for d := 0; d < days; d++ {
		dayStart := start.AddDate(0, 0, d)
		st, results = simulateDay(f, dayStart, st, results)
	}
	return results
}

// carryState — что переходит из суток в сутки.
type carryState struct {
	remainder    int
	remainderIdx string
	remStation   string
	remColor     string
	trains       []Train // перенесённые целиком или частично поезда
}

func simulateDay(f Flow, dayStart time.Time, in carryState, acc []DayResult) (carryState, []DayResult) {
	date := dayStart.Format("2006-01-02")
	planSpeed := f.PlanSpeed
	if v, ok := f.PlanOverrides[date]; ok {
		planSpeed = v
	}
	perHour := float64(planSpeed) / 24.0

	// Сутки в миллисекундах от их начала. Конец суток — 23:59:59.999, как в
	// эталоне (JS-даты, целые миллисекунды): последняя миллисекунда включена.
	const dayEndMs int64 = 24*3600*1000 - 1
	at := func(ms int64) time.Time { return dayStart.Add(time.Duration(ms) * time.Millisecond) }

	// Поезда, прибывающие в эти сутки, по возрастанию времени прибытия.
	var arriving []Train
	for _, t := range f.Trains {
		ms := t.CalcTime.Sub(dayStart).Milliseconds()
		if ms >= 0 && ms < dayEndMs {
			arriving = append(arriving, t)
		}
	}
	sortStableByCalc(arriving)

	all := append(append([]Train{}, in.trains...), arriving...)
	nIncoming := len(in.trains)

	ops := []Operation{}
	var curMs int64
	unloaded := 0
	totalWait := 0.0
	out := carryState{
		remainderIdx: in.remainderIdx,
		remStation:   in.remStation,
		remColor:     in.remColor,
	}
	var partials []Train
	consumed := make([]bool, len(all))

	cargoSuffix := f.Cargo
	if cargoSuffix == "ОБЩИЙ" {
		cargoSuffix = ""
	}

	// Сначала выгружается остаток предыдущих суток.
	if in.remainder > 0 {
		hours := float64(in.remainder) / perHour
		endMs := jsAddHours(curMs, hours)
		origIdx := ""
		if in.remainderIdx != "Остаток" {
			origIdx = in.remainderIdx
		}
		if endMs <= dayEndMs {
			ops = append(ops, Operation{
				TrainIndex: "Остаток", TrainName: fmt.Sprintf("Остаток %s", cargoSuffix),
				OrigIndex: origIdx, StationNach: in.remStation, GruzpolS: cargoSuffix,
				StartCalc: at(curMs), EndCalc: at(endMs),
				StartJd: CalcToRailway(at(curMs)), EndJd: CalcToRailway(at(endMs)),
				Wagons: in.remainder, TotalWagons: in.remainder,
				Color: nonEmpty(in.remColor, remainderColor), IsRemainder: true,
			})
			unloaded += in.remainder
			curMs = endMs
		} else {
			// Остаток не успевает за сутки — делится на части.
			availHours := float64(dayEndMs-curMs) / 3600000.0
			w := minInt(jsRound(perHour*availHours), in.remainder)
			ops = append(ops, Operation{
				TrainIndex: "Остаток", TrainName: fmt.Sprintf("Остаток %s (частично)", cargoSuffix),
				OrigIndex: origIdx, StationNach: in.remStation,
				StartCalc: at(curMs), EndCalc: at(dayEndMs),
				StartJd: CalcToRailway(at(curMs)), EndJd: CalcToRailway(at(dayEndMs)),
				Wagons: w, TotalWagons: in.remainder,
				Color: nonEmpty(in.remColor, remainderColor), IsRemainder: true, IsPartial: true,
			})
			unloaded += w
			out.remainder = in.remainder - w
			curMs = dayEndMs
		}
	}

	// Поезда по очереди прибытия.
	if curMs < dayEndMs {
		for i, tr := range all {
			color := nonEmpty(tr.Sub.Color, nonEmpty(cargoColors[f.Cargo], "#000000"))
			tMs := tr.CalcTime.Sub(dayStart).Milliseconds()

			// Простой между окончанием предыдущей выгрузки и прибытием поезда.
			if tMs > curMs {
				waitMin := float64(tMs-curMs) / 60000.0
				if waitMin > waitThresholdMin {
					totalWait += waitMin
					ops = append(ops, Operation{
						TrainIndex: tr.Index, TrainName: "Простой",
						StationNach: tr.Sub.StationNach, GruzpolS: tr.Sub.GruzpolS,
						StartCalc: at(curMs), EndCalc: at(tMs),
						StartJd: CalcToRailway(at(curMs)), EndJd: CalcToRailway(at(tMs)),
						Color: waitColor, WaitMin: waitMin, OrigArrivalJd: tr.OrigJd,
					})
				}
				curMs = tMs
			}

			// Технологический максимум состава: хвост отцепляется на подходе.
			effective := minInt(tr.Sub.VagonCount, f.MaxTrainWagons)
			hours := float64(effective) / perHour
			endMs := jsAddHours(curMs, hours)

			if endMs <= dayEndMs {
				// Поезд выгружается целиком.
				ops = append(ops, Operation{
					TrainIndex: tr.Index, TrainName: fmt.Sprintf("%s (%d в)", tr.Index, effective),
					StationNach: tr.Sub.StationNach, IndexMain: tr.Sub.IndexMain, GruzpolS: tr.Sub.GruzpolS,
					StartCalc: at(curMs), EndCalc: at(endMs),
					StartJd: CalcToRailway(at(curMs)), EndJd: CalcToRailway(at(endMs)),
					Wagons: effective, TotalWagons: effective, Color: color,
					IsCarriedOver: i < nIncoming, OrigArrivalJd: tr.OrigJd,
				})
				unloaded += effective
				curMs = endMs
				consumed[i] = true
			} else {
				// Частичная выгрузка: остаток переносится ИМЕНОВАННЫМ поездом
				// с уменьшенной подгруппой (не безымянным остатком).
				availHours := float64(dayEndMs-curMs) / 3600000.0
				w := minInt(jsRound(perHour*availHours), effective)
				ops = append(ops, Operation{
					TrainIndex: tr.Index, TrainName: fmt.Sprintf("%s (частично)", tr.Index),
					StationNach: tr.Sub.StationNach, IndexMain: tr.Sub.IndexMain, GruzpolS: tr.Sub.GruzpolS,
					StartCalc: at(curMs), EndCalc: at(dayEndMs),
					StartJd: CalcToRailway(at(curMs)), EndJd: CalcToRailway(at(dayEndMs)),
					Wagons: w, TotalWagons: effective, Color: color,
					IsCarriedOver: i < nIncoming, IsPartial: true, OrigArrivalJd: tr.OrigJd,
				})
				unloaded += w
				// Отход от эталона (осознанный): в gtlogic поезд, у которого
				// выгрузка округлилась до 0 вагонов, попадал в перенос ДВАЖДЫ —
				// и частично выгруженным, и «не начавшим» (startedTrainKeys
				// отбирал только операции с wagons > 0). Здесь поезд помечается
				// обработанным всегда — задвоения нет.
				consumed[i] = true
				left := tr.Sub.VagonCount - w
				if left < 0 {
					left = 0
				}
				if left > 0 {
					carried := tr
					carried.Sub.VagonCount = left
					partials = append(partials, carried)
				}
				curMs = dayEndMs
				break
			}
		}
	}

	// Не начавшие выгрузку поезда переходят целиком.
	out.trains = append(out.trains, partials...)
	for i, tr := range all {
		if !consumed[i] {
			out.trains = append(out.trains, tr)
		}
	}

	// Финальный простой после всех поездов.
	if curMs < dayEndMs {
		waitMin := float64(dayEndMs-curMs) / 60000.0
		if waitMin > waitThresholdMin {
			totalWait += waitMin
			ops = append(ops, Operation{
				TrainName: "Простой",
				StartCalc: at(curMs), EndCalc: at(dayEndMs),
				StartJd: CalcToRailway(at(curMs)), EndJd: CalcToRailway(at(dayEndMs)),
				Color: waitColor, WaitMin: waitMin,
			})
		}
	}

	// Части остатка, не перекрытые выше: если остаток выгружен целиком,
	// исходные подпись/цвет/станция сохраняются (эталон).
	if out.remainder > 0 {
		out.remainderIdx = in.remainderIdx
		out.remStation = in.remStation
		out.remColor = in.remColor
	}

	clamp := func(t Train) int { return minInt(t.Sub.VagonCount, f.MaxTrainWagons) }
	incomingTotal := in.remainder
	for _, t := range in.trains {
		incomingTotal += clamp(t)
	}
	arrivalSum := 0
	for _, t := range arriving {
		arrivalSum += clamp(t)
	}
	remaining := out.remainder
	for _, t := range out.trains {
		remaining += clamp(t)
	}

	acc = append(acc, DayResult{
		Date: date, Port: f.Port, Cargo: f.Cargo,
		PlanSpeed: planSpeed, NormSpeed: f.NormSpeed,
		IncomingTotal: incomingTotal, Arrival: arrivalSum, TotalFormation: incomingTotal + arrivalSum,
		UsefulFormation: simulateNormDay(f, dayStart, in, arriving),
		Unloaded:        unloaded, Remaining: remaining, TotalWaitMin: totalWait,
		RemainderOut: out.remainder, RemainderOutIdx: out.remainderIdx,
		CarriedOver: out.trains, Operations: ops,
	})
	return out, acc
}

// simulateNormDay — «полезное образование»: сколько выгрузилось бы по
// нормативной скорости (без учёта простоев эта величина всё равно зависит от
// времён прибытия — очередь обрабатывается той же хронологией).
func simulateNormDay(f Flow, dayStart time.Time, in carryState, arriving []Train) int {
	perHour := float64(f.NormSpeed) / 24.0
	const dayEndMs int64 = 24*3600*1000 - 1
	var curMs int64
	unloaded := 0

	if in.remainder > 0 {
		hours := float64(in.remainder) / perHour
		endMs := jsAddHours(curMs, hours)
		if endMs <= dayEndMs {
			unloaded += in.remainder
			curMs = endMs
		} else {
			availHours := float64(dayEndMs-curMs) / 3600000.0
			return minInt(jsRound(perHour*availHours), in.remainder)
		}
	}

	all := append(append([]Train{}, in.trains...), arriving...)
	for _, tr := range all {
		tMs := tr.CalcTime.Sub(dayStart).Milliseconds()
		if tMs > curMs {
			curMs = tMs
		}
		effective := minInt(tr.Sub.VagonCount, f.MaxTrainWagons)
		hours := float64(effective) / perHour
		endMs := jsAddHours(curMs, hours)
		if endMs <= dayEndMs {
			unloaded += effective
			curMs = endMs
		} else {
			availHours := float64(dayEndMs-curMs) / 3600000.0
			unloaded += minInt(jsRound(perHour*availHours), effective)
			break
		}
	}
	return unloaded
}

// jsAddHours — сложение «как в JS»: целые миллисекунды позиции + часы во
// float64, результат усекается к нулю (new Date(int + float)). Битовая
// совместимость с эталоном обязательна для golden-тестов.
func jsAddHours(ms int64, hours float64) int64 {
	return int64(float64(ms) + hours*3600000.0)
}

// jsRound — Math.round: половина вверх (для положительных совпадает с math.Round).
func jsRound(x float64) int {
	return int(math.Round(x))
}

func sortStableByCalc(trains []Train) {
	// сортировка вставками — стабильная, как Array.prototype.sort в эталоне
	for i := 1; i < len(trains); i++ {
		for j := i; j > 0 && trains[j].CalcTime.Before(trains[j-1].CalcTime); j-- {
			trains[j], trains[j-1] = trains[j-1], trains[j]
		}
	}
}

func nonEmpty(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
