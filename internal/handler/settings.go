package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// settingsHandler — клиентские настройки интерфейса для фронта
// (client_settings.extra.ui через ConfigCache) + флаги функций из файла
// конфига (map.enabled), которые фронт читает один раз на старте.
type settingsHandler struct {
	cfg          *service.ConfigCache
	mapEnabled   bool
	notifEnabled bool
}

func NewSettingsHandler(cfg *service.ConfigCache, mapEnabled, notifEnabled bool) *settingsHandler {
	return &settingsHandler{cfg: cfg, mapEnabled: mapEnabled, notifEnabled: notifEnabled}
}

func (h *settingsHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/settings/ui", h.ui)
}

// uiSettingsDTO — настройки интерфейса, которые фронт читает один раз на старте.
type uiSettingsDTO struct {
	// Шкала ручного ввода времени прибытия/выгрузки: "jd" | "msk"
	// (дефолт переключателя в диалогах; диспетчер может сменить на сессию).
	TimeBase string `json:"time_base"`
	// Экран «Карта» включён конфигом (map.enabled) — false прячет пункт меню.
	MapEnabled bool `json:"map_enabled"`
	// Уведомления (колокольчик) включены (notifications.enabled + есть БД) —
	// false прячет колокольчик в шапке.
	NotificationsEnabled bool `json:"notifications_enabled"`
}

// ui godoc
// @Summary  Настройки интерфейса (client_settings.extra.ui): шкала ввода времени
// @Tags     settings
// @Security BearerAuth
// @Success  200 {object} uiSettingsDTO
// @Router   /api/v1/settings/ui [get]
func (h *settingsHandler) ui(c *gin.Context) {
	c.JSON(http.StatusOK, uiSettingsDTO{
		TimeBase:             h.cfg.Settings().UI.TimeBaseOrDefault(),
		MapEnabled:           h.mapEnabled,
		NotificationsEnabled: h.notifEnabled,
	})
}
