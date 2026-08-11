package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// notificationsHandler — колокольчик уведомлений: список/бейдж/отметки.
// Видимость режет сервер по ролям claims (аудитории), фронт ничего не решает.
//
// ⚠️ PUT-ручки — мутации, глобальный RequireForWrites(AccessWrite) закрывает
// mark-read от клиентских ролей (client/client-dispatcher). Осознанно: все
// сегодняшние события адресованы oper/dicts/admin (⊆ AccessWrite), клиентские
// роли видят пустой колокольчик. Появятся уведомления для клиентов —
// понадобится исключение пути в мидлвари (та же оговорка у POST /history).
type notificationsHandler struct {
	svc *service.NotificationService
}

func NewNotificationsHandler(svc *service.NotificationService) *notificationsHandler {
	return &notificationsHandler{svc: svc}
}

func (h *notificationsHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/notifications", h.list)
	g.GET("/notifications/unread-count", h.unreadCount)
	g.PUT("/notifications/:id/read", h.markRead)
	g.PUT("/notifications/read-all", h.markAllRead)
}

// notificationDTO — уведомление в ответе API.
type notificationDTO struct {
	ID              int64             `json:"id"`
	Type            string            `json:"type"`
	Title           string            `json:"title"`
	Message         string            `json:"message"`
	ActionComponent string            `json:"action_component,omitempty"`
	ActionParams    any               `json:"action_params,omitempty"`
	CreatedAt       domain.LocalTime  `json:"created_at"`
	IsRead          bool              `json:"is_read"`
	ReadAt          *domain.LocalTime `json:"read_at,omitempty"`
}

func toNotificationDTO(n domain.UserNotification) notificationDTO {
	d := notificationDTO{
		ID: n.ID, Type: n.Type, Title: n.Title, Message: n.Message,
		ActionComponent: n.ActionComponent, CreatedAt: n.CreatedAt,
		IsRead: n.IsRead, ReadAt: n.ReadAt,
	}
	if len(n.ActionParams) > 0 {
		// jsonb уже валиден — отдаём как есть, без пересборки.
		d.ActionParams = json.RawMessage(n.ActionParams)
	}
	return d
}

// list godoc
// @Summary  Уведомления текущего пользователя (колокольчик)
// @Tags     notifications
// @Security BearerAuth
// @Param    unread query bool false "только непрочитанные"
// @Param    limit  query int  false "потолок выдачи (дефолт 100)"
// @Success  200 {array} notificationDTO
// @Router   /api/v1/notifications [get]
func (h *notificationsHandler) list(c *gin.Context) {
	claims := auth.ClaimsFromContext(c.Request.Context())
	unreadOnly := c.Query("unread") == "true"
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	list, err := h.svc.List(c.Request.Context(), claims, unreadOnly, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	out := make([]notificationDTO, 0, len(list))
	for _, n := range list {
		out = append(out, toNotificationDTO(n))
	}
	c.JSON(http.StatusOK, out)
}

// unreadCount godoc
// @Summary  Число непрочитанных уведомлений (бейдж колокольчика)
// @Tags     notifications
// @Security BearerAuth
// @Success  200 {object} map[string]int
// @Router   /api/v1/notifications/unread-count [get]
func (h *notificationsHandler) unreadCount(c *gin.Context) {
	claims := auth.ClaimsFromContext(c.Request.Context())
	n, err := h.svc.UnreadCount(c.Request.Context(), claims)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": n})
}

// markRead godoc
// @Summary  Отметить уведомление прочитанным (идемпотентно)
// @Tags     notifications
// @Security BearerAuth
// @Param    id path int true "id уведомления"
// @Success  200 {object} map[string]bool
// @Failure  400 {object} handler.ErrorResponse
// @Router   /api/v1/notifications/{id}/read [put]
func (h *notificationsHandler) markRead(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "неверный id уведомления"})
		return
	}
	claims := auth.ClaimsFromContext(c.Request.Context())
	if err := h.svc.MarkRead(c.Request.Context(), claims, id); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// markAllRead godoc
// @Summary  Отметить все видимые уведомления прочитанными
// @Tags     notifications
// @Security BearerAuth
// @Success  200 {object} map[string]int
// @Router   /api/v1/notifications/read-all [put]
func (h *notificationsHandler) markAllRead(c *gin.Context) {
	claims := auth.ClaimsFromContext(c.Request.Context())
	n, err := h.svc.MarkAllRead(c.Request.Context(), claims)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"marked": n})
}
