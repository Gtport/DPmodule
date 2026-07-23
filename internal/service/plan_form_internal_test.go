package service

import (
	"context"
	"testing"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

func pfLT(y, m, d, hh, mm int) *domain.LocalTime {
	v := domain.LocalTime(time.Date(y, time.Month(m), d, hh, mm, 0, 0, time.UTC))
	return &v
}

func ipf(i int) *int { return &i }

// TestIndexPart — середина индекса (порт gtport) для дефисного 4-3-4 и с.ф.
func TestIndexPart(t *testing.T) {
	cases := map[string]string{
		"8631-877-9847": "877",           // 4-3-4 → середина
		"8643-904-9420": "904",           //
		"с.ф.НАХОДКА":   "с.ф.НАХОДКА",    // не цифра на байте 5 → целиком
		"784":           "784",           // короткий → целиком
	}
	for in, want := range cases {
		if got := indexPart(in); got != want {
			t.Errorf("indexPart(%q) = %q, ждали %q", in, got, want)
		}
	}
}

// TestSubDisplay — подгруппа: «(N) середина SMS от терминал».
func TestSubDisplay(t *testing.T) {
	// чужой груз (gruzpol ≠ naznach) — с «от»
	got := subDisplay("8643-175-9420", "8643-904-9420", "ЛК-1", "АЭ", "ГУТ-2", 13)
	if got != "(13) 175 ЛК-1 от ГУТ-2" {
		t.Errorf("got %q", got)
	}
	// свой груз (gruzpol == naznach) — без «от»
	got = subDisplay("8643-784-9420", "8643-904-9420", "Челутай", "АЭ", "АЭ", 9)
	if got != "(9) 784 Челутай" {
		t.Errorf("got %q", got)
	}
	// index_main == index_pp — середину не дублируем
	got = subDisplay("8643-904-9420", "8643-904-9420", "Челутай", "АЭ", "АЭ", 63)
	if got != "(63) Челутай" {
		t.Errorf("got %q", got)
	}
}

// TestTrainDisplay — полная строка из скриншота gtport.
func TestTrainDisplay(t *testing.T) {
	tr := &pfTrain{
		indexPp: "8643-904-9420", arrived: true,
		t: time.Date(2026, 7, 22, 19, 23, 0, 0, time.UTC),
		subs: []*pfSub{
			{indexMain: "8643-175-9420", sms1: "ЛК-1", naznach: "АЭ", gruzpol: "ГУТ-2", count: 13},
			{indexMain: "8643-784-9420", sms1: "Челутай", naznach: "АЭ", gruzpol: "АЭ", count: 9},
		},
	}
	want := "904 - приб 19:23 (13) 175 ЛК-1 от ГУТ-2, (9) 784 Челутай"
	if got := trainDisplay(tr); got != want {
		t.Errorf("trainDisplay =\n  %q\nждали\n  %q", got, want)
	}

	// плановый (не «приб»), с.ф. целиком
	sf := &pfTrain{
		indexPp: "с.ф.НАХОДКА", arrived: false,
		t:    time.Date(2026, 7, 23, 12, 5, 0, 0, time.UTC),
		subs: []*pfSub{{indexMain: "8600-098-9420", sms1: "ПЗ", naznach: "АЭ", gruzpol: "ГУТ-2", count: 6}},
	}
	if got := trainDisplay(sf); got != "с.ф.НАХОДКА - 12:05 (6) 098 ПЗ от ГУТ-2" {
		t.Errorf("с.ф.: got %q", got)
	}
}

// TestPlanTrains — плановые из снимка: фильтр (не прибывшие, свой терминал, есть
// плановое время, не раньше суток), группировка по индексу+дате.
func TestPlanTrains(t *testing.T) {
	start := dayStart(time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC))
	rows := []domain.Dislocation{
		// план на сегодня (АЭ) — 2 вагона
		{Vagon: "1", Naznach: "АЭ", Index: "8643-176-9420", IndexMain: "8643-176-9420", Sms1: "ЛК-1", GruzpolS: "ГУТ-2", CargoGroup: "УГОЛЬ", Status: ipf(2), PlanJd: pfLT(2026, 7, 23, 9, 0)},
		{Vagon: "2", Naznach: "АЭ", Index: "8643-176-9420", IndexMain: "8643-176-9420", Sms1: "ЛК-1", GruzpolS: "ГУТ-2", CargoGroup: "УГОЛЬ", Status: ipf(2), PlanJd: pfLT(2026, 7, 23, 9, 0)},
		// прибывший (10) — не план
		{Vagon: "3", Naznach: "АЭ", Index: "8643-789-9420", Status: ipf(10), PlanJd: pfLT(2026, 7, 23, 0, 19)},
		// без планового времени — пропуск
		{Vagon: "4", Naznach: "АЭ", Index: "8643-999-9420", Status: ipf(2)},
		// только расчёт, без плана — НЕ в плане подвода, пропуск
		{Vagon: "6", Naznach: "АЭ", Index: "8643-888-9420", Status: ipf(2), RaschJd: pfLT(2026, 7, 23, 11, 0)},
		// чужой терминал — пропуск
		{Vagon: "5", Naznach: "ГУТ-2", Index: "8643-880-9420", Status: ipf(2), PlanJd: pfLT(2026, 7, 23, 10, 0)},
	}
	cache := NewActualCache(s9StubDisl{items: rows})
	if err := cache.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := &PlanFormService{actual: cache}
	trains := svc.planTrains("АЭ", start)

	if len(trains) != 1 {
		t.Fatalf("ждали 1 плановый поезд, got %d", len(trains))
	}
	tr := trains[0]
	if tr.indexPp != "8643-176-9420" || tr.arrived {
		t.Fatalf("поезд неверный: %+v", tr)
	}
	if got := trainDisplay(tr); got != "176 - 09:00 (2) ЛК-1 от ГУТ-2" {
		t.Errorf("display = %q", got)
	}
	if lt := lineTrains(trains, "УГОЛЬ", "2026-07-23"); len(lt) != 1 || lt[0].Wagons != 2 {
		t.Errorf("lineTrains(УГОЛЬ) = %+v", lt)
	}
}

// TestBuildDaysSort — сортировка внутри ЖД-суток: 18:00–23:59 раньше 00:00–17:59
// (отсечка 18). Пример АТТИС: 19:23, 22:08, 15:06 (позиции 01:23, 04:08, 21:06).
func TestBuildDaysSort(t *testing.T) {
	mk := func(idx string, hh, mm int) *pfTrain {
		return newTrain(idx, true, time.Date(2026, 7, 22, hh, mm, 0, 0, time.UTC))
	}
	days := buildDays([]*pfTrain{mk("8643-150-9420", 15, 6), mk("8643-190-9420", 19, 23), mk("8643-220-9420", 22, 8)}, 18)
	if len(days) != 1 {
		t.Fatalf("ждали 1 день, got %d", len(days))
	}
	got := []string{days[0].Trains[0][:3], days[0].Trains[1][:3], days[0].Trains[2][:3]}
	want := []string{"190", "220", "150"} // 19:23 → 22:08 → 15:06 по ЖД-порядку
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ЖД-порядок = %v, ждали %v", got, want)
		}
	}
}
