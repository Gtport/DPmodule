package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// referenceHandler — памятки ГУ-45 на подачу/уборку из внешнего сервиса.
// Инкремент раскладывает вехи подачи/уборки по рейсам vagon_history (крон +
// ручной триггер здесь); забор по номеру отдаёт сырой документ провайдера
// как есть — это другой контракт, в историю он не раскладывается.
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
// @Summary  Памятка по номеру — сырой документ провайдера (диагностика)
// @Tags     reference
// @Security BearerAuth
// @Param    number query string true  "номер памятки (NUMBER_PAMYATKA)"
// @Param    client query string false "код клиента у провайдера; по умолчанию первый из reference.clients"
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
	body, err := h.svc.FetchByNumber(c.Request.Context(), c.Query("client"), number)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

// updatePull godoc
// @Summary  Инкремент памяток по всем клиентам: разнос вех подачи/уборки по рейсам
// @Tags     reference
// @Security BearerAuth
// @Success  200 {object} object "по клиенту: разобрано памяток/вагонов, обновлено рейсов, пропущено с причинами"
// @Failure  502 {object} object "провайдер недоступен / ошибка забора или записи"
// @Router   /api/v1/reference/update/pull [post]
func (h *referenceHandler) updatePull(c *gin.Context) {
	res, err := h.svc.PullUpdatesDetailed(c.Request.Context())
	if err != nil {
		// Часть клиентов могла отработать — отдаём и результаты, и ошибку.
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "results": res})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "results": res})
}
