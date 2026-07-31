package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// trainsHandler — экран «Поезда в движении» (перенос gtport
// OperatorToolsDislocation): весь снимок трёхуровневым деревом, только чтение.
type trainsHandler struct {
	svc *service.TrainsService
}

func NewTrainsHandler(svc *service.TrainsService) *trainsHandler {
	return &trainsHandler{svc: svc}
}

func (h *trainsHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/trains", h.trains)
}

// trains godoc
// @Summary  Поезда в движении: снимок деревом «поезд → подгруппа → вагоны»
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} service.TrainsDTO
// @Router   /api/v1/dislocation/trains [get]
func (h *trainsHandler) trains(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Trains(c.Request.Context()))
}
