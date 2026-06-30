package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	dir := flag.String("dir", "migrations", "directory with SQL migration files")
	dsn := flag.String("dsn", os.Getenv("PG_DSN"), "PostgreSQL DSN")
	flag.Parse()

	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "PG_DSN is required (env or -dsn flag)")
		os.Exit(1)
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
