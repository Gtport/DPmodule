// @title           IQPort Service API
// @version         1.0
// @description     IQPort microservice template.
//
// @host            localhost:8080
// @BasePath        /
//
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
// @description     Keycloak JWT. Format: "Bearer <token>"
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
	"gorm.io/gorm"

	_ "github.com/Gtport/DPmodule/api/swagger"
	"github.com/Gtport/DPmodule/internal/config"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/port"
	gormrepo "github.com/Gtport/DPmodule/internal/repository/gorm"
	"github.com/Gtport/DPmodule/internal/server"
	"github.com/Gtport/DPmodule/internal/service"
	"github.com/Gtport/DPmodule/internal/worker"
	"github.com/Gtport/DPmodule/pkg/logger"
	"github.com/Gtport/DPmodule/pkg/middleware"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.yaml", "path to YAML config file")
	flag.Parse()

	// -- config --
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// -- logger --
	log, err := logger.New(logger.Config{
		Level:      cfg.Log.Level,
		Env:        cfg.App.Env,
		File:       cfg.Log.File,
		MaxSizeMB:  cfg.Log.MaxSizeMB,
		MaxBackups: cfg.Log.MaxBackups,
		MaxAgeDays: cfg.Log.MaxAgeDays,
	})
	if err != nil {
		return fmt.Errorf("logger: %w", err)
	}
	defer log.Sync() //nolint:errcheck

	log.Info("starting", zap.String("app", cfg.App.Name), zap.String("env", cfg.App.Env))

	// -- postgres (optional) --
	// Every external dependency is gated by its `enabled` flag so the template
	// boots without any of them. Flip the flag in config once the dependency is
	// wired to the stand.
	var (
		db    *gorm.DB
		sqlDB *sql.DB
	)
	if cfg.Postgres.Enabled {
		db, err = gormrepo.Open(cfg.Postgres)
		if err != nil {
			return fmt.Errorf("db: %w", err)
		}
		sqlDB, err = db.DB()
		if err != nil {
			return fmt.Errorf("db.DB(): %w", err)
		}
		defer sqlDB.Close()

		if err := sqlDB.PingContext(context.Background()); err != nil {
			return fmt.Errorf("db ping: %w", err)
		}
		log.Info("postgres connected")
	} else {
		log.Warn("postgres disabled — skipping (set postgres.enabled: true to connect)")
	}

	// -- directory cache (require postgres) --
	// Справочники обогащения грузятся в RAM при старте; Stage 1–2 будут читать их
	// отсюда. Пока — прогрев и валидация цепочки (схема → seed → загрузка); ссылку
	// получит движок дислокации при переносе обогащения.
	var (
		cfgCache     *service.ConfigCache
		dirCache     *service.DirectoryCache
		actualCache  *service.ActualCache
		dislRepo     port.DislocationRepository // интерфейс: при db==nil остаётся истинным nil
		status9Repo  port.Status9Repository
		status6Repo  port.Status6Repository
		historyRepo  port.HistoryRepository
		planRepo     port.PlanRepository
		journalRepo  port.JournalRepository
		adminRepo    port.AdminTablesRepository
		status9Cache *service.Status9Cache
		status6Cache *service.Status6Cache
	)
	if db != nil {
		dislRepo = gormrepo.NewDislocationRepository(db)
		status9Repo = gormrepo.NewStatus9Repository(db)
		status6Repo = gormrepo.NewStatus6Repository(db)
		historyRepo = gormrepo.NewHistoryRepository(db)
		planRepo = gormrepo.NewPlanRepository(db)
		journalRepo = gormrepo.NewJournalRepository(db)
		adminRepo = gormrepo.NewAdminTablesRepository(db)
		dirCache = service.NewDirectoryCache(gormrepo.NewDirectoryRepository(db))
		if err := dirCache.Load(context.Background()); err != nil {
			return fmt.Errorf("directory cache: %w", err)
		}
		stationsN, cargoOpsN, cargoN, markaN, portsN, routeSpeedN, naznachN := dirCache.Counts()
		log.Info("directory cache loaded",
			zap.Int("stations", stationsN),
			zap.Int("cargo_operations", cargoOpsN),
			zap.Int("cargo", cargoN),
			zap.Int("marka", markaN),
			zap.Int("ports", portsN),
			zap.Int("route_speed", routeSpeedN),
			zap.Int("naznach_station", naznachN),
		)

		// Настроечная таблица (data_source, client_settings) — в RAM при старте.
		// Слой приёма (загрузка/обработка ЛК) читает контроль отсюда.
		cfgCache = service.NewConfigCache(gormrepo.NewConfigRepository(db))
		if err := cfgCache.Load(context.Background()); err != nil {
			return fmt.Errorf("config cache: %w", err)
		}
		srcTotal, srcEnabled := cfgCache.Counts()
		log.Info("config cache loaded",
			zap.Int("data_sources", srcTotal),
			zap.Int("enabled", srcEnabled),
			zap.String("client", cfgCache.Settings().ClientName),
		)

		// Профили станций плана из настроечной таблицы → реестр парсера (расхардка
		// builtinProfiles). Пусто → у парсера остаётся builtin-fallback.
		log.Info("plan profiles applied", zap.Int("profiles", service.ApplyPlanProfiles(cfgCache)))

		// Актуальная мапа дислокации (текущий снимок) — в RAM при старте. Основа
		// Stage 2 (сравнение нового батча с актуальным). Пока — прогрев.
		actualCache = service.NewActualCache(dislRepo)
		if err := actualCache.Load(context.Background()); err != nil {
			return fmt.Errorf("actual cache: %w", err)
		}
		log.Info("actual cache loaded", zap.Int("vagons", actualCache.Count()))

		// Write-through RAM-кэши таблиц состояния (кандидаты status9, доноры status6):
		// чтение/сопоставление — из RAM, запись — в БД+RAM. Прогрев на старте.
		status9Cache = service.NewStatus9Cache(status9Repo)
		if err := status9Cache.Load(context.Background()); err != nil {
			return fmt.Errorf("status9 cache: %w", err)
		}
		status6Cache = service.NewStatus6Cache(status6Repo)
		if err := status6Cache.Load(context.Background()); err != nil {
			return fmt.Errorf("status6 cache: %w", err)
		}
		log.Info("state caches loaded",
			zap.Int("status9", status9Cache.Count()), zap.Int("status6", status6Cache.Count()))
	}

	// -- auth middleware (optional) --
	var jwtMW *middleware.KeycloakJWT
	if cfg.Keycloak.Enabled {
		jwtMW = middleware.NewKeycloakJWT(cfg.Keycloak)
	} else {
		log.Warn("keycloak disabled — /api routes are served WITHOUT auth")
	}

	// -- http server --
	// Metrics get a dedicated port unless metrics.port == http.port.
	metricsOnMain := cfg.Metrics.Port == cfg.HTTP.Port
	srv, asuIngest, refSvc := server.Build(cfg, sqlDB, cfgCache, dirCache, dislRepo, actualCache, status9Cache, status6Cache, historyRepo, planRepo, journalRepo, adminRepo, jwtMW, log, metricsOnMain)

	var metricsSrv *http.Server
	if !metricsOnMain {
		metricsSrv = server.NewMetricsServer(cfg.HTTP.Host, cfg.Metrics.Port)
	}

	// -- фоновые воркеры --
	// Крон-тикеры в процессе сервера (RAM-кэши/интеграции работают здесь же).
	// Останавливаются отменой bgCtx при shutdown.
	bgCtx, bgCancel := context.WithCancel(context.Background())
	var workers []worker.Worker

	// Забор дислокации из АСУ. Источники — в data_source; если включённых нет,
	// Pull вернёт ErrNoASUSource — тихо пропускаем.
	if cfg.ASU.Enabled && asuIngest != nil {
		job := func(ctx context.Context) error {
			_, err := asuIngest.Pull(ctx, domain.TriggerScheduled)
			if errors.Is(err, service.ErrNoASUSource) {
				log.Debug("asu-pull: источник не настроен/выключен — пропуск")
				return nil
			}
			return err
		}
		// Тики выровнены по стеночным часам: pull_offset от начала часа
		// (10m/5m → :05, :15, ...), независимо от момента старта процесса.
		workers = append(workers, worker.NewAlignedCronWorker("asu-pull", cfg.ASU.PullInterval, cfg.ASU.PullOffset, log, job))
	}

	// Инкремент памяток на подачу/уборку (пока только лог, без сохранения).
	if cfg.Reference.Enabled && refSvc != nil {
		workers = append(workers, worker.NewCronWorker("reference-update", cfg.Reference.PullInterval, log, refSvc.PullUpdates))
	}

	if len(workers) > 0 {
		go worker.Run(bgCtx, log, workers...)
		log.Info("background workers started", zap.Int("count", len(workers)))
	}

	// -- graceful shutdown --
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info("listening", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("server error", zap.Error(err))
		}
	}()

	if metricsSrv != nil {
		go func() {
			log.Info("metrics listening", zap.String("addr", metricsSrv.Addr))
			if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("metrics server error", zap.Error(err))
			}
		}()
	}

	<-quit
	log.Info("shutting down...")
	bgCancel() // останавливаем фоновые воркеры (крон АСУ)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()

	if metricsSrv != nil {
		if err := metricsSrv.Shutdown(ctx); err != nil {
			log.Error("metrics graceful shutdown", zap.Error(err))
		}
	}

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	log.Info("stopped")
	return nil
}
