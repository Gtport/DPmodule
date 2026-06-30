package handler

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

type healthHandler struct {
	db *sql.DB
}

func NewHealthHandler(db *sql.DB) *healthHandler {
	return &healthHandler{db: db}
}

func (h *healthHandler) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", h.health)
	r.GET("/ready", h.ready)
}

// health godoc
// @Summary  Liveness probe
// @Tags     system
// @Success  200  {object}  object
// @Router   /health [get]
func (h *healthHandler) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ready godoc
// @Summary  Readiness probe — checks DB connectivity
// @Tags     system
// @Success  200  {object}  object
// @Failure  503  {object}  object
// @Router   /ready [get]
func (h *healthHandler) ready(c *gin.Context) {
	// No DB wired (postgres disabled) — nothing to check, report ready.
	if h.db == nil {
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
		return
	}
	if err := h.db.PingContext(c.Request.Context()); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}
