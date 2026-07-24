package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// brosHandler — экран «Брошенные поезда». В этой ветке только справочник кодов
// бросания (read, всем авторизованным). Активные броски, журнал и отчёт —
// следующие ветки.
type brosHandler struct {
	codes *service.BrosReasonCodes
}

func NewBrosHandler(codes *service.BrosReasonCodes) *brosHandler {
	return &brosHandler{codes: codes}
}

func (h *brosHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/bros/reason-codes", h.reasonCodes)
}

// reasonCodes godoc
// @Summary  Справочник кодов бросания (классификатор РЖД)
// @Tags     bros
// @Security BearerAuth
// @Success  200 {object} object
// @Router   /api/v1/dislocation/bros/reason-codes [get]
func (h *brosHandler) reasonCodes(c *gin.Context) {
	codes, err := h.codes.Codes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"codes": codes, "count": len(codes)})
}
