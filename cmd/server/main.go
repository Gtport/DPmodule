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
	gormrepo "github.com/Gtport/DPmodule/internal/repository/gorm"
	"github.com/Gtport/DPmodule/internal/server"
	"github.com/Gtport/DPmodule/internal/service"
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
	if db != nil {
		dirCache := service.NewDirectoryCache(gormrepo.NewDirectoryRepository(db))
		if err := dirCache.Load(context.Background()); err != nil {
			return fmt.Errorf("directory cache: %w", err)
		}
		stationsN, cargoOpsN, markaN, portsN := dirCache.Counts()
		log.Info("directory cache loaded",
			zap.Int("stations", stationsN),
			zap.Int("cargo_operations", cargoOpsN),
			zap.Int("marka", markaN),
			zap.Int("ports", portsN),
		)
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
	srv := server.Build(cfg, sqlDB, jwtMW, log, metricsOnMain)

	var metricsSrv *http.Server
	if !metricsOnMain {
		metricsSrv = server.NewMetricsServer(cfg.HTTP.Host, cfg.Metrics.Port)
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
