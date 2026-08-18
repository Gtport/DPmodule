package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/lib/pq"
)

func main() {
	dir := flag.String("dir", "migrations", "directory with SQL migration files")
	dsn := flag.String("dsn", os.Getenv("PG_DSN"), "PostgreSQL DSN")
	schema := flag.String("schema", "dpport", "application schema: created if missing, set as role default search_path (empty = skip)")
	flag.Parse()

	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "PG_DSN is required (env or -dsn flag)")
		os.Exit(1)
	}

	if *schema != "" {
		if err := ensureSchema(*dsn, *schema); err != nil {
			fmt.Fprintf(os.Stderr, "ensure schema %q: %v\n", *schema, err)
			os.Exit(1)
		}
		fmt.Printf("schema %q ensured\n", *schema)
		augmented, err := dsnWithMigrationsTable(*dsn, *schema)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dsn: %v\n", err)
			os.Exit(1)
		}
		*dsn = augmented
	}

	cmd := flag.Arg(0)
	if cmd == "" {
		cmd = "up"
	}

	m, err := migrate.New("file://"+*dir, *dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate.New: %v\n", err)
		os.Exit(1)
	}
	defer m.Close()

	switch cmd {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			fmt.Fprintf(os.Stderr, "migrate up: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("migrations applied")
	case "down":
		if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			fmt.Fprintf(os.Stderr, "migrate down: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("rolled back one step")
	case "drop":
		if err := m.Drop(); err != nil {
			fmt.Fprintf(os.Stderr, "migrate drop: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("database dropped")
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (use: up | down | drop)\n", cmd)
		os.Exit(1)
	}
}

// ensureSchema готовит выданную DevOps базу к работе без ручной настройки:
// создаёт схему приложения (на готовой схеме — no-op; нужно право CREATE в
// базе, у владельца базы оно есть всегда) и, где позволено, вешает search_path
// дефолтом на роль — это удобство для ручного psql, сам код на search_path не
// опирается: и приложение, и миграции пишут схему явно (см.
// internal/repository/gorm/schema.go). На управляемом Postgres ALTER ROLE может
// быть запрещён — тогда только предупреждаем и продолжаем. После успешного
// ALTER срезаем собственное серверное соединение: за пулером (PgBouncer) оно
// открылось до ALTER, дефолта не несёт и иначе вернулось бы в пул; ошибка от
// pg_terminate_backend ожидаема и глотается.
func ensureSchema(dsn, schema string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec("CREATE SCHEMA IF NOT EXISTS " + pq.QuoteIdentifier(schema)); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if _, err := db.Exec("ALTER ROLE CURRENT_USER SET search_path TO " + pq.QuoteIdentifier(schema) + ", public"); err != nil {
		fmt.Printf("note: role default search_path not set (%v) — ok, SQL is schema-qualified\n", err)
		return nil
	}
	_, _ = db.Exec("SELECT pg_terminate_backend(pg_backend_pid())")
	return nil
}

// dsnWithMigrationsTable закрепляет таблицу версий за схемой приложения
// квалифицированным именем (x-migrations-table golang-migrate; свои x-параметры
// он вырезает из DSN до подключения). Без этого место таблицы решал бы
// search_path сессии, а за пулером сессия может достаться и без него — тогда
// schema_migrations молча уехала бы в public. Уже заданный x-migrations-table
// не перекрывается. Сами миграции от search_path не зависят: каждый файл либо
// ставит его сам, либо пишет схему явно (сторожит TestMigrationsSchemaSafe).
func dsnWithMigrationsTable(dsn, schema string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" {
		return "", fmt.Errorf("must be a URL (postgres://user:pass@host:port/db?...): %q", dsn)
	}
	q := u.Query()
	if q.Get("x-migrations-table") == "" {
		q.Set("x-migrations-table", pq.QuoteIdentifier(schema)+"."+pq.QuoteIdentifier("schema_migrations"))
		q.Set("x-migrations-table-quoted", "1")
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}
