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

	got := SFCandidates("БИКИН", sf, records, target, used, nil)
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

// TestSFCandidates_Departed: сформированные сборные ловятся по префиксу индекса
// (АААА = kod_4 станции с.ф.) — уехавшие (Departed) и ещё стоящие на станции
// формирования (Formed); идут первыми (уехавшие → сформированные → группы);
// прибывшие/порожние/чужие префиксы/занятые — не предлагаются.
func TestSFCandidates_Departed(t *testing.T) {
	sf := []SFRecord{{Sinonim: "БИКИН", Station: "БИКИН", Quantity: 50}}
	target := map[string]struct{}{"АЭ": {}}
	// У станции может быть несколько kod_4 (парки с одним именем) — ловим по любому.
	kod4 := map[string][]string{"БИКИН": {"9401", "9402"}}
	used := map[string]struct{}{"D9": {}}
	s2, s10 := 2, 10

	records := []domain.Dislocation{
		// сформирован (индекс со «своим» префиксом), но ещё на станции формирования
		{Vagon: "100", StationOper: "БИКИН", Naznach: "АЭ", Index: "9401-011-9857", IdDisl: "D0", Status: &s2},
		// несобранная группа на станции формирования (индекс прибывшего поезда)
		{Vagon: "050", StationOper: "БИКИН", Naznach: "АЭ", Index: "7777-005-9722", IdDisl: "D7", Status: &s2},
		// уехал: в пути, индекс с префиксом БИКИНа — кандидат Departed
		{Vagon: "111", StationOper: "ХАБАРОВСК II", Naznach: "АЭ", Index: "9401-055-9857", IndexMain: "9401-055-9857", IdDisl: "D1", Status: &s2},
		{Vagon: "112", StationOper: "ХАБАРОВСК II", Naznach: "АЭ", Index: "9401-055-9857", IndexMain: "9401-055-9857", IdDisl: "D1", Status: &s2},
		// переформирован в пути: текущий индекс чужой, IndexMain хранит исходный — кандидат
		{Vagon: "200", StationOper: "РУЖИНО", Naznach: "АЭ", Index: "8888-001-9857", IndexMain: "9401-077-9857", IdDisl: "D2", Status: &s2},
		// прибыл (10) → не предлагается
		{Vagon: "300", StationOper: "МЫС АСТАФЬЕВА", Naznach: "АЭ", Index: "9401-099-9857", IdDisl: "D3", Status: &s10},
		// порожний → не предлагается
		{Vagon: "400", StationOper: "РУЖИНО", Naznach: "АЭ", Index: "9401-100-9857", IdDisl: "D4", Status: &s2, PorozhPriznak: "1"},
		// чужой префикс → не предлагается
		{Vagon: "500", StationOper: "РУЖИНО", Naznach: "АЭ", Index: "7777-001-9857", IdDisl: "D5", Status: &s2},
		// занят обычной ниткой → не предлагается
		{Vagon: "600", StationOper: "РУЖИНО", Naznach: "АЭ", Index: "9401-200-9857", IdDisl: "D9", Status: &s2},
		// уехал со ВТОРОГО парка станции (другой kod_4 того же имени) — кандидат
		{Vagon: "700", StationOper: "РУЖИНО", Naznach: "АЭ", Index: "9402-033-9857", IdDisl: "D6", Status: &s2},
	}

	got := SFCandidates("БИКИН", sf, records, target, used, kod4)
	if len(got) != 5 {
		t.Fatalf("ожидалось 5 групп (уехавшие D1, D2, D6 + сформированный D0 + группа D7), получено %d: %+v", len(got), got)
	}
	// Порядок: уехавшие → сформированный → несобранная группа.
	for i, want := range []string{"D1", "D2", "D6", "D0", "D7"} {
		// D1/D2/D6 сортируются между собой по дате/станции/индексу — проверяем классы.
		_ = want
		switch {
		case i < 3 && !got[i].Departed:
			t.Errorf("позиция %d: ожидался уехавший, получено %+v", i, got[i])
		case i == 3 && (!got[i].Formed || got[i].Departed):
			t.Errorf("позиция 3: ожидался сформированный на станции, получено %+v", got[i])
		case i == 4 && (got[i].Formed || got[i].Departed):
			t.Errorf("позиция 4: ожидалась несобранная группа, получено %+v", got[i])
		}
	}
	byID := map[string]SFGroup{}
	for _, g := range got {
		byID[g.IdDisl] = g
	}
	if g := byID["D1"]; !g.Departed || g.Quantity != 2 || g.StationOper != "ХАБАРОВСК II" {
		t.Errorf("D1: %+v", g)
	}
	if g := byID["D2"]; !g.Departed || g.Index != "9401-077-9857" {
		t.Errorf("D2 (переформирован, IndexMain): %+v", g)
	}
	if g := byID["D6"]; !g.Departed || g.Index != "9402-033-9857" {
		t.Errorf("D6 (второй парк станции, kod_4 9402): %+v", g)
	}
	if g := byID["D0"]; g.Departed || !g.Formed {
		t.Errorf("D0 сформирован, но на станции формирования — Formed, не Departed: %+v", g)
	}
	if g := byID["D7"]; g.Departed || g.Formed {
		t.Errorf("D7 — несобранная группа (чужой префикс): %+v", g)
	}
	for _, bad := range []string{"D3", "D4", "D5", "D9"} {
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

// Регрессия (план Находка, с.ф. ХАБАРОВСК II): у вагонов уехавшего сборного IndexMain —
// исходный индекс со станции отправления (8649-… Междуреченск), а кандидат найден по
// ТЕКУЩЕМУ индексу сборного (9700-… = kod_4 Хабаровска II) — его и показываем/ключуем.
// Сборный из вагонов разных исходных маршрутов — ОДНА группа (по IndexMain развалился бы).
func TestSFCandidates_DepartedShowsSbornyiIndex(t *testing.T) {
	sf := []SFRecord{{Sinonim: "ХАБАРОВСК II", Station: "ХАБАРОВСК II", Quantity: 60}}
	target := map[string]struct{}{"УТ-1": {}}
	kod4 := map[string][]string{"ХАБАРОВСК II": {"9700"}}
	s2 := 2

	records := []domain.Dislocation{
		{Vagon: "111", StationOper: "КАМЕНУШКА", Naznach: "УТ-1",
			Index: "9700-429-9845", IndexMain: "8649-299-9847", IdDisl: "D1", Status: &s2},
		{Vagon: "112", StationOper: "КАМЕНУШКА", Naznach: "УТ-1",
			Index: "9700-429-9845", IndexMain: "8700-100-9847", IdDisl: "D1", Status: &s2},
	}

	got := SFCandidates("ХАБАРОВСК II", sf, records, target, map[string]struct{}{}, kod4)
	if len(got) != 1 {
		t.Fatalf("сборный из разных исходных маршрутов должен быть одной группой, получено %d: %+v", len(got), got)
	}
	g := got[0]
	if !g.Departed || g.Index != "9700-429-9845" || g.Quantity != 2 {
		t.Errorf("ожидался уехавший сборный с ТЕКУЩИМ индексом 9700-429-9845 и qty=2: %+v", g)
	}
	if len(g.SubGroups) != 2 {
		t.Errorf("«Состав» должен показать 2 подгруппы (разные исходные маршруты): %+v", g.SubGroups)
	}
}
