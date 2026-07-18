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

// TestFindLeavesFirstColumnUnderTerminalName: в «новой форме» (реальный ma.xlsx от
// 15.07.2026) у терминала НЕТ своего столбца «ИТОГО» — название терминала (НМТП, col8)
// стоит ПРЯМО над первым грузом (Каменный уголь, col8), а train-total «Итого» — отдельный
// col7. Раньше поиск листьев стартовал с term.start+1 и терял этот первый груз (уголь
// НМТП и уголь ТЕРМИНАЛА не прогружались). Старт с term.start — оба должны найтись.
func TestFindLeavesFirstColumnUnderTerminalName(t *testing.T) {
	rows := [][]string{
		mkRow(map[int]string{0: "План подвода поездов к станции МЫС АСТАФЬЕВА на 15.07.2026"}),
		mkRow(map[int]string{0: "N п/п", 1: "Индекс"}), // row1
		mkRow(map[int]string{7: "Итого", 8: "НМТП", 11: "ТЕРМИНАЛ", 13: "АТТИС ЭНТЕРПРАЙС", 14: "ПОРТ ЛИВАДИЯ"}), // терминалы (row1+1)
		mkRow(map[int]string{8: "Каменный уголь", 9: "Черные металлы", 10: "ПРОЧИЕ ГРУЗЫ", 11: "Каменный уголь", 12: "Грузы в контейнерах"}), // грузы (row1+2)
		mkRow(map[int]string{0: "План на 15-07-2026"}),                       // row1+3
		mkRow(map[int]string{7: "118", 8: "16", 11: "80", 13: "13", 14: "9"}), // остаток на 18:00 (row1+4)
	}
	g := &GridParser{prof: Profile{PlanCode: "ma", OurTerminals: []string{"НМТП", "АТТИС"}}}
	leaves := g.findLeaves(rows, 1)

	got := map[string]bool{}
	for _, lf := range leaves {
		got[lf.label] = true
	}
	// Ключ регрессии: первый груз каждого терминала (стоит под его названием).
	for _, want := range []string{
		"НМТП Каменный уголь", "НМТП Черные металлы", "НМТП ПРОЧИЕ ГРУЗЫ",
		"ТЕРМИНАЛ Каменный уголь", "ТЕРМИНАЛ Грузы в контейнерах",
	} {
		if !got[want] {
			t.Errorf("нет ожидаемого листа %q; получено метки: %v", want, mapKeys(got))
		}
	}
	// «Итого» (train-total, col7) — не терминал и не лист.
	if got["Итого"] {
		t.Errorf("train-total «Итого» ошибочно попал в листья")
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

// TestCollectByIndexNotRowNumber: нитка распознаётся по валидному индексу 4-3-4
// (или маркеру с.ф.) в столбце «Индекс», номер п/п НЕ требуется — в свежих блоках
// месячной книги он не проставлен (прод-фикс gtport). «План» с текстом
// («не подводить») → нитка без времени, текст в PlanRaw; свободные нитки (только
// время, без индекса) и служебные строки — не эмитятся.
func TestCollectByIndexNotRowNumber(t *testing.T) {
	rows := [][]string{
		mkRow(map[int]string{0: "План подвода поездов к станции МЫС АСТАФЬЕВА"}),
		mkRow(map[int]string{0: "N п/п", 1: "Индекс", 5: "План", 6: "Факт"}), // row1
		mkRow(map[int]string{7: "Итого", 8: "НМТП"}),
		mkRow(map[int]string{8: "Каменный уголь"}),
		mkRow(map[int]string{0: "План на 18-07-2026"}),
		mkRow(map[int]string{1: "Остаток на 18:00", 8: "25"}),
		// с номером п/п — принимается (как раньше)
		mkRow(map[int]string{0: "1", 1: "9131-001-9857", 5: "19:22", 7: "56", 8: "56"}),
		// БЕЗ номера п/п, валидный индекс — принимается (фикс)
		mkRow(map[int]string{1: "9131-677-9857", 5: "21:06", 7: "56", 8: "56"}),
		// БЕЗ номера, «не подводить» вместо времени — нитка без времени, текст в PlanRaw
		mkRow(map[int]string{1: "9379-782-9857", 5: "не подводить", 7: "71", 8: "71"}),
		// свободная нитка (только время) — пропускается
		mkRow(map[int]string{5: "00:01"}),
		// служебная итоговая строка — пропускается
		mkRow(map[int]string{1: "План выгрузки", 8: "55"}),
		// с.ф. без номера — эмитится
		mkRow(map[int]string{1: "с.ф.", 3: "БИКИН", 5: "19:22", 7: "50", 8: "15"}),
	}
	g := &GridParser{prof: Profile{PlanCode: "ma", OurTerminals: []string{"НМТП"}}}
	cols, err := g.findColumns(rows)
	if err != nil {
		t.Fatalf("findColumns: %v", err)
	}
	cols.colStation = 3 // станция с.ф. (в mkRow-фикстуре шапка её не объявляет)
	nitki, err := g.collect(rows, cols)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	byIdx := map[string]PlanNitka{}
	for _, n := range nitki {
		byIdx[n.IndexPp] = n
	}
	if len(nitki) != 5 { // Остаток + 3 поезда + с.ф.
		t.Fatalf("ожидалось 5 ниток, получено %d: %+v", len(nitki), byIdx)
	}
	if _, ok := byIdx["9131-001-9857"]; !ok {
		t.Error("нитка с номером п/п потеряна")
	}
	n, ok := byIdx["9131-677-9857"]
	if !ok || n.PlanJd.IsZero() || n.PlanJd.Day() != 18 || n.PlanJd.Hour() != 21 {
		t.Errorf("нитка без номера п/п: %+v (ожидалось 18.07 21:06)", n)
	}
	np, ok := byIdx["9379-782-9857"]
	if !ok {
		t.Fatal("нитка «не подводить» потеряна")
	}
	if !np.PlanJd.IsZero() || !np.PlanMsk.IsZero() || np.PlanRaw != "не подводить" {
		t.Errorf("«не подводить»: jd=%v msk=%v raw=%q (ожидалось: без времени, текст в PlanRaw)", np.PlanJd, np.PlanMsk, np.PlanRaw)
	}
	if _, ok := byIdx["с.ф.БИКИН"]; !ok {
		t.Error("с.ф. без номера п/п потеряна")
	}
	if _, ok := byIdx[""]; ok && len(byIdx) > 5 {
		t.Error("свободная нитка/служебная строка попала в нитки")
	}
}

// Извлечение станции из строки-заголовка файла (опора гарда «файл не той станции»).
func TestTitleStation(t *testing.T) {
	cases := []struct {
		name string
		rows [][]string
		want string
	}{
		{"МА", [][]string{{"План подвода поездов к станции МЫС АСТАФЬЕВА на 18.07.2026"}}, "МЫС АСТАФЬЕВА"},
		{"НК", [][]string{{"", "План подвода поездов к станции НАХОДКА на 18.07.2026"}}, "НАХОДКА"},
		{"лишние пробелы и регистр", [][]string{{"план подвода поездов к станции  Мыс   Астафьева  на 01-01-2027"}}, "МЫС АСТАФЬЕВА"},
		{"нет заголовка", [][]string{{"N п/п", "Индекс"}}, ""},
		{"заголовок глубже 6 строк не ищем", [][]string{{}, {}, {}, {}, {}, {}, {"к станции НАХОДКА на 18.07.2026"}}, ""},
	}
	for _, tc := range cases {
		if got := titleStation(tc.rows); got != tc.want {
			t.Errorf("%s: titleStation = %q, ожидалось %q", tc.name, got, tc.want)
		}
	}
}
