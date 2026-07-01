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
	"github.com/Gtport/DPmodule/pkg/metrics"
	"github.com/Gtport/DPmodule/pkg/middleware"
)

// Build constructs the http.Server with all routes and middleware wired up.
// mountMetrics controls whether /metrics is served on this (main) server;
// when false, metrics are served on a dedicated server (see NewMetricsServer).
func Build(
	cfg *config.Config,
	db *sql.DB,
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

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTP.Port),
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}
}

// NewMetricsServer returns a minimal http.Server that serves /metrics only,
// on its own port — kept off the public API surface.
func NewMetricsServer(port int) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.StdHandler())
	return &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
}
