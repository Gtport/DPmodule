package server

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/internal/config"
	"github.com/Gtport/DPmodule/internal/handler"
	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/internal/service"
	"github.com/Gtport/DPmodule/pkg/metrics"
	"github.com/Gtport/DPmodule/pkg/middleware"
)

// Build constructs the http.Server with all routes and middleware wired up.
// mountMetrics controls whether /metrics is served on this (main) server;
// when false, metrics are served on a dedicated server (see NewMetricsServer).
func Build(
	cfg *config.Config,
	db *sql.DB,
	cfgCache *service.ConfigCache,
	dirCache *service.DirectoryCache,
	dislRepo port.DislocationRepository,
	actualCache *service.ActualCache,
	status9Cache *service.Status9Cache,
	status6Cache *service.Status6Cache,
	historyRepo port.HistoryRepository,
	planRepo port.PlanRepository,
	jwtMW *middleware.KeycloakJWT,
	log *zap.Logger,
	mountMetrics bool,
) *http.Server {
	if cfg.App.Env != "dev" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()

	// ---- global middleware ----
	router.Use(
		middleware.InjectLogger(log),
		middleware.Recover(log),
		middleware.RequestID(),
		middleware.RequestLogger(),
		metrics.Middleware(),
	)

	// ---- system routes (no auth) ----
	handler.NewHealthHandler(db).RegisterRoutes(router)
	if mountMetrics {
		router.GET("/metrics", metrics.Handler())
	}
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// ---- protected API routes ----
	// jwtMW may be nil when keycloak is disabled — guard so the template still
	// boots and serves system routes. Реальные маршруты (dislocation и т.д.)
	// монтируются здесь, в группе /api/v1.
	api := router.Group("/api/v1")
	if jwtMW != nil {
		api.Use(jwtMW.Middleware())
	}
	handler.NewMeHandler().RegisterRoutes(api)

	// Приём файлов ЛК (шаг 1) — только если справочники и настроечная таблица
	// загружены (требует БД). Формат — из ConfigCache, «чей файл» (ОКПО→терминалы)
	// — из DirectoryCache (ports).
	if cfgCache != nil && dirCache != nil {
		lkIntake := service.NewLKIntake(cfgCache, dirCache, cfg.Storage.BaseDir)
		handler.NewLKUploadHandler(lkIntake).RegisterRoutes(api)

		// Шаг 2 (обработка в снимок) — требует репозиторий дислокации (БД).
		if dislRepo != nil {
			proc := service.NewLKProcessor(lkIntake, dislRepo, actualCache, status9Cache, status6Cache, historyRepo)
			handler.NewLKProcessHandler(proc).RegisterRoutes(api)

			// Приём плана подвода: разбор + матч + простановка PlanMsk в снимок.
			// Целевые площадки — из DirectoryCache (ports.plan_code).
			planProc := service.NewPlanProcessor(dirCache, dislRepo, actualCache, planRepo, cfg.Storage.BaseDir)
			handler.NewPlanUploadHandler(planProc).RegisterRoutes(api)
		}
	}

	return &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.HTTP.Host, cfg.HTTP.Port),
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}
}

// NewMetricsServer returns a minimal http.Server that serves /metrics only,
// on its own port — kept off the public API surface.
func NewMetricsServer(host string, port int) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.StdHandler())
	return &http.Server{
		Addr:    fmt.Sprintf("%s:%d", host, port),
		Handler: mux,
	}
}
