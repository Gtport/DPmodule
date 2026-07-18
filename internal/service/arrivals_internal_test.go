package service

import (
	"testing"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

func histRow(id, vagon, indexPp, indexMain, stationNach, naznach, gruzpol string, prib time.Time) domain.VagonHistory {
	return domain.VagonHistory{
		ID: id, Vagon: vagon, IndexPp: indexPp, IndexMain: indexMain,
		StationNach: stationNach, StanNazn: "МЫС АСТАФЬЕВА",
		Naznach: naznach, GruzpolS: gruzpol,
		DatePrib: domain.NewLocalTime(prib), Otkl: "+01:49",
	}
}

// Группировка прибывших: группа = index_pp+date_prib, подгруппы по
// index_main/naznach/gruzpol_s, display в формате gtport.
func TestGroupArrivals(t *testing.T) {
	prib := time.Date(2026, 7, 17, 21, 11, 0, 0, time.UTC)
	rows := []domain.VagonHistory{
		histRow("1", "V1", "9379-783-9857", "9379-783-9857", "ЧЕЛУТАЙ", "АЭ", "АЭ", prib),
		histRow("2", "V2", "9379-783-9857", "9379-783-9857", "ЧЕЛУТАЙ", "АЭ", "АЭ", prib),
		// тот же поезд, переставленная подгруппа (чужой груз ГУТ-2 → АЭ)
		histRow("3", "V3", "9379-783-9857", "9379-782-9857", "ЧЕЛУТАЙ", "АЭ", "ГУТ-2", prib),
		// другой поезд (другой index_pp)
		histRow("4", "V4", "9853-008-9856", "9853-008-9856", "СЛЯБЫ", "ГУТ-2", "ГУТ-2", prib.Add(time.Hour)),
	}

	groups := groupArrivals(rows)
	if len(groups) != 2 {
		t.Fatalf("ожидалось 2 группы, получено %d", len(groups))
	}
	g := groups[0]
	if g.IndexPp != "9379-783-9857" || g.VagonCount != 3 || len(g.SubGroups) != 2 {
		t.Fatalf("группа 1: %+v", g)
	}
	if g.Otkl != "+01:49" {
		t.Errorf("otkl должен пробрасываться как есть, получено %q", g.Otkl)
	}
	var disp []string
	for _, sg := range g.SubGroups {
		disp = append(disp, sg.Display)
	}
	want := map[string]bool{
		"(2)-783-ЧЕЛУТАЙ АЭ":         false, // родная подгруппа
		"(1)-782-ЧЕЛУТАЙ ГУТ-2 → АЭ": false, // переставленный чужой груз
	}
	for _, d := range disp {
		if _, ok := want[d]; !ok {
			t.Errorf("неожиданный display %q", d)
		}
		want[d] = true
	}
	for d, seen := range want {
		if !seen {
			t.Errorf("не найден display %q (есть: %v)", d, disp)
		}
	}
}

// Дефолтный период — вчера/сегодня; кривые даты — ошибка; перепутанные — свап.
func TestArrivalsRange(t *testing.T) {
	from, to, err := arrivalsRange("", "")
	if err != nil {
		t.Fatalf("дефолт: %v", err)
	}
	if d := to.Time().Sub(from.Time()); d != 24*time.Hour {
		t.Errorf("дефолтный период должен быть сутки (вчера-сегодня), получено %v", d)
	}
	if _, _, err := arrivalsRange("2026-13-40", ""); err == nil {
		t.Error("кривая дата: ожидалась ошибка")
	}
	from, to, err = arrivalsRange("2026-07-18", "2026-07-17")
	if err != nil {
		t.Fatalf("свап: %v", err)
	}
	if !from.Time().Before(to.Time()) {
		t.Errorf("границы не свапнулись: from=%v to=%v", from, to)
	}
}
