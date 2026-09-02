package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// gu2bHandler — уведомления ГУ-2б (факт выгрузки): ручной триггер
// крон-инкремента, по образцу памяток (POST /reference/update/pull).
type gu2bHandler struct {
	svc *service.GU2BService
}

func NewGU2BHandler(svc *service.GU2BService) *gu2bHandler {
	return &gu2bHandler{svc: svc}
}

func (h *gu2bHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.POST("/gu2b/update/pull", h.updatePull) // ручной триггер крон-инкремента
}

// updatePull godoc
// @Summary  Инкремент уведомлений ГУ-2б по всем клиентам: приём + (при gu2b.apply) уточнение вех выгрузки
// @Tags     gu2b
// @Security BearerAuth
// @Success  200 {object} object "по клиенту: уведомлений/вагонов, применено/пропущено с причинами, дыры нумерации"
// @Failure  502 {object} object "провайдер недоступен / ошибка забора или записи"
// @Router   /api/v1/gu2b/update/pull [post]
func (h *gu2bHandler) updatePull(c *gin.Context) {
	res, err := h.svc.PullUpdatesDetailed(c.Request.Context())
	if err != nil {
		// Часть клиентов могла отработать — отдаём и результаты, и ошибку.
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "results": res})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "results": res})
}
