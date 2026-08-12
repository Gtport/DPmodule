package handler

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/domain"
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
	// Станции плана подвода (plan_profile: mode='planned' с непустым plan_code) —
	// вкладки экрана «План подвода». Пустой список = у клиента плана подвода
	// нет, фронт прячет пункт меню (у бесплановой станции Stage 4 живёт на
	// перерабатывающей способности, экран плана не нужен).
	PlanStations []planStationDTO `json:"plan_stations"`
}

// planStationDTO — вкладка станции плана: code — plan_code (ma/nk, ключ API
// плана), label — человекочитаемое имя станции из профиля.
type planStationDTO struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// planStations собирает вкладки станций плана из настроечных профилей.
// Порядок стабильный — по code (у трёх терминалов: ma «Мыс Астафьева»,
// nk «Находка» — как в прежнем хардкоде фронта).
func planStations(profiles []domain.PlanProfile) []planStationDTO {
	out := make([]planStationDTO, 0, len(profiles))
	for _, p := range profiles {
		code := strings.ToLower(strings.TrimSpace(p.PlanCode))
		if code == "" || p.Mode != domain.PlanModePlanned {
			continue
		}
		label := strings.TrimSpace(p.StationName)
		if label == "" {
			label = strings.ToUpper(code)
		}
		out = append(out, planStationDTO{Code: code, Label: label})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
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
		PlanStations:         planStations(h.cfg.PlanProfiles()),
	})
}
