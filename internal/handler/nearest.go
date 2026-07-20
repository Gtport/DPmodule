package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// nearestHandler — блок «Ближайшие поезда» домашней страницы (перенос gtport
// Nearest): агрегация подходящих поездов из RAM-снимка, только чтение.
type nearestHandler struct {
	svc *service.NearestService
}

func NewNearestHandler(svc *service.NearestService) *nearestHandler {
	return &nearestHandler{svc: svc}
}

func (h *nearestHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/nearest", h.trains)
}

// trains godoc
// @Summary  Ближайшие поезда в подходе (снимок, время план→прогноз→расчёт)
// @Tags     dislocation
// @Security BearerAuth
// @Param    naznach query string false "терминалы через запятую; пусто — все"
// @Param    limit   query int    false "максимум поездов (дефолт 50)"
// @Success  200 {array} service.NearestTrainDTO
// @Router   /api/v1/dislocation/nearest [get]
func (h *nearestHandler) trains(c *gin.Context) {
	var naznach []string
	if raw := strings.TrimSpace(c.Query("naznach")); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			if s = strings.TrimSpace(s); s != "" {
				naznach = append(naznach, s)
			}
		}
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	c.JSON(http.StatusOK, h.svc.Trains(c.Request.Context(), naznach, limit))
}
