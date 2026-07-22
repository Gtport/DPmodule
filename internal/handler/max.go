package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/port"
)

// maxHandler — исходящая рассылка в мессенджер MAX. На этом этапе (ветка
// feat/max-adapter) только health-ручка: проверка, что канал настроен и токен
// валиден («проверка боем»). Рассылка текста/картинки — следующие ветки.
//
// sender == nil означает max.enabled=false в конфиге: ручка отвечает 200 с
// enabled=false, не ошибкой — это штатное «канал выключен», а не поломка.
type maxHandler struct {
	sender port.MessengerSender // nil, если MAX отключён в конфиге
}

func NewMaxHandler(sender port.MessengerSender) *maxHandler {
	return &maxHandler{sender: sender}
}

func (h *maxHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/max/health", h.health)
}

// health godoc
// @Summary  Состояние канала MAX (и проверка токена боем, если включён)
// @Tags     max
// @Security BearerAuth
// @Success  200 {object} object
// @Failure  502 {object} handler.ErrorResponse
// @Router   /api/v1/max/health [get]
func (h *maxHandler) health(c *gin.Context) {
	if h.sender == nil {
		c.JSON(http.StatusOK, gin.H{"service": "max", "enabled": false})
		return
	}
	if err := h.sender.Ping(c.Request.Context()); err != nil {
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"service": "max", "enabled": true, "status": "ok"})
}
