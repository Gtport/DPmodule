package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDSNWithMigrationsTable(t *testing.T) {
	t.Run("закрепляет таблицу версий за схемой, остальные параметры целы", func(t *testing.T) {
		got, err := dsnWithMigrationsTable("postgres://u:p@h:5432/db?sslmode=disable", "dpport")
		if err != nil {
			t.Fatalf("неожиданная ошибка: %v", err)
		}
		want := "postgres://u:p@h:5432/db?sslmode=disable" +
			"&x-migrations-table=%22dpport%22.%22schema_migrations%22&x-migrations-table-quoted=1"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("заданный x-migrations-table не перекрывается", func(t *testing.T) {
		in := "postgres://u@h/db?x-migrations-table=custom"
		got, err := dsnWithMigrationsTable(in, "dpport")
		if err != nil {
			t.Fatalf("неожиданная ошибка: %v", err)
		}
		if got != in {
			t.Fatalf("got %q, want %q", got, in)
		}
	})

	t.Run("key=value DSN отвергается: golang-migrate требует URL", func(t *testing.T) {
		if _, err := dsnWithMigrationsTable("host=x dbname=y", "dpport"); err == nil {
			t.Fatal("ожидалась ошибка для key=value DSN")
		}
	})
}

// TestMigrationsSchemaSafe сторожит независимость миграций от search_path
// сессии: за пулером соединений (PgBouncer) сессия может прийти без него,
// поэтому каждый файл обязан либо сам ставить SET search_path, либо писать
// схему dpport явно. Новая миграция без того и другого — ошибка.
func TestMigrationsSchemaSafe(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("миграции не найдены — проверь путь")
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		s := string(body)
		if !strings.Contains(s, "search_path") && !strings.Contains(s, "dpport.") {
			t.Errorf("%s: ни SET search_path, ни явной схемы dpport. — за пулером уедет не в ту схему", filepath.Base(f))
		}
	}
}
