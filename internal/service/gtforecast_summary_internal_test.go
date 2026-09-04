package service

import (
	"testing"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Агрегаты обстановки: корзины по часам хода у поездов в пути, брошенные
// отдельно (и из них у ворот), прибывшие не считаются; поезд — к терминалу
// большинства вагонов, вагоны — по подгруппам точно; нитки по ЖД-суткам от
// старта; ожидающие — из операций диаграмм; свободные нитки — только в Total;
// нагрузка плана на завтра = вагоны в нитках / сумма норм линий терминала.
func TestGtSnapshotSummary(t *testing.T) {
	lt := func(s string) *domain.LocalTime {
		tt, err := time.Parse("2006-01-02T15:04:05", s)
		if err != nil {
			t.Fatal(err)
		}
		v := domain.LocalTime(tt)
		return &v
	}
	fp := func(v float64) *float64 { return &v }
	start := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

	res := GtSimulateDTO{
		Trains: []GtTrainDTO{
			{Index: "A", Status: "2", ToGo: fp(10), PlanJd: lt("2026-09-05T10:00:00"), VagonCount: 71,
				SubGroups: []GtSubGroupDTO{{Naznach: "АЭ", VagonCount: 71}}},
			{Index: "B", Status: "2", ToGo: fp(40), PlanJd: lt("2026-09-06T03:00:00"), VagonCount: 60,
				SubGroups: []GtSubGroupDTO{{Naznach: "ГУТ-2", VagonCount: 60}}},
			{Index: "C", Status: "5", ToGo: fp(20), VagonCount: 65,
				SubGroups: []GtSubGroupDTO{{Naznach: "АЭ", VagonCount: 65}}},
			{Index: "D", Status: "history", IsArrived: true, VagonCount: 70,
				SubGroups: []GtSubGroupDTO{{Naznach: "АЭ", VagonCount: 70}}},
			{Index: "E", Status: "2", ToGo: fp(100), VagonCount: 70,
				SubGroups: []GtSubGroupDTO{{Naznach: "АЭ", VagonCount: 40}, {Naznach: "ГУТ-2", VagonCount: 30}}},
			{Index: "F", Status: "2", ToGo: fp(200), VagonCount: 50,
				SubGroups: []GtSubGroupDTO{{Naznach: "ГУТ-2", VagonCount: 50}}},
		},
		Flows: []GtFlowDTO{
			{Terminal: "АЭ", CargoKey: "", Days: []GtDayDTO{
				{Date: "2026-09-05", NormSpeed: 144, Arrival: 71, Remaining: 20, TotalWaitMin: 300,
					CarriedOver: []GtCarriedDTO{{Index: "C", Wagons: 65}},
					Operations: []GtOperationDTO{{TrainIndex: "A", WaitMin: 30}, {TrainIndex: "X", IsRemainder: true, WaitMin: 99}}},
				{Date: "2026-09-06", NormSpeed: 144, Arrival: 40},
			}},
			{Terminal: "ГУТ-2", CargoKey: "УГОЛЬ", Days: []GtDayDTO{
				{Date: "2026-09-05", NormSpeed: 168, Arrival: 60},
				{Date: "2026-09-06", NormSpeed: 168, Arrival: 65},
			}},
		},
		FreeSlots: []GtFreeSlotDTO{
			{Jd: *lt("2026-09-05T12:00:00")}, {Jd: *lt("2026-09-06T01:00:00")},
			{Jd: *lt("2026-09-06T15:00:00")}, {Jd: *lt("2026-09-07T02:00:00")},
		},
	}

	h10, h3 := 10.0, 3.4
	gate := []GtGateTrainDTO{
		{Index: "G1", Terminal: "АЭ", VagonCount: 71, Phase: GtGateOnFront, HoursAtStation: &h10},
		{Index: "G2", Terminal: "АЭ", VagonCount: 60, Phase: GtGateWaitingFeed, HoursAtStation: &h3},
		{Index: "G3", Terminal: "ГУТ-2", VagonCount: 55, Phase: GtGateWaitingFeed},
	}
	s := gtSnapshotSummary(start, res, gate)
	tot := s.Total
	check := func(name string, got, want int) {
		t.Helper()
		if got != want {
			t.Errorf("%s: %d, ожидалось %d", name, got, want)
		}
	}
	check("total.TrainsQueue", tot.TrainsQueue, 5)
	check("total.WagonsQueue", tot.WagonsQueue, 316)
	check("total.TrainsGate", tot.TrainsGate, 1)
	check("total.WagonsGate", tot.WagonsGate, 71)
	check("total.TrainsWave", tot.TrainsWave, 1)
	check("total.WagonsWave", tot.WagonsWave, 60)
	check("total.Trains3to7", tot.Trains3to7, 1)
	check("total.TrainsFar", tot.TrainsFar, 1)
	check("total.TrainsBros", tot.TrainsBros, 1)
	check("total.WagonsBros", tot.WagonsBros, 65)
	check("total.TrainsBrosGate", tot.TrainsBrosGate, 1)
	check("total.TrainsWaiting", tot.TrainsWaiting, 1)
	check("total.NitkiDay0", tot.NitkiDay0, 1)
	check("total.NitkiDay1", tot.NitkiDay1, 1)
	check("total.WagonsNitkiDay1", tot.WagonsNitkiDay1, 60)
	check("total.ArrivalDay0", tot.ArrivalDay0, 131)
	check("total.ArrivalDay1", tot.ArrivalDay1, 105)
	check("total.RemainingDay0", tot.RemainingDay0, 20)
	check("total.CarriedDay0", tot.CarriedDay0, 1)
	check("total.IdleMinDay0", tot.IdleMinDay0, 300)
	check("total.NormSpeed", tot.NormSpeed, 312)
	if tot.PlanLoadDay1 != 0.19 {
		t.Errorf("total.PlanLoadDay1 %v, ожидалось 0.19 (60/312)", tot.PlanLoadDay1)
	}
	check("total.FreeSlotsH0", tot.FreeSlotsH0, 1)
	check("total.FreeSlotsH1", tot.FreeSlotsH1, 2)
	check("total.FreeSlotsH2", tot.FreeSlotsH2, 1)
	check("total.TrainsAtStation", tot.TrainsAtStation, 3)
	check("total.WagonsAtStation", tot.WagonsAtStation, 186)
	check("total.TrainsOnFront", tot.TrainsOnFront, 1)
	check("total.TrainsWaitingFeed", tot.TrainsWaitingFeed, 2)
	if tot.MaxHoursAtStation != 10 {
		t.Errorf("total.MaxHoursAtStation %v, ожидалось 10", tot.MaxHoursAtStation)
	}

	ae, ok := s.ByTerminal["АЭ"]
	if !ok {
		t.Fatal("нет строки АЭ")
	}
	check("АЭ.TrainsQueue", ae.TrainsQueue, 3) // A, C, E (большинство вагонов — АЭ)
	check("АЭ.WagonsQueue", ae.WagonsQueue, 176)
	check("АЭ.TrainsGate", ae.TrainsGate, 1)
	check("АЭ.Trains3to7", ae.Trains3to7, 1)
	check("АЭ.TrainsBros", ae.TrainsBros, 1)
	check("АЭ.WagonsBros", ae.WagonsBros, 65)
	check("АЭ.TrainsWaiting", ae.TrainsWaiting, 1)
	check("АЭ.NitkiDay0", ae.NitkiDay0, 1)
	check("АЭ.ArrivalDay0", ae.ArrivalDay0, 71)
	check("АЭ.ArrivalDay1", ae.ArrivalDay1, 40)
	check("АЭ.RemainingDay0", ae.RemainingDay0, 20)
	check("АЭ.CarriedDay0", ae.CarriedDay0, 1)
	check("АЭ.IdleMinDay0", ae.IdleMinDay0, 300)
	check("АЭ.NormSpeed", ae.NormSpeed, 144)
	check("АЭ.FreeSlotsH1 (только Total)", ae.FreeSlotsH1, 0)
	check("АЭ.TrainsAtStation", ae.TrainsAtStation, 2)
	check("АЭ.WagonsAtStation", ae.WagonsAtStation, 131)
	check("АЭ.TrainsOnFront", ae.TrainsOnFront, 1)
	check("ГУТ-2.TrainsWaitingFeed", s.ByTerminal["ГУТ-2"].TrainsWaitingFeed, 1)

	gut := s.ByTerminal["ГУТ-2"]
	check("ГУТ-2.TrainsQueue", gut.TrainsQueue, 2) // B, F
	check("ГУТ-2.WagonsQueue", gut.WagonsQueue, 140) // 60 + 30 (часть E) + 50
	check("ГУТ-2.TrainsWave", gut.TrainsWave, 1)
	check("ГУТ-2.WagonsWave", gut.WagonsWave, 60)
	check("ГУТ-2.TrainsFar", gut.TrainsFar, 1)
	check("ГУТ-2.NitkiDay1", gut.NitkiDay1, 1)
	check("ГУТ-2.WagonsNitkiDay1", gut.WagonsNitkiDay1, 60)
	if gut.PlanLoadDay1 != 0.36 {
		t.Errorf("ГУТ-2.PlanLoadDay1 %v, ожидалось 0.36 (60/168)", gut.PlanLoadDay1)
	}
	check("ГУТ-2.ArrivalDay1", gut.ArrivalDay1, 65)
}
