// Command planrun разбирает файл «плана подвода» станции и печатает нитки, а при
// указании дампа дислокации — ещё и сопоставление вагонов с нитками (движок
// planmatch). Только чтение и печать, без БД.
//
//	go run ./cmd/planrun <файл.xlsx> <код_станции>            # только нитки
//	go run ./cmd/planrun <файл.xlsx> <код_станции> <disl.json> # + матч по дампу
//
//	go run ./cmd/planrun "/home/alex/projects/new_go/Мыс Астафьева.xlsx" ma
//	go run ./cmd/planrun "/home/alex/projects/new_go/Находка.xlsx" nk disl.json
//
// Дамп дислокации — JSON-массив domain.Dislocation (например, выгрузка disl_actual).
// Целевой набор площадок берётся из ports.csv (по умолчанию _reference/seed/ports.csv,
// переопределяется переменной PORTS_CSV) — данные, не хардкод.
package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser/plan"
	"github.com/Gtport/DPmodule/internal/service/planmatch"
)

func main() {
	if len(os.Args) != 3 && len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "использование: planrun <файл.xlsx> <код_станции: ma|nk> [disl.json]\n")
		os.Exit(2)
	}
	path, code := os.Args[1], os.Args[2]

	doc, err := plan.ParseFile(path, code)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}

	printNitki(doc)

	if len(os.Args) == 4 {
		if err := runMatch(doc, code, os.Args[3]); err != nil {
			fmt.Fprintf(os.Stderr, "ошибка матча: %v\n", err)
			os.Exit(1)
		}
	}
}

func printNitki(doc *plan.PlanDoc) {
	fmt.Printf("Файл:    %s\n", doc.SourceFile)
	fmt.Printf("Станция: %s\n", doc.PlanCode)
	fmt.Printf("Ниток:   %d\n\n", len(doc.Nitki))

	fmt.Printf("%-3s  %-13s  %-16s  %-16s  %-7s  %5s  %5s\n",
		"№", "Индекс", "План (МСК)", "Факт (МСК)", "Откл.", "Ваг.", "Activ")
	fmt.Println(rule(3, 13, 16, 16, 7, 5, 5))

	var totalWagons, totalActiv int
	for i, n := range doc.Nitki {
		fmt.Printf("%-3d  %-13s  %-16s  %-16s  %-7s  %5d  %5d\n",
			i+1, n.Index, fmtTime(n.PlanMsk), fmtTime(n.FactMsk), n.Otkl, n.Wagons, n.Activ)
		totalWagons += n.Wagons
		totalActiv += n.Activ
	}
	fmt.Printf("\nИТОГО: вагонов %d, из них «наших» (Activ) %d\n", totalWagons, totalActiv)
}

func runMatch(doc *plan.PlanDoc, code, dislPath string) error {
	prof, err := plan.ResolveProfile(code)
	if err != nil {
		return err
	}
	target, err := loadTargets(portsCSVPath(), code)
	if err != nil {
		return err
	}
	if len(target) == 0 {
		return fmt.Errorf("для плана %q не найдено целевых площадок в ports.csv", code)
	}
	records, err := loadDisl(dislPath)
	if err != nil {
		return err
	}

	agg := planmatch.Aggregate(records, target)
	res := planmatch.Match(doc.Nitki, agg, prof.MatchRequiresNaznach)

	fmt.Printf("\n\nМатч по дампу %q (%d записей дислокации)\n", dislPath, len(records))
	fmt.Printf("Целевые площадки (%s): %s\n\n", code, keys(target))

	fmt.Printf("%-3s  %-13s  %-12s  %5s  %5s  %7s  %-14s  %5s\n",
		"№", "Индекс", "База", "Activ", "Наши", "Балл", "Источник", "Ваг.")
	fmt.Println(rule(3, 13, 12, 5, 5, 7, 14, 5))

	matched, stamped := 0, 0
	for i, m := range res {
		base := ""
		if len(m.Nitka.Index) >= 11 {
			base = m.Nitka.Index[:11]
		}
		src, maw, sc := "—", "", ""
		if m.Matched {
			matched++
			stamped += len(m.Vagons)
			src = m.Source
			maw = fmt.Sprintf("%d", m.MaWagons)
			sc = fmt.Sprintf("%.1f", m.Score)
		}
		fmt.Printf("%-3d  %-13s  %-12s  %5d  %5s  %7s  %-14s  %5d\n",
			i+1, m.Nitka.Index, base, m.Nitka.Activ, maw, sc, src, len(m.Vagons))
	}
	fmt.Printf("\nИТОГО: сопоставлено ниток %d из %d; вагонов застолблено %d\n",
		matched, len(res), stamped)
	return nil
}

// ─────────────────────────── загрузка данных ───────────────────────────

func portsCSVPath() string {
	if p := os.Getenv("PORTS_CSV"); p != "" {
		return p
	}
	return "_reference/seed/ports.csv"
}

// loadTargets читает ports.csv и возвращает множество name_s для plan_code==code.
func loadTargets(csvPath, code string) (map[string]struct{}, error) {
	f, err := os.Open(csvPath)
	if err != nil {
		return nil, fmt.Errorf("открытие ports.csv (%s): %w", csvPath, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("чтение заголовка ports.csv: %w", err)
	}
	colName, colPlan := -1, -1
	for i, h := range header {
		switch h {
		case "name_s":
			colName = i
		case "plan_code":
			colPlan = i
		}
	}
	if colName == -1 || colPlan == -1 {
		return nil, fmt.Errorf("в ports.csv нет колонок name_s/plan_code")
	}

	out := map[string]struct{}{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("чтение ports.csv: %w", err)
		}
		if colPlan < len(rec) && colName < len(rec) && rec[colPlan] == code && rec[colName] != "" {
			out[rec[colName]] = struct{}{}
		}
	}
	return out, nil
}

// loadDisl читает JSON-массив domain.Dislocation.
func loadDisl(path string) ([]domain.Dislocation, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("чтение дампа: %w", err)
	}
	var recs []domain.Dislocation
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil, fmt.Errorf("разбор JSON дампа: %w", err)
	}
	return recs, nil
}

// ─────────────────────────── печать ───────────────────────────

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("02.01.06 15:04")
}

func keys(m map[string]struct{}) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	out := ""
	for i, k := range ks {
		if i > 0 {
			out += ", "
		}
		out += k
	}
	return out
}

func rule(widths ...int) string {
	out := ""
	for i, w := range widths {
		if i > 0 {
			out += "  "
		}
		b := make([]byte, w)
		for j := range b {
			b[j] = '-'
		}
		out += string(b)
	}
	return out
}
