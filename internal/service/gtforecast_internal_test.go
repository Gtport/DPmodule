package service

import (
	"testing"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
)

// Группировка снимка в очередь GT: индекс из плановой нитки, подсчёт вагонов по
// подгруппам, нумерация Б/И, фильтры (статус ≥ 9, без прогноза, чужой терминал),
// признак универсальности по паре станций.
func TestGtTransitTrains(t *testing.T) {
	lt := func(s string) *domain.LocalTime {
		tt, err := time.Parse("2006-01-02T15:04:05", s)
		if err != nil {
			t.Fatal(err)
		}
		v := domain.LocalTime(tt)
		return &v
	}
	ip := func(v int) *int { return &v }

	prog := lt("2026-08-04T10:30:00")
	rows := []domain.Dislocation{
		// Поезд с плановой ниткой: три вагона, две подгруппы (разные станции отправления).
		{Index: "8650-101-9840", IndexPp: "8650-555-9840", StationOper: "ТАЙШЕТ", Status: ip(2),
			ProgJd: prog, RasstStanNazn: ip(500), Naznach: "АЭ", GruzpolS: "АЭ",
			StanNazn: "МЫС АСТАФЬЕВА", StationNach: "ЕРУНАКОВО", CargoGroup: "УГОЛЬ", Color: "#7030A0"},
		{Index: "8650-101-9840", IndexPp: "8650-555-9840", StationOper: "ТАЙШЕТ", Status: ip(2),
			ProgJd: prog, RasstStanNazn: ip(500), Naznach: "АЭ", GruzpolS: "АЭ",
			StanNazn: "МЫС АСТАФЬЕВА", StationNach: "ЕРУНАКОВО", CargoGroup: "УГОЛЬ", Color: "#7030A0"},
		{Index: "8650-101-9840", IndexPp: "8650-555-9840", StationOper: "ТАЙШЕТ", Status: ip(2),
			ProgJd: prog, RasstStanNazn: ip(500), Naznach: "ГУТ-2", GruzpolS: "ГУТ-2",
			StanNazn: "МЫС АСТАФЬЕВА", StationNach: "УЛАК", CargoGroup: "МЕТАЛЛ", Color: "#FF5722"},
		// Без индекса → Б/И 1.
		{Index: "", StationOper: "ХАБАРОВСК", Status: ip(4),
			ProgJd: lt("2026-08-05T02:00:00"), RasstStanNazn: ip(900), Naznach: "АЭ", GruzpolS: "АЭ",
			StanNazn: "МЫС АСТАФЬЕВА", StationNach: "НЕРЮНГРИ", CargoGroup: "УГОЛЬ"},
		// Отсекаются: прибывший, без прогноза, чужой терминал.
		{Index: "X1", StationOper: "П", Status: ip(10), ProgJd: prog, Naznach: "АЭ"},
		{Index: "X2", StationOper: "П", Status: ip(2), ProgJd: nil, Naznach: "АЭ"},
		{Index: "X3", StationOper: "П", Status: ip(2), ProgJd: prog, Naznach: "УТ-1"},
	}
	known := map[string]bool{"АЭ": true, "ГУТ-2": true}
	univers := map[string]bool{"МЫС АСТАФЬЕВА|УЛАК": true}

	got := gtTransitTrains(rows, known, univers)
	if len(got) != 2 {
		t.Fatalf("поездов %d, ожидалось 2", len(got))
	}

	tr := got[0]
	if tr.Index != "8650-555-9840" {
		t.Errorf("индекс поезда %q, ожидалась плановая нитка 8650-555-9840", tr.Index)
	}
	if tr.VagonCount != 3 || len(tr.SubGroups) != 2 {
		t.Errorf("вагонов %d (ждали 3), подгрупп %d (ждали 2)", tr.VagonCount, len(tr.SubGroups))
	}
	for _, sg := range tr.SubGroups {
		switch sg.Naznach {
		case "АЭ":
			if sg.VagonCount != 2 || sg.IsUniversal {
				t.Errorf("подгруппа АЭ: вагонов %d (ждали 2), универс %v (ждали false)", sg.VagonCount, sg.IsUniversal)
			}
		case "ГУТ-2":
			if sg.VagonCount != 1 || !sg.IsUniversal {
				t.Errorf("подгруппа ГУТ-2: вагонов %d (ждали 1), универс %v (ждали true — УЛАК)", sg.VagonCount, sg.IsUniversal)
			}
		}
	}
	if got[1].Index != "Б/И 1" {
		t.Errorf("безындексный поезд получил %q, ожидалось «Б/И 1»", got[1].Index)
	}
}

// Извлечение поездов потока: фильтр по терминалу и группе груза; пустой
// cargo_key линии собирает все грузы терминала; поезд дублируется по подгруппам.
func TestGtFlowTrains(t *testing.T) {
	prog := domain.LocalTime(time.Date(2026, 8, 4, 10, 30, 0, 0, time.UTC))
	trains := []GtTrainDTO{{
		Index: "8650-555-9840", ProgJd: &prog,
		SubGroups: []GtSubGroupDTO{
			{Key: "a", Naznach: "АЭ", CargoGroup: "УГОЛЬ", VagonCount: 40},
			{Key: "b", Naznach: "ГУТ-2", CargoGroup: "МЕТАЛЛ", VagonCount: 22},
			{Key: "c", Naznach: "ГУТ-2", CargoGroup: "УГОЛЬ", VagonCount: 18},
		},
	}}

	ae := gtFlowTrains(trains, "АЭ", "")
	if len(ae) != 1 || ae[0].Sub.VagonCount != 40 {
		t.Errorf("поток АЭ: %d записей (ждали 1 с 40 ваг)", len(ae))
	}
	gutMetal := gtFlowTrains(trains, "ГУТ-2", "МЕТАЛЛ")
	if len(gutMetal) != 1 || gutMetal[0].Sub.VagonCount != 22 {
		t.Errorf("поток ГУТ-2/МЕТАЛЛ: %d записей (ждали 1 с 22 ваг)", len(gutMetal))
	}
	if got := gtFlowTrains(trains, "УТ-1", ""); len(got) != 0 {
		t.Errorf("поток УТ-1 должен быть пуст, получено %d", len(got))
	}
	// Расчётная шкала: ЖД 10:30 → 16:30 тех же суток.
	want := time.Date(2026, 8, 4, 16, 30, 0, 0, time.UTC)
	if !ae[0].CalcTime.Equal(want) {
		t.Errorf("calcTime %v, ожидалось %v", ae[0].CalcTime, want)
	}
}
