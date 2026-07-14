package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// referenceHandler — памятки на подачу/уборку из внешнего сервиса. По номеру —
// ручной забор; крон-инкремент по клиентам дёргается фоновым воркером, здесь же —
// ручной триггер. На этом этапе данные только логируются, не сохраняются.
type referenceHandler struct {
	svc *service.ReferenceService
}

func NewReferenceHandler(svc *service.ReferenceService) *referenceHandler {
	return &referenceHandler{svc: svc}
}

func (h *referenceHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/reference", h.byNumber)                // ?number=...
	g.POST("/reference/update/pull", h.updatePull) // ручной триггер крон-инкремента
}

// byNumber godoc
// @Summary  Памятка по номеру (забор у провайдера; пока не сохраняется)
// @Tags     reference
// @Security BearerAuth
// @Param    number query string true "номер памятки (NUMBER_PAMYATKA)"
// @Success  200 {object} object
// @Failure  400 {object} object "не задан number"
// @Failure  502 {object} object "провайдер недоступен / ошибка забора"
// @Router   /api/v1/reference [get]
func (h *referenceHandler) byNumber(c *gin.Context) {
	number := c.Query("number")
	if number == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "требуется параметр number"})
		return
	}
	n, err := h.svc.FetchByNumber(c.Request.Context(), number)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "received", "number": number, "bytes": n})
}

// updatePull godoc
// @Summary  Крон-инкремент памяток по всем клиентам (забор; пока не сохраняется)
// @Tags     reference
// @Security BearerAuth
// @Success  200 {object} object
// @Failure  502 {object} object "провайдер недоступен / ошибка забора"
// @Router   /api/v1/reference/update/pull [post]
func (h *referenceHandler) updatePull(c *gin.Context) {
	if err := h.svc.PullUpdates(c.Request.Context()); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "received"})
}
