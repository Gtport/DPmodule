package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// statusHandler — статус-панель: актуальность дислокации (по терминалам) и планов
// подвода. Данные — из единого журнала событий.
type statusHandler struct {
	svc *service.StatusService
}

func NewStatusHandler(svc *service.StatusService) *statusHandler {
	return &statusHandler{svc: svc}
}

func (h *statusHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/status", h.status)
	g.GET("/dislocation/journal", h.journal) // журнал обновлений дислокации за период
}

// status godoc
// @Summary  Статус-панель: актуальность дислокации и планов подвода
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} object
// @Router   /api/v1/dislocation/status [get]
func (h *statusHandler) status(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Status(c.Request.Context()))
}

// journal godoc
// @Summary  Журнал обновлений дислокации за период (источник, триггер, кто, когда, вагоны)
// @Tags     dislocation
// @Security BearerAuth
// @Param    from  query string false "начало периода (2006-01-02 или 2006-01-02T15:04:05, МСК)"
// @Param    to    query string false "конец периода (МСК)"
// @Param    limit query int    false "макс. записей (по умолчанию 200)"
// @Success  200 {object} object
// @Failure  400 {object} object
// @Router   /api/v1/dislocation/journal [get]
func (h *statusHandler) journal(c *gin.Context) {
	from, err := parsePeriodTS(c.Query("from"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный параметр from: " + err.Error()})
		return
	}
	to, err := parsePeriodTS(c.Query("to"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный параметр to: " + err.Error()})
		return
	}
	limit := 0
	if s := c.Query("limit"); s != "" {
		if limit, err = strconv.Atoi(s); err != nil || limit < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный параметр limit"})
			return
		}
	}

	items, err := h.svc.DislocationJournal(c.Request.Context(), from, to, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"from": c.Query("from"), "to": c.Query("to"), "items": items})
}

// parsePeriodTS разбирает границу периода: дата или дата-время (МСК naive). Пусто → nil.
func parsePeriodTS(s string) (*domain.LocalTime, error) {
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return domain.NewLocalTime(t), nil
		}
	}
	return nil, fmt.Errorf("ожидался формат 2006-01-02 или 2006-01-02T15:04:05, получено %q", s)
}
