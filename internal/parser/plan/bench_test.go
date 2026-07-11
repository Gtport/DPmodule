package plan

import (
	"os"
	"testing"
)

// benchFixture — реальный план для бенчмарков разбора. Файл вне git (_data),
// поэтому при его отсутствии бенчмарк пропускается (тимлид/CI не падают).
const benchFixture = "/home/alex/projects/DPmodule/_data/plan/ma.xlsx"

func requireFixture(tb testing.TB) {
	if _, err := os.Stat(benchFixture); err != nil {
		tb.Skipf("нет фикстуры %s — бенчмарк пропущен", benchFixture)
	}
}

func BenchmarkParseFile(b *testing.B) {
	requireFixture(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ParseFile(benchFixture, "ma"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStages разбивает разбор на этапы: чтение листа (excelize) vs Parse.
func BenchmarkStages(b *testing.B) {
	requireFixture(b)

	b.Run("ReadPlanSheet", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := ReadPlanSheet(benchFixture); err != nil {
				b.Fatal(err)
			}
		}
	})

	rows, err := ReadPlanSheet(benchFixture)
	if err != nil {
		b.Fatal(err)
	}
	p, _ := Resolve("ma")
	b.Run("Parse", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := p.Parse(rows, benchFixture); err != nil {
				b.Fatal(err)
			}
		}
	})
}
