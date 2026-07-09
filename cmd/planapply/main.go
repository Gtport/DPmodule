// Command planapply — разовый прогон файла плана подвода через боевой dpport:
// разбор + сопоставление вагонов с нитками + простановка PlanMsk/PlanJd/IndexPp в
// снимок дислокации. Переиспользует service.PlanProcessor. Пароль БД — из окружения
// (PG_PASSWORD, как у сервера). Запуск из корня репозитория:
//
//	set -a; . ./.env; set +a
//	go run ./cmd/planapply "/home/alex/projects/new_go/Мыс Астафьева.xlsx" ma
//
// После прогона проверить простановку:
//
//	SELECT index_pp, plan_msk, count(*) FROM dpport.dislocation
//	WHERE index_pp <> '' GROUP BY 1,2 ORDER BY 2;
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Gtport/DPmodule/internal/config"
	gormrepo "github.com/Gtport/DPmodule/internal/repository/gorm"
	"github.com/Gtport/DPmodule/internal/service"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("использование: planapply <файл.xlsx> <код_станции: ma|nk>")
	}
	path, code := os.Args[1], os.Args[2]

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("чтение файла плана: %w", err)
	}

	cfg, err := config.Load("config.yaml")
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if !cfg.Postgres.Enabled {
		return fmt.Errorf("postgres.enabled = false")
	}
	db, err := gormrepo.Open(cfg.Postgres)
	if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	ctx := context.Background()

	// Прогрев кэшей (как на старте сервера): справочники + актуальный снимок.
	dirCache := service.NewDirectoryCache(gormrepo.NewDirectoryRepository(db))
	if err := dirCache.Load(ctx); err != nil {
		return fmt.Errorf("directory cache: %w", err)
	}
	dislRepo := gormrepo.NewDislocationRepository(db)
	actualCache := service.NewActualCache(dislRepo)
	if err := actualCache.Load(ctx); err != nil {
		return fmt.Errorf("actual cache: %w", err)
	}
	fmt.Printf("прогрето: actual=%d, целевых площадок для %q=%d\n",
		actualCache.Count(), code, len(dirCache.TargetNaznach(code)))

	planRepo := gormrepo.NewPlanRepository(db)
	proc := service.NewPlanProcessor(dirCache, dislRepo, actualCache, planRepo, cfg.Storage.BaseDir)
	res, err := proc.ProcessFile(ctx, code, filepath.Base(path), data)
	if err != nil {
		return fmt.Errorf("обработка плана: %w", err)
	}

	fmt.Println("\n=== РЕЗУЛЬТАТ ===")
	fmt.Printf("файл: %s (план %s)\n", res.Filename, res.PlanCode)
	fmt.Printf("ниток: %d, сопоставлено: %d\n", res.Nitki, res.Matched)
	fmt.Printf("вагонов проставлено: %d, очищено от старого плана: %d\n", res.Stamped, res.Cleared)
	return nil
}
