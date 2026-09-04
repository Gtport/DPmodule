package service

import (
	"testing"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Очередь у ворот: берутся только гружёные на станции назначения (статусы 9/10)
// в адрес терминалов станции; группа — нитка/индекс × станция операции; фаза —
// по большинству вагонов с кодами подачи/выгрузки; операция — самая поздняя;
// часы на станции — от самого раннего факта прибытия, ЖД-штамп приведён к МСК
// (час ≥ 18 → минус сутки); порядок — дольше стоящие первыми, без факта в конце.
func TestGtGateTrains(t *testing.T) {
	lt := func(s string) *domain.LocalTime {
		tt, err := time.Parse("2006-01-02T15:04:05", s)
		if err != nil {
			t.Fatal(err)
		}
		v := domain.LocalTime(tt)
		return &v
	}
	ip := func(v int) *int { return &v }
	now := time.Date(2026, 9, 5, 6, 30, 0, 0, time.UTC)
	known := map[string]bool{"АЭ": true, "ГУТ-2": true}

	rows := []domain.Dislocation{
		// G1: три вагона, два поданы на ПП (80), один только прибыл (8, старее);
		// факт прибытия ЖД-штампом 05.09 20:30 = МСК 04.09 20:30 → 10 ч на станции.
		{Index: "8650-101-9840", IndexPp: "8650-555-9840", StationOper: "МЫС АСТАФЬЕВА", Status: ip(10), Naznach: "АЭ",
			CodeOper: "80", Oper: "ПОДАЧА ВАГОНА НА ПП", TimeOp: lt("2026-09-05T03:00:00"), DatePrib: lt("2026-09-05T20:30:00")},
		{Index: "8650-101-9840", IndexPp: "8650-555-9840", StationOper: "МЫС АСТАФЬЕВА", Status: ip(10), Naznach: "АЭ",
			CodeOper: "80", Oper: "ПОДАЧА ВАГОНА НА ПП", TimeOp: lt("2026-09-05T03:05:00"), DatePrib: lt("2026-09-05T21:00:00")},
		{Index: "8650-101-9840", IndexPp: "8650-555-9840", StationOper: "МЫС АСТАФЬЕВА", Status: ip(10), Naznach: "ГУТ-2",
			CodeOper: "8", Oper: "ПРИБЫТИЕ ВАГОНА НА СТАНЦИЮ", TimeOp: lt("2026-09-05T02:00:00"), DatePrib: lt("2026-09-05T20:30:00")},
		// G2: кандидат (9) без факта прибытия, стоит после прибытия на станцию.
		{Index: "9001-001-9840", StationOper: "МЫС АСТАФЬЕВА", Status: ip(9), Naznach: "ГУТ-2",
			CodeOper: "8", Oper: "ПРИБЫТИЕ ВАГОНА НА СТАНЦИЮ", TimeOp: lt("2026-09-05T05:00:00")},
		// Отсекаются: чужой терминал, в пути, выгружен.
		{Index: "X1", StationOper: "НАХОДКА", Status: ip(10), Naznach: "УТ-1", CodeOper: "80"},
		{Index: "X2", StationOper: "ТАЙШЕТ", Status: ip(2), Naznach: "АЭ"},
		{Index: "X3", StationOper: "МЫС АСТАФЬЕВА", Status: ip(12), Naznach: "АЭ", CodeOper: "81"},
	}

	got := gtGateTrains(rows, known, now)
	if len(got) != 2 {
		t.Fatalf("поездов у ворот %d, ожидалось 2", len(got))
	}
	g1 := got[0]
	if g1.Index != "8650-555-9840" || g1.VagonCount != 3 || g1.Terminal != "АЭ" || g1.Status != "10" {
		t.Errorf("G1: индекс %q вагонов %d терминал %q статус %q", g1.Index, g1.VagonCount, g1.Terminal, g1.Status)
	}
	if g1.Phase != GtGateOnFront {
		t.Errorf("G1: фаза %q, ожидалась «на фронте» (2 из 3 поданы)", g1.Phase)
	}
	if g1.CodeOper != "80" || g1.TimeOp == nil || time.Time(*g1.TimeOp) != time.Time(*lt("2026-09-05T03:05:00")) {
		t.Errorf("G1: операция %q %v, ожидалась самая поздняя 80 в 03:05", g1.CodeOper, g1.TimeOp)
	}
	if g1.HoursAtStation == nil || *g1.HoursAtStation != 10 {
		t.Errorf("G1: часов на станции %v, ожидалось 10 (ЖД-штамп 05.09 20:30 → МСК 04.09 20:30)", g1.HoursAtStation)
	}
	if g1.DatePrib == nil || time.Time(*g1.DatePrib) != time.Time(*lt("2026-09-05T20:30:00")) {
		t.Errorf("G1: date_prib %v, ожидался самый ранний ЖД-штамп 20:30", g1.DatePrib)
	}
	g2 := got[1]
	if g2.Index != "9001-001-9840" || g2.Status != "9" || g2.Phase != GtGateWaitingFeed || g2.HoursAtStation != nil || g2.Terminal != "ГУТ-2" {
		t.Errorf("G2: %+v", g2)
	}
	if gtFromJd(time.Date(2026, 9, 5, 17, 59, 0, 0, time.UTC)).Day() != 5 || gtFromJd(time.Date(2026, 9, 5, 18, 0, 0, 0, time.UTC)).Day() != 4 {
		t.Error("gtFromJd: час < 18 — те же сутки, час ≥ 18 — минус сутки")
	}
}
