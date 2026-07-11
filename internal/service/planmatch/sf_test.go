package planmatch

import (
	"testing"

	"github.com/Gtport/DPmodule/internal/domain"
)

// TestSFCandidates: кандидаты для с.ф. — только вагоны на станции синонима, только
// «наши» площадки, без занятых обычными нитками; агрегация по IdDisl с количеством.
func TestSFCandidates(t *testing.T) {
	sf := []SFRecord{{Sinonim: "БИКИН", Station: "БИКИН", Quantity: 50}}
	target := map[string]struct{}{"АЭ": {}, "ГУТ-2": {}}
	used := map[string]struct{}{"D5": {}} // занята обычной ниткой

	records := []domain.Dislocation{
		// группа D1: станция БИКИН, наш (ГУТ-2) — 2 вагона
		{Vagon: "111", StationOper: "БИКИН", Naznach: "ГУТ-2", Index: "9401-011-1234", IndexMain: "9401-011-1234", IdDisl: "D1"},
		{Vagon: "112", StationOper: "БИКИН", Naznach: "ГУТ-2", Index: "9401-011-1234", IndexMain: "9401-011-1234", IdDisl: "D1"},
		// группа D2: станция БИКИН, наш (АЭ) — 1 вагон
		{Vagon: "200", StationOper: "БИКИН", Naznach: "АЭ", Index: "9500-022-5678", IdDisl: "D2"},
		// чужая площадка (УТ-1) → исключить
		{Vagon: "300", StationOper: "БИКИН", Naznach: "УТ-1", IdDisl: "D3"},
		// другая станция → исключить
		{Vagon: "400", StationOper: "ПАРТИЗАНСК", Naznach: "ГУТ-2", IdDisl: "D4"},
		// IdDisl занят обычной ниткой → исключить
		{Vagon: "500", StationOper: "БИКИН", Naznach: "ГУТ-2", IdDisl: "D5"},
	}

	got := SFCandidates("БИКИН", sf, records, target, used)
	if len(got) != 2 {
		t.Fatalf("ожидалось 2 группы-кандидата, получено %d: %+v", len(got), got)
	}

	byID := map[string]SFGroup{}
	for _, g := range got {
		byID[g.IdDisl] = g
	}
	if g, ok := byID["D1"]; !ok || g.Quantity != 2 || len(g.Vagons) != 2 {
		t.Errorf("D1: ok=%v qty=%d vagons=%v", ok, g.Quantity, g.Vagons)
	}
	if g, ok := byID["D2"]; !ok || g.Quantity != 1 || g.Index != "9500-022-5678" {
		t.Errorf("D2: ok=%v qty=%d index=%q", ok, g.Quantity, g.Index)
	}
	for _, bad := range []string{"D3", "D4", "D5"} {
		if _, ok := byID[bad]; ok {
			t.Errorf("группа %s не должна быть кандидатом", bad)
		}
	}
}

// TestSFStationsAndUsed: синоним → станции; сбор занятых IdDisl из сматченных ниток.
func TestSFStationsAndUsed(t *testing.T) {
	sf := []SFRecord{
		{Sinonim: "ХАБАРОВСК II", Station: "ХАБАРОВСК II", Quantity: 50},
		{Sinonim: "ХАБАР-К I", Station: "ХАБАРОВСК II", Quantity: 50},
		{Sinonim: "БИКИН", Station: "БИКИН", Quantity: 50},
	}
	st := SFStations("хабар-к i", sf) // регистр не важен
	if _, ok := st["ХАБАРОВСК II"]; !ok || len(st) != 1 {
		t.Errorf("SFStations(ХАБАР-К I) = %v, ожидалась {ХАБАРОВСК II}", st)
	}

	used := UsedIdDisl([]NitkaMatch{
		{Matched: true, IdDisl: "A"},
		{Matched: false, IdDisl: "B"}, // не сматчена — не в used
		{Matched: true, IdDisl: ""},   // пустой — не в used
		{Matched: true, IdDisl: "C"},
	})
	if _, ok := used["A"]; !ok {
		t.Error("A должен быть занят")
	}
	if _, ok := used["C"]; !ok {
		t.Error("C должен быть занят")
	}
	if len(used) != 2 {
		t.Errorf("used=%v, ожидалось {A,C}", used)
	}
}
