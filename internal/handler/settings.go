package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// settingsHandler — клиентские настройки интерфейса для фронта
// (client_settings.extra.ui через ConfigCache).
type settingsHandler struct {
	cfg *service.ConfigCache
}

func NewSettingsHandler(cfg *service.ConfigCache) *settingsHandler {
	return &settingsHandler{cfg: cfg}
}

func (h *settingsHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/settings/ui", h.ui)
}

// uiSettingsDTO — настройки интерфейса, которые фронт читает один раз на старте.
type uiSettingsDTO struct {
	// Шкала ручного ввода времени прибытия/выгрузки: "jd" | "msk"
	// (дефолт переключателя в диалогах; диспетчер может сменить на сессию).
	TimeBase string `json:"time_base"`
}

// ui godoc
// @Summary  Настройки интерфейса (client_settings.extra.ui): шкала ввода времени
// @Tags     settings
// @Security BearerAuth
// @Success  200 {object} uiSettingsDTO
// @Router   /api/v1/settings/ui [get]
func (h *settingsHandler) ui(c *gin.Context) {
	c.JSON(http.StatusOK, uiSettingsDTO{
		TimeBase: h.cfg.Settings().UI.TimeBaseOrDefault(),
	})
}
