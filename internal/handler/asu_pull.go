package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// asuPullHandler — автозагрузка дислокации из АСУ-АСУ. Дёргается по cron (каждые
// 10 мин) с JWT сервис-аккаунта; тянет всех клиентов, сверяет метки, пересобирает снимок.
type asuPullHandler struct {
	ingest *service.ASUIngest
}

func NewASUPullHandler(ingest *service.ASUIngest) *asuPullHandler {
	return &asuPullHandler{ingest: ingest}
}

func (h *asuPullHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.POST("/dislocation/asu/pull", h.pull)
}

// pull godoc
// @Summary  Автозагрузка дислокации из АСУ-АСУ (cron): забор + сверка меток + снимок
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} object
// @Failure  409 {object} object "рассогласование меток формирования источников"
// @Failure  502 {object} object "АСУ недоступна / ошибка забора"
// @Failure  503 {object} object "источник АСУ не настроен"
// @Router   /api/v1/dislocation/asu/pull [post]
func (h *asuPullHandler) pull(c *gin.Context) {
	res, err := h.ingest.Pull(c.Request.Context())
	if err != nil {
		c.JSON(statusForASUError(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

// statusForASUError маппит доменные ошибки автозагрузки в HTTP-статусы.
func statusForASUError(err error) int {
	switch {
	case errors.Is(err, service.ErrSourceSkew), errors.Is(err, service.ErrNoFormationTS),
		errors.Is(err, service.ErrDataLoss), errors.Is(err, service.ErrSourceNotNewer),
		errors.Is(err, service.ErrDislTooStale), errors.Is(err, service.ErrDislOlderThanCurrent):
		return http.StatusConflict
	case errors.Is(err, service.ErrNoASUSource):
		return http.StatusServiceUnavailable
	default:
		// Ошибки забора/разбора (АСУ недоступна, битый JSON) — проблема вышестоящего сервиса.
		return http.StatusBadGateway
	}
}
