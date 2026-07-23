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

func TestDominantCargo(t *testing.T) {
	if g := dominantCargo(map[string]int{"УГОЛЬ": 40, "МЕТАЛЛ": 5}); g != "УГОЛЬ" {
		t.Fatalf("ждали УГОЛЬ, got %q", g)
	}
	if g := dominantCargo(map[string]int{}); g != "" {
		t.Fatalf("пусто → '', got %q", g)
	}
}

// TestApproaching — фильтр и свёртка подхода: только не-прибывшие своего терминала,
// группировка по IdDisl, счёт по группе груза, время из прогноза.
func TestApproaching(t *testing.T) {
	rows := []domain.Dislocation{
		// поезд A (АЭ, в пути) — 2 вагона угля
		{Vagon: "1", Naznach: "АЭ", IdDisl: "A", Index: "783", Status: ipf(2), CargoGroup: "УГОЛЬ", ProgJd: pfLT(2026, 7, 23, 14, 15)},
		{Vagon: "2", Naznach: "АЭ", IdDisl: "A", Index: "783", Status: ipf(2), CargoGroup: "УГОЛЬ", ProgJd: pfLT(2026, 7, 23, 14, 15)},
		// прибывший (статус 10) — не подход
		{Vagon: "3", Naznach: "АЭ", IdDisl: "B", Index: "784", Status: ipf(10), CargoGroup: "УГОЛЬ", ProgJd: pfLT(2026, 7, 23, 10, 0)},
		// чужой терминал — мимо
		{Vagon: "4", Naznach: "ГУТ-2", IdDisl: "C", Index: "900", Status: ipf(2), CargoGroup: "УГОЛЬ", ProgJd: pfLT(2026, 7, 23, 9, 0)},
	}
	cache := NewActualCache(s9StubDisl{items: rows})
	if err := cache.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := &PlanFormService{actual: cache}
	ap := svc.approaching("АЭ")

	if len(ap.trains) != 1 {
		t.Fatalf("ждали 1 подходящий поезд (A), got %d", len(ap.trains))
	}
	if ap.trains[0].index != "783" || ap.trains[0].total != 2 || ap.trains[0].cargo != "УГОЛЬ" {
		t.Fatalf("свёртка поезда A неверна: %+v", ap.trains[0])
	}
	// Для аналитики линии «УГОЛЬ» — один поезд на 2 вагона.
	tl := ap.trainsForLine("УГОЛЬ")
	if len(tl) != 1 || tl[0].Wagons != 2 || tl[0].Name != "783" {
		t.Fatalf("trainsForLine(УГОЛЬ) неверно: %+v", tl)
	}
	// Линия без разбивки ('') — все вагоны поезда.
	if all := ap.trainsForLine(""); len(all) != 1 || all[0].Wagons != 2 {
		t.Fatalf("trainsForLine('') неверно: %+v", all)
	}
	// Металла в подходе нет.
	if m := ap.trainsForLine("МЕТАЛЛ"); len(m) != 0 {
		t.Fatalf("металла не должно быть, got %+v", m)
	}
}

// TestTrainList — список поездов: приб + подход, приб помечены, сортировка по времени.
func TestTrainList(t *testing.T) {
	arrived := []domain.VagonHistory{
		{IndexPp: "783", DatePrib: pfLT(2026, 7, 23, 8, 0), CargoGroup: "УГОЛЬ"},
		{IndexPp: "783", DatePrib: pfLT(2026, 7, 23, 8, 0), CargoGroup: "УГОЛЬ"},
	}
	ap := approachingSet{trains: []approachingTrain{
		{index: "790", total: 5, cargo: "УГОЛЬ", time: pfLT(2026, 7, 23, 16, 40)},
	}}
	svc := &PlanFormService{}
	list := svc.trainList(arrived, ap)

	if len(list) != 2 {
		t.Fatalf("ждали 2 поезда (приб 783 + подход 790), got %d", len(list))
	}
	// Сортировка по времени: 08:00 приб раньше 16:40 подход.
	if !list[0].Arrived || list[0].Index != "783" || list[0].Count != 2 {
		t.Fatalf("первый — приб 783 (2 ваг), got %+v", list[0])
	}
	if list[1].Arrived || list[1].Index != "790" || list[1].Count != 5 {
		t.Fatalf("второй — подход 790 (5 ваг), got %+v", list[1])
	}
}
