package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// missingHandler — экран «Пропавшие вагоны»: записи-8 из таблицы кандидатов.
type missingHandler struct {
	svc *service.MissingService
}

func NewMissingHandler(svc *service.MissingService) *missingHandler {
	return &missingHandler{svc: svc}
}

func (h *missingHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/missing", h.list)
}

// list godoc
// @Summary  Пропавшие вагоны (статус 8): последняя известная позиция
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {array} service.MissingVagonDTO
// @Router   /api/v1/dislocation/missing [get]
func (h *missingHandler) list(c *gin.Context) {
	rows, err := h.svc.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}
