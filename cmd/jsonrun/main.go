// Command jsonrun — разовый прогон боевых JSON-выгрузок (api_pull) через весь
// конвейер дислокации (Stage 1 + Stage 2/3 + подмена снимка + запись status9/6 и
// vagon_history). Переиспользует LKProcessor.ProcessRecords. Пароль БД — из окружения
// (PG_PASSWORD, как у сервера). Запуск из корня репозитория:
//
//	set -a; . ./.env; set +a
//	go run ./cmd/jsonrun /home/alex/projects/new_go/nmtp.json /home/alex/projects/new_go/attis.json
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Gtport/DPmodule/internal/config"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/parser"
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
	files := os.Args[1:]
	if len(files) == 0 {
		return fmt.Errorf("укажите пути к JSON-файлам")
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

	// Прогрев кэшей (как на старте сервера).
	dirCache := service.NewDirectoryCache(gormrepo.NewDirectoryRepository(db))
	if err := dirCache.Load(ctx); err != nil {
		return fmt.Errorf("directory cache: %w", err)
	}
	cfgCache := service.NewConfigCache(gormrepo.NewConfigRepository(db))
	if err := cfgCache.Load(ctx); err != nil {
		return fmt.Errorf("config cache: %w", err)
	}
	dislRepo := gormrepo.NewDislocationRepository(db)
	actualCache := service.NewActualCache(dislRepo)
	if err := actualCache.Load(ctx); err != nil {
		return fmt.Errorf("actual cache: %w", err)
	}
	status9Cache := service.NewStatus9Cache(gormrepo.NewStatus9Repository(db))
	if err := status9Cache.Load(ctx); err != nil {
		return fmt.Errorf("status9 cache: %w", err)
	}
	status6Cache := service.NewStatus6Cache(gormrepo.NewStatus6Repository(db))
	if err := status6Cache.Load(ctx); err != nil {
		return fmt.Errorf("status6 cache: %w", err)
	}
	historyRepo := gormrepo.NewHistoryRepository(db)

	fmt.Printf("прогрето: actual=%d, status9=%d, status6=%d\n",
		actualCache.Count(), status9Cache.Count(), status6Cache.Count())

	// Парсинг JSON.
	jp := parser.NewJSONParser()
	all := make([]domain.Dislocation, 0, 8192)
	perFile := map[string]int{}
	for _, f := range files {
		recs, err := jp.ParseFile(f)
		if err != nil {
			return fmt.Errorf("парсинг %s: %w", f, err)
		}
		perFile[filepath.Base(f)] = len(recs)
		all = append(all, recs...)
		fmt.Printf("  %s → %d записей\n", filepath.Base(f), len(recs))
	}

	intake := service.NewLKIntake(cfgCache, dirCache, cfg.Storage.BaseDir)
	proc := service.NewLKProcessor(intake, dislRepo, actualCache, status9Cache, status6Cache, historyRepo)

	res, err := proc.ProcessRecords(ctx, all, len(files), perFile)
	if err != nil {
		return fmt.Errorf("обработка: %w", err)
	}

	fmt.Println("\n=== РЕЗУЛЬТАТ ===")
	fmt.Printf("снимок: %d записей (было %d)\n", res.Count, res.PrevSnapshot)
	fmt.Printf("Stage1: назначений=%d, порт не резолвится=%d, порт выключен=%d\n",
		res.NaznEnriched, res.PortUnresolved, res.PortDisabled)
	fmt.Printf("статусы: %v\n", res.StatusDist)
	fmt.Printf("carry-over: matched=%d new=%d sticky=%d\n", res.CarryMatched, res.CarryNew, res.CarrySticky)
	fmt.Printf("marka: кандидатов=%d заполнено=%d нет марки=%d, перестановок=%d\n",
		res.MarkaCandidates, res.MarkaFilled, res.MarkaMissed, res.NaznachOverride)
	fmt.Printf("status6 доноры=%d, донорство=%d\n", res.Status6Donors, res.Status6Matched)
	fmt.Printf("status9: 9-вставлено=%d снято=%d, 8-пропавших=%d\n",
		res.Status9Inserted, res.Status9Removed, res.Status8Missing)
	fmt.Printf("прогноз: посчитано=%d\n", res.ForecastComputed)
	fmt.Printf("vagon_history: вставлено=%d обновлено=%d\n", res.HistoryInserted, res.HistoryUpdated)
	if len(res.StationsNotFound) > 0 {
		fmt.Printf("станций не найдено: %d\n", len(res.StationsNotFound))
	}
	return nil
}
