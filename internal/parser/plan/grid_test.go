package plan

import (
	"testing"
	"time"
)

// mkRow строит строку листа с непустыми значениями по индексам (остальное — "").
func mkRow(vals map[int]string) []string {
	max := 0
	for i := range vals {
		if i > max {
			max = i
		}
	}
	r := make([]string, max+1)
	for i, v := range vals {
		r[i] = v
	}
	return r
}

// TestFindLeavesSkipsNumericRows: под строкой имён грузов (row1+2) в реальном плане
// Мыс Астафьева лежит строка «Перераб. спос.» (row1+4) с числами. Имя столбца-листа —
// текст; число за имя брать нельзя, иначе метка получается «НМТП 160» вместо
// «НМТП Каменный уголь». Столбец без числа (слябы) должен сохранить имя.
func TestFindLeavesSkipsNumericRows(t *testing.T) {
	rows := [][]string{
		mkRow(map[int]string{0: "План подвода поездов к станции МЫС АСТАФЬЕВА на 11.07.2026"}),
		mkRow(map[int]string{0: "N п/п", 1: "Индекс"}), // row1 — строка «N п/п»
		mkRow(map[int]string{7: "Итого", 8: "НМТП", 12: "ТЕРМИНАЛ АСТАФЬЕВА"}), // терминалы (row1+1)
		mkRow(map[int]string{8: "ИТОГО", 9: "Каменный уголь", 10: "Черные металлы", 11: "слябы"}), // грузы (row1+2)
		mkRow(map[int]string{0: "План на 11-07-2026"}),                       // row1+3
		mkRow(map[int]string{7: "668", 8: "260", 9: "160", 10: "100", 12: "250"}), // перераб. способность (row1+4)
	}
	g := &GridParser{prof: Profile{PlanCode: "ma", OurTerminals: []string{"НМТП"}}}
	leaves := g.findLeaves(rows, 1)

	got := map[string]bool{}
	for _, lf := range leaves {
		got[lf.label] = true
		for _, r := range lf.label {
			if r >= '0' && r <= '9' {
				t.Errorf("в метку листа попало число (строка перераб. способности): %q", lf.label)
				break
			}
		}
	}
	for _, want := range []string{"НМТП Каменный уголь", "НМТП Черные металлы", "НМТП слябы"} {
		if !got[want] {
			t.Errorf("нет ожидаемого листа %q; получено метки: %v", want, mapKeys(got))
		}
	}
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestSfSynonym: распознавание с.ф. и извлечение синонима — оба варианта реального
// файла («с.ф.БИКИН» суффиксом, «0000-000-0000» из станции) + бесстанционная без станции.
func TestSfSynonym(t *testing.T) {
	cases := []struct {
		index, station, wantSyn string
		wantSf                  bool
	}{
		{"с.ф.БИКИН", "Бикин", "БИКИН", true},
		{"0000-000-0000", "Партизанск", "ПАРТИЗАНСК", true},
		{"с.ф.", "Хмыловский", "ХМЫЛОВСКИЙ", true},
		{"СФ ХАБАРОВСК II", "", "ХАБАРОВСК II", true},
		{"с.ф.", "", "", true}, // с.ф., но станции нет → синоним пуст (строку не эмитим)
		{"9401-429-9857", "Смоляниново", "", false},
	}
	for _, c := range cases {
		if got := isSfRow(c.index); got != c.wantSf {
			t.Errorf("isSfRow(%q)=%v, want %v", c.index, got, c.wantSf)
		}
		if c.wantSf {
			if got := sfSynonym(c.index, c.station); got != c.wantSyn {
				t.Errorf("sfSynonym(%q,%q)=%q, want %q", c.index, c.station, got, c.wantSyn)
			}
		}
	}
}

// TestBuildSfNitka: с.ф.-строка эмитится с флагом IsSf и индексом «с.ф.<СИНОНИМ>»;
// бесстанционная без станции — не эмитится.
func TestBuildSfNitka(t *testing.T) {
	g := &GridParser{prof: Profile{PlanCode: "ma", OurTerminals: []string{"НМТП"}}}
	cols := gridCols{colIndex: 1, colStation: 3, colPlan: 5, colFact: 6, colKolVag: 7, colComment: 16}
	bd := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)

	n, ok := g.buildSfNitka(mkRow(map[int]string{1: "с.ф.БИКИН", 3: "Бикин", 5: "12:05", 7: "60"}), cols, bd)
	if !ok || !n.IsSf || n.IndexPp != "с.ф.БИКИН" || n.Index != "" {
		t.Fatalf("с.ф.БИКИН: ok=%v IsSf=%v IndexPp=%q Index=%q", ok, n.IsSf, n.IndexPp, n.Index)
	}
	n, ok = g.buildSfNitka(mkRow(map[int]string{1: "0000-000-0000", 3: "Партизанск", 5: "10:07", 7: "34"}), cols, bd)
	if !ok || n.IndexPp != "с.ф.ПАРТИЗАНСК" {
		t.Fatalf("0000: ok=%v IndexPp=%q", ok, n.IndexPp)
	}
	if _, ok := g.buildSfNitka(mkRow(map[int]string{1: "с.ф.", 3: ""}), cols, bd); ok {
		t.Errorf("с.ф. без станции должна пропускаться (ok=false)")
	}
}
