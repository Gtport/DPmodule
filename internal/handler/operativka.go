package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// operativkaHandler — карточка «Оперативка» домашней страницы (суточные счётчики
// по терминалам), только чтение.
type operativkaHandler struct {
	svc *service.OperativkaService
}

func NewOperativkaHandler(svc *service.OperativkaService) *operativkaHandler {
	return &operativkaHandler{svc: svc}
}

func (h *operativkaHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/operativka", h.snapshot)
}

// snapshot godoc
// @Summary  «Оперативка»: прибыло/выгружено по терминалам за вчера и сегодня (ЖД-сутки) + не выгружено (статус 10)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} service.OperativkaDTO
// @Router   /api/v1/dislocation/operativka [get]
func (h *operativkaHandler) snapshot(c *gin.Context) {
	res, err := h.svc.Snapshot(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
