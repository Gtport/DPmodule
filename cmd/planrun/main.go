// Command planrun разбирает файл «плана подвода» станции и печатает нитки.
// Только чтение и печать — без БД (хранение появится отдельным шагом).
//
//	go run ./cmd/planrun <файл.xlsx> <код_станции>
//	go run ./cmd/planrun "/home/alex/projects/new_go/Мыс Астафьева.xlsx" ma
//	go run ./cmd/planrun "/home/alex/projects/new_go/Находка.xlsx" nk
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/Gtport/DPmodule/internal/parser/plan"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "использование: planrun <файл.xlsx> <код_станции: ma|nk>\n")
		os.Exit(2)
	}
	path, code := os.Args[1], os.Args[2]

	doc, err := plan.ParseFile(path, code)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Файл:    %s\n", doc.SourceFile)
	fmt.Printf("Станция: %s\n", doc.PlanCode)
	fmt.Printf("Ниток:   %d\n\n", len(doc.Nitki))

	fmt.Printf("%-3s  %-13s  %-16s  %-16s  %-7s  %5s  %5s\n",
		"№", "Индекс", "План (МСК)", "Факт (МСК)", "Откл.", "Ваг.", "Activ")
	fmt.Println(dashes(3) + "  " + dashes(13) + "  " + dashes(16) + "  " + dashes(16) + "  " + dashes(7) + "  " + dashes(5) + "  " + dashes(5))

	var totalWagons, totalActiv int
	for i, n := range doc.Nitki {
		fmt.Printf("%-3d  %-13s  %-16s  %-16s  %-7s  %5d  %5d\n",
			i+1, n.Index, fmtTime(n.PlanMsk), fmtTime(n.FactMsk), n.Otkl, n.Wagons, n.Activ)
		totalWagons += n.Wagons
		totalActiv += n.Activ
	}
	fmt.Printf("\nИТОГО: вагонов %d, из них «наших» (Activ) %d\n", totalWagons, totalActiv)
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("02.01.06 15:04")
}

func dashes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '-'
	}
	return string(b)
}
