package plan

import "testing"

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
