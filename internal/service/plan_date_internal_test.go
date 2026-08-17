package service

// Гард даты плана подвода (случай 17.08.2026: файл с датой из будущего прошёл в снимок):
// checkPlanDate — дата плана обязана быть текущей МСК, после 18:00 допустима и
// завтрашняя (ЖД-сутки); fixPlanDates — сдвиг дат ниток на сегодня при согласии
// диспетчера («Исправить и загрузить»).

import (
	"testing"
	"time"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/parser/plan"
)

// planDocAt строит документ с одной датированной ниткой (PlanJd задан).
func planDocAt(jd time.Time) *plan.PlanDoc {
	return &plan.PlanDoc{Nitki: []plan.PlanNitka{{Index: "7438-011-1234", PlanJd: jd}}}
}

func TestCheckPlanDate_TodayOK(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC))
	defer restore()
	if w := checkPlanDate(planDocAt(time.Date(2026, 8, 18, 9, 30, 0, 0, time.UTC))); w != nil {
		t.Fatalf("сегодняшняя дата плана не должна давать предупреждение: %+v", w)
	}
}

func TestCheckPlanDate_FutureWarned(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC))
	defer restore()
	w := checkPlanDate(planDocAt(time.Date(2026, 8, 19, 9, 30, 0, 0, time.UTC)))
	if w == nil {
		t.Fatal("будущая дата плана до 18:00 обязана давать предупреждение")
	}
	if w.PlanDate != "19.08.2026" || w.Today != "18.08.2026" {
		t.Fatalf("даты предупреждения: %+v", w)
	}
}

func TestCheckPlanDate_PastWarned(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC))
	defer restore()
	if checkPlanDate(planDocAt(time.Date(2026, 8, 17, 9, 30, 0, 0, time.UTC))) == nil {
		t.Fatal("вчерашняя дата плана обязана давать предупреждение (решение владельца 18.08.2026)")
	}
}

// После 18:00 МСК идут следующие ЖД-сутки: план на завтра — норма, на сегодня — тоже.
func TestCheckPlanDate_EveningTomorrowOK(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 18, 19, 0, 0, 0, time.UTC))
	defer restore()
	if w := checkPlanDate(planDocAt(time.Date(2026, 8, 19, 21, 0, 0, 0, time.UTC))); w != nil {
		t.Fatalf("после 18:00 завтрашняя дата — норма (ЖД-сутки): %+v", w)
	}
	if w := checkPlanDate(planDocAt(time.Date(2026, 8, 18, 21, 0, 0, 0, time.UTC))); w != nil {
		t.Fatalf("после 18:00 сегодняшняя дата тоже норма: %+v", w)
	}
	if checkPlanDate(planDocAt(time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC))) == nil {
		t.Fatal("послезавтра — предупреждение и вечером")
	}
}

func TestCheckPlanDate_NoDatesSkipped(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC))
	defer restore()
	doc := &plan.PlanDoc{Nitki: []plan.PlanNitka{{Index: "7438-011-1234", PlanRaw: "не подводить"}}}
	if w := checkPlanDate(doc); w != nil {
		t.Fatalf("без датированных ниток сравнивать нечего: %+v", w)
	}
}

// Сдвиг «на сегодня»: все датированные времена ниток едут на одно и то же число суток
// (взаимные смещения многодневного плана сохраняются), нулевые времена не трогаются.
func TestFixPlanDates_ShiftsWholeDoc(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC))
	defer restore()
	doc := &plan.PlanDoc{Nitki: []plan.PlanNitka{
		{ // будущий блок «План на 25-08»: вечерняя нитка, PlanMsk уже с правилом ≥18
			Index:   "7438-011-1234",
			PlanJd:  time.Date(2026, 8, 25, 19, 22, 0, 0, time.UTC),
			PlanMsk: time.Date(2026, 8, 24, 19, 22, 0, 0, time.UTC),
			FactMsk: time.Date(2026, 8, 24, 20, 0, 0, 0, time.UTC),
		},
		{ // второй день многодневного плана
			Index:  "8600-793-9847",
			PlanJd: time.Date(2026, 8, 26, 9, 10, 0, 0, time.UTC),
		},
		{Index: "9370-101-9857", PlanRaw: "не подводить"}, // нитка без времени
	}}
	fixPlanDates(doc)

	if got := doc.Nitki[0].PlanJd; !got.Equal(time.Date(2026, 8, 18, 19, 22, 0, 0, time.UTC)) {
		t.Fatalf("PlanJd первой нитки: %v", got)
	}
	if got := doc.Nitki[0].PlanMsk; !got.Equal(time.Date(2026, 8, 17, 19, 22, 0, 0, time.UTC)) {
		t.Fatalf("PlanMsk обязан сдвинуться на те же 7 суток: %v", got)
	}
	if got := doc.Nitki[0].FactMsk; !got.Equal(time.Date(2026, 8, 17, 20, 0, 0, 0, time.UTC)) {
		t.Fatalf("FactMsk обязан сдвинуться: %v", got)
	}
	if got := doc.Nitki[1].PlanJd; !got.Equal(time.Date(2026, 8, 19, 9, 10, 0, 0, time.UTC)) {
		t.Fatalf("второй день плана обязан остаться днём +1: %v", got)
	}
	if !doc.Nitki[2].PlanJd.IsZero() {
		t.Fatalf("нитка без времени не должна получить дату: %v", doc.Nitki[2].PlanJd)
	}
	if w := checkPlanDate(doc); w != nil {
		t.Fatalf("после исправления гард обязан пропускать: %+v", w)
	}
}

func TestFixPlanDates_TodayNoop(t *testing.T) {
	restore := clock.SetForTest(time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC))
	defer restore()
	jd := time.Date(2026, 8, 18, 9, 30, 0, 0, time.UTC)
	doc := planDocAt(jd)
	fixPlanDates(doc)
	if !doc.Nitki[0].PlanJd.Equal(jd) {
		t.Fatalf("сегодняшний план сдвигаться не должен: %v", doc.Nitki[0].PlanJd)
	}
}
