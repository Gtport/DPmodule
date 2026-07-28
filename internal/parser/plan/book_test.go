package plan

import (
	"os"
	"testing"

	"github.com/xuri/excelize/v2"
)

// TestAllSheets — прогон парсера по ВСЕМ листам месячной книги. Лист плана верстают
// руками, и раскладка плывёт день ото дня (см. комментарий к GridParser), поэтому
// проверка «файл разбирается» на одном листе ничего не доказывает: месячная книга —
// это 50+ независимых раскладок одной формы, то есть готовый регрессионный корпус.
//
// Книги в git не лежат (боевые данные, `_data/` в .gitignore) — тест пропускается,
// если файл не указан. Порядок при спорном файле: прогнать книгу ДО правки, потом
// ПОСЛЕ, и сверить вывод — меняться должны только починенные листы.
//
//	PLAN_BOOK=~/projects/_data/plan/nk.xlsx PLAN_CODE=nk go test -count=1 -run TestAllSheets -v ./internal/parser/plan/
func TestAllSheets(t *testing.T) {
	path, code := os.Getenv("PLAN_BOOK"), os.Getenv("PLAN_CODE")
	if path == "" {
		t.Skip("PLAN_BOOK не задан")
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	prof, err := ResolveProfile(code)
	if err != nil {
		t.Fatal(err)
	}
	g := NewGridParser(prof)

	ok, bad := 0, 0
	for _, sheet := range f.GetSheetList() {
		merged, _ := f.GetMergeCells(sheet)
		for _, mc := range merged {
			_ = f.UnmergeCell(sheet, mc.GetStartAxis(), mc.GetEndAxis())
		}
		rows, err := f.GetRows(sheet)
		if err != nil {
			t.Errorf("лист %s: %v", sheet, err)
			continue
		}
		doc, err := g.Parse(rows, path)
		if err != nil {
			bad++
			t.Logf("ЛИСТ %-5s ОШИБКА: %v", sheet, err)
			continue
		}
		w, a := 0, 0
		for _, n := range doc.Nitki {
			w += n.Wagons
			a += n.Activ
		}
		ok++
		t.Logf("ЛИСТ %-5s ниток=%-3d вагонов=%-5d activ=%-5d", sheet, len(doc.Nitki), w, a)
	}
	t.Logf("ИТОГО листов: разобрано %d, с ошибкой %d", ok, bad)
}
