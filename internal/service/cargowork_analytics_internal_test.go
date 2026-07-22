package service

import (
	"testing"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Golden-тесты движка суточной аналитики «Грузовой работы».
//
// Ожидаемые значения получены ПРОГОНОМ ЭТАЛОННОГО КОДА gtport
// (analytics/port_analytics_service.go) на тех же входах, а не выведены
// вручную — включая его особенности, которые перенесены намеренно:
//
//   · при незавершённой выгрузке в «полное образование» идёт ВЕСЬ поданный
//     объём (остаток/поезд целиком), а в «полезное» — только успетое;
//   · поезд, до которого сутки не дошли, всё равно даёт нулевую операцию
//     выгрузки (см. «чугун: остаток съедает сутки»);
//   · простой короче 15 минут в операции не пишется;
//   · времена печатаются в ЖД-представлении, дата при этом НЕ переносится —
//     поэтому «21:20 → 07:50» внутри одних расчётных суток это норма.
//
// Порядок внутри суток проверяет TestCalcCargoWorkDay_JdDayOrder: он и есть
// страховка инварианта «на вход идёт date_prib (ЖД-штамп), выборка — по
// date_prib_d». Если кто-то начнёт кормить движок сырым time_op, тест упадёт.

// cwDay — дата учётных суток тестов.
func cwDay() time.Time { return time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC) }

// cwTrain — поезд со штампом прибытия из истории (date_prib, ЖД-сдвинутый:
// час ≥ 18 означает прибытие ВЧЕРАШНИМ вечером в эти же ЖД-сутки).
func cwTrain(name string, wagons, hour, min int) CargoWorkTrain {
	return CargoWorkTrain{
		Name:    name,
		Wagons:  wagons,
		Arrival: domain.LocalTime(time.Date(2026, 7, 20, hour, min, 0, 0, time.UTC)),
	}
}

func TestCalcCargoWorkDay_GoldenAE(t *testing.T) {
	// АЭ: остаток 20 + два поезда по 63, способность 144 ваг/сут.
	got := calcCargoWorkDay(cwDay(), 20, 144, 18, []CargoWorkTrain{
		cwTrain("1234-567-8901", 63, 19, 22),
		cwTrain("2234-567-8902", 63, 8, 15),
	})

	if got.UsefulFormation != 141 || got.TotalFormation != 146 {
		t.Errorf("образование = %d/%d, эталон gtport 141/146",
			got.UsefulFormation, got.TotalFormation)
	}
	if got.Downtime != "0:25" {
		t.Errorf("простой = %q, эталон gtport %q", got.Downtime, "0:25")
	}

	want := []CargoWorkOperation{
		{"20.07.26 18:00", "20.07.26 21:20", "Выгрузка остатка", 20, "3:20", ""},
		{"20.07.26 21:20", "20.07.26 07:50", "Выгрузка 1234-567-8901", 63, "10:30", "1234-567-8901"},
		{"20.07.26 07:50", "20.07.26 08:15", "Простой порта", 0, "0:25", ""},
		{"20.07.26 08:15", "21.07.26 18:00", "Выгрузка 2234-567-8902", 58, "9:45", "2234-567-8902"},
	}
	assertCargoWorkOps(t, got.Operations, want)

	// Первый поезд прибыл, пока фронт добивал остаток, — ждал 1:58.
	if len(got.Waits) != 1 {
		t.Fatalf("ожиданий = %d, эталон gtport 1", len(got.Waits))
	}
	if got.Waits[0].TrainName != "1234-567-8901" || got.Waits[0].WaitDuration != "1:58" {
		t.Errorf("ожидание = %s/%s, эталон gtport 1234-567-8901/1:58",
			got.Waits[0].TrainName, got.Waits[0].WaitDuration)
	}
}

func TestCalcCargoWorkDay_GoldenUT(t *testing.T) {
	// УТ-1: без остатка, три поезда по 72, способность 432 — фронт быстрее подвода.
	got := calcCargoWorkDay(cwDay(), 0, 432, 18, []CargoWorkTrain{
		cwTrain("A", 72, 20, 0),
		cwTrain("B", 72, 2, 0),
		cwTrain("C", 72, 10, 30),
	})

	if got.UsefulFormation != 216 || got.TotalFormation != 216 {
		t.Errorf("образование = %d/%d, эталон gtport 216/216",
			got.UsefulFormation, got.TotalFormation)
	}
	if got.Downtime != "12:00" {
		t.Errorf("простой = %q, эталон gtport %q", got.Downtime, "12:00")
	}
	if len(got.Waits) != 0 {
		t.Errorf("ожиданий = %d, эталон gtport 0 (фронт всегда свободен)", len(got.Waits))
	}

	want := []CargoWorkOperation{
		{"20.07.26 18:00", "20.07.26 20:00", "Простой порта", 0, "2:00", ""},
		{"20.07.26 20:00", "20.07.26 00:00", "Выгрузка A", 72, "4:00", "A"},
		{"20.07.26 00:00", "20.07.26 02:00", "Простой порта", 0, "2:00", ""},
		{"20.07.26 02:00", "20.07.26 06:00", "Выгрузка B", 72, "4:00", "B"},
		{"20.07.26 06:00", "20.07.26 10:30", "Простой порта", 0, "4:30", ""},
		{"20.07.26 10:30", "20.07.26 14:30", "Выгрузка C", 72, "4:00", "C"},
		{"20.07.26 14:30", "21.07.26 18:00", "Простой порта", 0, "3:30", ""},
	}
	assertCargoWorkOps(t, got.Operations, want)
}

func TestCalcCargoWorkDay_GoldenOstatokEatsDay(t *testing.T) {
	// ГУТ-2 чугун: остаток 45 при способности 30 — сутки уходят на него целиком,
	// поезд не начат, но операция с нулём вагонов эмитится (особенность gtport).
	got := calcCargoWorkDay(cwDay(), 45, 30, 18, []CargoWorkTrain{
		cwTrain("CH-1", 10, 9, 0),
	})

	if got.UsefulFormation != 30 || got.TotalFormation != 55 {
		t.Errorf("образование = %d/%d, эталон gtport 30/55",
			got.UsefulFormation, got.TotalFormation)
	}
	if got.Downtime != "0:00" {
		t.Errorf("простой = %q, эталон gtport %q", got.Downtime, "0:00")
	}

	want := []CargoWorkOperation{
		{"20.07.26 18:00", "21.07.26 18:00", "Выгрузка остатка", 30, "24:00", ""},
		{"21.07.26 18:00", "21.07.26 18:00", "Выгрузка CH-1", 0, "0:00", "CH-1"},
	}
	assertCargoWorkOps(t, got.Operations, want)
}

func TestCalcCargoWorkDay_GoldenEmptyDay(t *testing.T) {
	// Пустые сутки: ни остатка, ни поездов — сплошной простой.
	got := calcCargoWorkDay(cwDay(), 0, 144, 18, nil)

	if got.UsefulFormation != 0 || got.TotalFormation != 0 {
		t.Errorf("образование = %d/%d, эталон gtport 0/0",
			got.UsefulFormation, got.TotalFormation)
	}
	if got.Downtime != "24:00" {
		t.Errorf("простой = %q, эталон gtport %q", got.Downtime, "24:00")
	}
	assertCargoWorkOps(t, got.Operations, []CargoWorkOperation{
		{"20.07.26 18:00", "21.07.26 18:00", "Простой порта", 0, "24:00", ""},
	})
}

func TestCalcCargoWorkDay_GoldenOutOfDayDropped(t *testing.T) {
	// Поезд с прибытием вне расчётных суток в расчёт не берётся.
	got := calcCargoWorkDay(cwDay(), 0, 144, 18, []CargoWorkTrain{
		cwTrain("IN", 30, 12, 0),
		{Name: "OUT", Wagons: 30, Arrival: domain.LocalTime(
			time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC))},
	})

	if got.UsefulFormation != 30 || got.TotalFormation != 30 {
		t.Errorf("образование = %d/%d, эталон gtport 30/30 (OUT отброшен)",
			got.UsefulFormation, got.TotalFormation)
	}
	if got.Downtime != "19:00" {
		t.Errorf("простой = %q, эталон gtport %q", got.Downtime, "19:00")
	}
	for _, op := range got.Operations {
		if op.TrainName == "OUT" {
			t.Fatalf("поезд вне суток попал в операции: %+v", op)
		}
	}
}

// Способность не настроена (pc пуст и в линии, и в ports) — выгружать нечем.
// Отход от gtport: там деление на ноль давало бесконечные длительности.
func TestCalcCargoWorkDay_NoCapacity(t *testing.T) {
	got := calcCargoWorkDay(cwDay(), 10, 0, 18, []CargoWorkTrain{cwTrain("A", 50, 10, 0)})

	if got.UsefulFormation != 0 || got.TotalFormation != 0 {
		t.Errorf("образование = %d/%d, ожидалось 0/0", got.UsefulFormation, got.TotalFormation)
	}
	if got.Downtime != "24:00" {
		t.Errorf("простой = %q, ожидались полные сутки простоя", got.Downtime)
	}
}

// Час отсечки — параметр, а не константа: при cutoff=0 расчётные сутки совпадают
// с календарными и ЖД-представление ничего не сдвигает.
func TestCalcCargoWorkDay_CutoffIsParameter(t *testing.T) {
	got := calcCargoWorkDay(cwDay(), 0, 240, 6, []CargoWorkTrain{cwTrain("A", 10, 7, 0)})

	if len(got.Operations) == 0 {
		t.Fatal("операций нет")
	}
	if got.Operations[0].StartTime != "20.07.26 06:00" {
		t.Errorf("начало суток = %q, при cutoff=6 ожидалось %q",
			got.Operations[0].StartTime, "20.07.26 06:00")
	}
}

// Порядок обслуживания внутри ЖД-суток — хронологический, а не по часам суток.
// Вагон с date_prib 20.07 19:22 реально прибыл 19-го вечером (штамп сдвинут
// правилом «час ≥ 18 → +сутки») и должен обслуживаться ПЕРВЫМ, раньше вагона с
// 20.07 08:15. Это же удерживает выборку поездов на date_prib_d.
func TestCalcCargoWorkDay_JdDayOrder(t *testing.T) {
	got := calcCargoWorkDay(cwDay(), 0, 240, 18, []CargoWorkTrain{
		cwTrain("УТРО", 10, 8, 15),
		cwTrain("ВЕЧЕР", 10, 19, 22),
	})

	var order []string
	for _, op := range got.Operations {
		if op.TrainName != "" {
			order = append(order, op.TrainName)
		}
	}
	if len(order) != 2 {
		t.Fatalf("выгрузок = %d, ожидалось 2: %+v", len(order), got.Operations)
	}
	if order[0] != "ВЕЧЕР" || order[1] != "УТРО" {
		t.Errorf("порядок = %v, ожидался [ВЕЧЕР УТРО]: вечернее прибытие "+
			"открывает ЖД-сутки, утреннее идёт следом", order)
	}
}

func assertCargoWorkOps(t *testing.T, got, want []CargoWorkOperation) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("операций = %d, эталон gtport %d:\n got = %+v\nwant = %+v",
			len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("операция %d:\n got = %+v\nwant = %+v", i, got[i], want[i])
		}
	}
}
