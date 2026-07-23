package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/internal/service"
)

// maxHandler — исходящая рассылка в мессенджер MAX. Пока: health (проверка
// канала/токена) и список чатов для фронта. Сами рассылки текста/картинки —
// следующие ветки.
//
// sender == nil — max.enabled=false в конфиге (канал спит, не ошибка).
// chats == nil — нет БД (справочник чатов недоступен).
type maxHandler struct {
	sender port.MessengerSender // nil, если MAX отключён в конфиге
	chats  *service.MaxChatService
}

func NewMaxHandler(sender port.MessengerSender, chats *service.MaxChatService) *maxHandler {
	return &maxHandler{sender: sender, chats: chats}
}

func (h *maxHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/max/health", h.health)
	if h.chats != nil {
		g.GET("/max/chats", h.listChats)
	}
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

// listChats godoc
// @Summary  Справочник чатов MAX (для выбора адресатов рассылки)
// @Tags     max
// @Security BearerAuth
// @Success  200 {array} service.MaxChatDTO
// @Failure  500 {object} handler.ErrorResponse
// @Router   /api/v1/max/chats [get]
func (h *maxHandler) listChats(c *gin.Context) {
	chats, err := h.chats.Chats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, chats)
}
