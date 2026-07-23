package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/port"
	"github.com/Gtport/DPmodule/internal/service"
)

// maxHandler — исходящая рассылка в мессенджер MAX: health (проверка канала/токена),
// список чатов и рассылка текстовых форм. Картинка — следующая ветка.
//
// sender == nil — max.enabled=false (канал спит, не ошибка).
// chats == nil — нет БД (справочник чатов недоступен).
// broadcast == nil — нет транспорта ИЛИ справочника (рассылать нечем/некуда).
type maxHandler struct {
	sender    port.MessengerSender
	chats     *service.MaxChatService
	broadcast *service.MaxBroadcastService
}

func NewMaxHandler(sender port.MessengerSender, chats *service.MaxChatService, broadcast *service.MaxBroadcastService) *maxHandler {
	return &maxHandler{sender: sender, chats: chats, broadcast: broadcast}
}

func (h *maxHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/max/health", h.health)
	if h.chats != nil {
		g.GET("/max/chats", h.listChats)
	}
	if h.broadcast != nil {
		g.POST("/max/broadcast/text", h.broadcastText)
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

// broadcastTextRequest — рассылка готовой текстовой формы. Текст собирает фронт
// (как в gtport formatTextForCopy); сервер только разрешает адресатов и шлёт.
type broadcastTextRequest struct {
	Report   string `json:"report"`   // 'spravki' | 'oper' | 'plan'
	Terminal string `json:"terminal"` // ports.name_s; пусто — сводная форма
	Text     string `json:"text"`
}

// broadcastText godoc
// @Summary  Рассылка текстовой формы в чаты MAX по маршруту (форма×терминал)
// @Tags     max
// @Security BearerAuth
// @Param    body body handler.broadcastTextRequest true "форма, терминал, текст"
// @Success  200 {object} service.BroadcastResult
// @Failure  400 {object} handler.ErrorResponse
// @Failure  502 {object} handler.ErrorResponse
// @Router   /api/v1/max/broadcast/text [post]
func (h *maxHandler) broadcastText(c *gin.Context) {
	var req broadcastTextRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "неверное тело запроса: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Report) == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "не указан тип формы (report)"})
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "пустой текст рассылки"})
		return
	}

	res, err := h.broadcast.SendText(c.Request.Context(), req.Report, req.Terminal, req.Text)
	if err != nil {
		c.JSON(http.StatusBadGateway, ErrorResponse{Error: err.Error()})
		return
	}
	// Чаты нашлись, но ни одна отправка не прошла — это отказ канала, не успех.
	if res.AllFailed() {
		c.JSON(http.StatusBadGateway, res)
		return
	}
	c.JSON(http.StatusOK, res)
}
