package service

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Движок суточной аналитики «Грузовой работы» — перенос gtport
// (analytics/port_analytics_service.go) без изменения поведения.
//
// Что считает: сколько вагонов терминал МОГ переработать за учётные сутки при
// своей перерабатывающей способности, если выгружать подряд — сперва остаток
// прошлых суток, затем поезда по мере прибытия. Отсюда три цифры учётного листа:
//
//   · полезное образование — сколько реально успели бы выгрузить в сутках;
//   · полное образование   — сколько всего было подано (включая не успетое);
//   · простой порта        — сколько времени фронт стоял без работы.
//
// Сравнение полезного образования с фактом выгрузки даёт «эффективность»: порт
// сработал хуже/лучше, чем позволяла его способность.
//
// Расчётные сутки: движок переводит штамп прибытия в положение ВНУТРИ ЖД-суток
// (час отсечки → 00:00), считает на отрезке 00:00…24:00 и отдаёт времена
// обратно в ЖД-представлении. Час отсечки — параметр (у нас 18), не константа.
//
// ⚠️ Сутки честно ЖД-шные, и это держится на том, ЧЕМ кормят движок: на входе
// vagon_history.date_prib, а он для статуса 10 равен date_op_jd — уже СДВИНУТОМУ
// штампу (time_op +24ч при часе ≥ отсечки, enrich.go computeDateKon). Поэтому
// вагон, реально прибывший 20-го в 19:22, лежит в истории как 21.07 19:22 с
// date_prib_d = 21 (ЖД-сутки 21 идут с 20.07 18:00) и после сдвига встаёт в
// 01:22 — в начало своих суток, раньше пришедшего 21-го в 08:15 (→ 14:15).
// Набор поездов ОБЯЗАН выбираться по date_prib_d, иначе порядок внутри суток
// перестанет быть хронологическим.

// cargoWorkCutoffDefault — час начала ЖД-суток по умолчанию (как в Stage 4).
const cargoWorkCutoffDefault = 18

// cargoWorkIdleMin — простой короче этого в операции не пишем (порог gtport).
const cargoWorkIdleMin = 15 * time.Minute

// CargoWorkTrain — поезд на входе движка: сколько вагонов и когда прибыл.
type CargoWorkTrain struct {
	Name    string           // индекс поезда (index_pp)
	Wagons  int              // вагонов этой линии учёта
	Arrival domain.LocalTime // vagon_history.date_prib — ЖД-штамп, НЕ сырое time_op
}

// CargoWorkOperation — отрезок расчётных суток: выгрузка конкретного поезда,
// выгрузка остатка либо простой порта. Времена — в ЖД-представлении.
type CargoWorkOperation struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Operation string `json:"operation"`
	Wagons    int    `json:"wagons"`
	Duration  string `json:"duration"`
	TrainName string `json:"train_name"`
}

// CargoWorkWait — ожидание поезда: прибыл, но фронт был занят предыдущим.
type CargoWorkWait struct {
	TrainName    string `json:"train_name"`
	ArrivalTime  string `json:"arrival_time"`
	StartTime    string `json:"start_time"`
	WaitDuration string `json:"wait_duration"`
	WaitReason   string `json:"wait_reason"`
}

// CargoWorkAnalytics — результат расчёта суток (ложится в analytics_json).
type CargoWorkAnalytics struct {
	UsefulFormation int                  `json:"useful_formation"`
	TotalFormation  int                  `json:"total_formation"`
	Downtime        string               `json:"downtime"`
	Operations      []CargoWorkOperation `json:"operations"`
	Waits           []CargoWorkWait      `json:"waits"`
}

// calcCargoWorkDay — расчёт учётных суток линии.
//
//	day       — учётные ЖД-сутки (= date_prib_d поездов; время игнорируется);
//	remainder — остаток вагонов на начало суток (ost_18);
//	dailySpeed— перерабатывающая способность линии, ваг/сут (pc);
//	cutoff    — час начала ЖД-суток (≤0 → 18);
//	trains    — поезда этой линии; вне расчётных суток отбрасываются.
//
// Способность ≤ 0 (линия не настроена) — выгружать нечем: нулевое образование
// и полный простой. Молча, без ошибки: это настройка терминала, а не порча данных.
func calcCargoWorkDay(day time.Time, remainder, dailySpeed, cutoff int, trains []CargoWorkTrain) CargoWorkAnalytics {
	if cutoff <= 0 || cutoff >= 24 {
		cutoff = cargoWorkCutoffDefault
	}
	out := CargoWorkAnalytics{
		Operations: []CargoWorkOperation{},
		Waits:      []CargoWorkWait{},
	}

	startOfDay := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	endOfDay := startOfDay.Add(24 * time.Hour)

	if dailySpeed <= 0 {
		addCargoWorkOp(&out, "Простой порта", startOfDay, endOfDay, 0, "", cutoff)
		out.Downtime = sumCargoWorkIdle(out.Operations)
		return out
	}
	speedPerHour := float64(dailySpeed) / 24.0

	queue := sortCargoWorkTrains(trains, startOfDay, endOfDay, cutoff)
	current := startOfDay

	// 1. Остаток прошлых суток выгружается первым — фронт занят с 00:00.
	if remainder > 0 {
		end := current.Add(wagonsDuration(remainder, speedPerHour))
		if end.After(endOfDay) {
			done := int(speedPerHour * endOfDay.Sub(current).Hours())
			addCargoWorkOp(&out, "Выгрузка остатка", current, endOfDay, done, "", cutoff)
			out.UsefulFormation += done
			out.TotalFormation += remainder // подан весь остаток, даже если не успели
			current = endOfDay
		} else {
			addCargoWorkOp(&out, "Выгрузка остатка", current, end, remainder, "", cutoff)
			out.UsefulFormation += remainder
			out.TotalFormation += remainder
			current = end
		}
	}

	// 2. Поезда по очереди: прибывшие ждут, пока фронт освободится.
	var waiting []cargoWorkQueued
	next := 0
	for next < len(queue) || len(waiting) > 0 {
		for next < len(queue) && queue[next].calc.Before(current) {
			waiting = append(waiting, queue[next])
			next++
		}

		if len(waiting) > 0 {
			train := waiting[0]
			waiting = waiting[1:]

			if train.calc.Before(current) {
				if wait := current.Sub(train.calc); wait > 0 {
					out.Waits = append(out.Waits, CargoWorkWait{
						TrainName:    train.Name,
						ArrivalTime:  formatCargoWorkTime(train.calc, cutoff),
						StartTime:    formatCargoWorkTime(current, cutoff),
						WaitDuration: formatCargoWorkDur(wait),
						WaitReason:   "Порт занят",
					})
				}
			}

			end := current.Add(wagonsDuration(train.Wagons, speedPerHour))
			label := fmt.Sprintf("Выгрузка %s", train.Name)
			if end.After(endOfDay) {
				done := int(math.Floor(speedPerHour * endOfDay.Sub(current).Hours()))
				addCargoWorkOp(&out, label, current, endOfDay, done, train.Name, cutoff)
				out.UsefulFormation += done
				out.TotalFormation += train.Wagons
				current = endOfDay
			} else {
				addCargoWorkOp(&out, label, current, end, train.Wagons, train.Name, cutoff)
				out.UsefulFormation += train.Wagons
				out.TotalFormation += train.Wagons
				current = end
			}
			continue
		}

		// Очередь пуста — фронт стоит до прихода следующего поезда.
		if next < len(queue) {
			train := queue[next]
			if train.calc.After(current) {
				if train.calc.Sub(current) > cargoWorkIdleMin {
					addCargoWorkOp(&out, "Простой порта", current, train.calc, 0, "", cutoff)
				}
				current = train.calc
			}
			waiting = append(waiting, train)
			next++
		}
	}

	// 3. Хвост суток после последней выгрузки — тоже простой.
	if current.Before(endOfDay) && endOfDay.Sub(current) > cargoWorkIdleMin {
		addCargoWorkOp(&out, "Простой порта", current, endOfDay, 0, "", cutoff)
	}

	out.Downtime = sumCargoWorkIdle(out.Operations)
	return out
}

// cargoWorkQueued — поезд с посчитанным положением внутри расчётных суток.
type cargoWorkQueued struct {
	CargoWorkTrain
	calc time.Time
}

// sortCargoWorkTrains переводит прибытия в расчётное время, отбрасывает поезда
// вне суток и сортирует по очереди подхода.
func sortCargoWorkTrains(trains []CargoWorkTrain, startOfDay, endOfDay time.Time, cutoff int) []cargoWorkQueued {
	out := make([]cargoWorkQueued, 0, len(trains))
	for _, t := range trains {
		if t.Arrival.IsZero() {
			continue
		}
		calc := toCargoWorkCalc(t.Arrival.Time(), cutoff)
		if calc.Before(startOfDay) || calc.After(endOfDay) {
			continue
		}
		out = append(out, cargoWorkQueued{CargoWorkTrain: t, calc: calc})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].calc.Before(out[j].calc) })
	return out
}

// wagonsDuration — сколько времени займёт выгрузка N вагонов на скорости ваг/час.
func wagonsDuration(wagons int, speedPerHour float64) time.Duration {
	return time.Duration(float64(wagons)/speedPerHour*3600) * time.Second
}

// toCargoWorkCalc — МСК-время → положение внутри расчётных суток (час отсечки
// становится нулём). Дата сохраняется: движок смотрит только очерёдность внутри
// суток, набор поездов задаётся снаружи выборкой по дате.
func toCargoWorkCalc(t time.Time, cutoff int) time.Time {
	h := t.Hour()
	if h >= cutoff {
		h -= cutoff
	} else {
		h += 24 - cutoff
	}
	return time.Date(t.Year(), t.Month(), t.Day(), h, t.Minute(), 0, 0, time.UTC)
}

// formatCargoWorkTime — расчётное время обратно в ЖД-представление «ДД.ММ.ГГ ЧЧ:ММ».
func formatCargoWorkTime(t time.Time, cutoff int) string {
	h := t.Hour()
	if h < 24-cutoff {
		h += cutoff
	} else {
		h -= 24 - cutoff
	}
	return fmt.Sprintf("%s %02d:%02d", t.Format("02.01.06"), h, t.Minute())
}

// formatCargoWorkDur — длительность как «Ч:ММ».
func formatCargoWorkDur(d time.Duration) string {
	m := int(d.Minutes())
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}

func addCargoWorkOp(a *CargoWorkAnalytics, op string, start, end time.Time, wagons int, train string, cutoff int) {
	a.Operations = append(a.Operations, CargoWorkOperation{
		StartTime: formatCargoWorkTime(start, cutoff),
		EndTime:   formatCargoWorkTime(end, cutoff),
		Operation: op,
		Wagons:    wagons,
		Duration:  formatCargoWorkDur(end.Sub(start)),
		TrainName: train,
	})
}

// sumCargoWorkIdle — суммарный простой порта за сутки (по операциям простоя).
func sumCargoWorkIdle(ops []CargoWorkOperation) string {
	var total time.Duration
	for _, op := range ops {
		if op.Operation != "Простой порта" {
			continue
		}
		var h, m int
		if _, err := fmt.Sscanf(op.Duration, "%d:%d", &h, &m); err != nil {
			continue
		}
		total += time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
	}
	return formatCargoWorkDur(total)
}
