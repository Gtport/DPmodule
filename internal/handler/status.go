package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// statusHandler — статус-панель: актуальность дислокации (по терминалам) и планов
// подвода. Данные — из единого журнала событий.
type statusHandler struct {
	svc *service.StatusService
}

func NewStatusHandler(svc *service.StatusService) *statusHandler {
	return &statusHandler{svc: svc}
}

func (h *statusHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/status", h.status)
}

// status godoc
// @Summary  Статус-панель: актуальность дислокации и планов подвода
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} object
// @Router   /api/v1/dislocation/status [get]
func (h *statusHandler) status(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Status(c.Request.Context()))
}
