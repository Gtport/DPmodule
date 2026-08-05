package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/service"
)

// gtForecastHandler — вкладка «Прогноз прибытия/выгрузки» страницы «Прогнозы»
// (перенос страницы «Прогноз GT» gtport): режимы по причальным станциям и
// серверная симуляция выгрузки (диаграммы Ганта).
type gtForecastHandler struct {
	svc *service.GtForecastService
}

func NewGtForecastHandler(svc *service.GtForecastService) *gtForecastHandler {
	return &gtForecastHandler{svc: svc}
}

func (h *gtForecastHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/gt-forecast/context", h.context)
	g.POST("/dislocation/gt-forecast/simulate", h.simulate)
	g.POST("/dislocation/gt-forecast/snapshots", h.snapshotSave)
	g.GET("/dislocation/gt-forecast/snapshots", h.snapshotList)
	g.GET("/dislocation/gt-forecast/snapshots/analytics", h.snapshotAnalytics)
	g.GET("/dislocation/gt-forecast/snapshots/:date", h.snapshotGet)
	g.DELETE("/dislocation/gt-forecast/snapshots/:date", h.snapshotDelete)
}

// context godoc
// @Summary  Режимы прогноза ГТ: причальные станции, терминалы, линии выгрузки со скоростями
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} service.GtContextDTO
// @Router   /api/v1/dislocation/gt-forecast/context [get]
func (h *gtForecastHandler) context(c *gin.Context) {
	dto, err := h.svc.Context(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, dto)
}

// simulate godoc
// @Summary  Пересчёт прогноза ГТ: очередь поездов режима + симуляция выгрузки по суткам
// @Tags     dislocation
// @Security BearerAuth
// @Param    request body service.GtSimulateRequest true "режим, дата начала, горизонт, скорости"
// @Success  200 {object} service.GtSimulateDTO
// @Router   /api/v1/dislocation/gt-forecast/simulate [post]
func (h *gtForecastHandler) simulate(c *gin.Context) {
	var req service.GtSimulateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректное тело запроса: " + err.Error()})
		return
	}
	dto, err := h.svc.Simulate(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, dto)
}

// snapshotSave godoc
// @Summary  Сохранить план прогноза ГТ на дату (сервер пересчитывает по входу сеанса; upsert по дате × станции)
// @Tags     dislocation
// @Security BearerAuth
// @Param    request body service.GtSnapshotSaveRequest true "дата плана, вход сеанса, журнал правок"
// @Success  200 {object} map[string]string
// @Router   /api/v1/dislocation/gt-forecast/snapshots [post]
func (h *gtForecastHandler) snapshotSave(c *gin.Context) {
	var req service.GtSnapshotSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректное тело запроса: " + err.Error()})
		return
	}
	savedBy := ""
	if cl := auth.ClaimsFromContext(c.Request.Context()); cl != nil {
		savedBy = cl.Username
	}
	if err := h.svc.SaveSnapshot(c.Request.Context(), req, savedBy); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "сохранено"})
}

// snapshotList godoc
// @Summary  Список сохранённых планов прогноза ГТ за период
// @Tags     dislocation
// @Security BearerAuth
// @Param    from    query string true  "YYYY-MM-DD"
// @Param    to      query string true  "YYYY-MM-DD"
// @Param    station query string false "код причальной станции (пусто — все)"
// @Success  200 {array} service.GtSnapshotMetaDTO
// @Router   /api/v1/dislocation/gt-forecast/snapshots [get]
func (h *gtForecastHandler) snapshotList(c *gin.Context) {
	from, to, err := parsePeriod(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	list, err := h.svc.ListSnapshots(c.Request.Context(), from, to, c.Query("station"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// snapshotGet godoc
// @Summary  Сохранённый план прогноза ГТ (архивный просмотр, read-only)
// @Tags     dislocation
// @Security BearerAuth
// @Param    date    path  string true "YYYY-MM-DD"
// @Param    station query string true "код причальной станции"
// @Success  200 {object} service.GtSnapshotDTO
// @Router   /api/v1/dislocation/gt-forecast/snapshots/{date} [get]
func (h *gtForecastHandler) snapshotGet(c *gin.Context) {
	date, err := time.Parse("2006-01-02", c.Param("date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date: " + err.Error()})
		return
	}
	dto, err := h.svc.GetSnapshot(c.Request.Context(), date, c.Query("station"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if dto == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "план на эту дату не сохранялся"})
		return
	}
	c.JSON(http.StatusOK, dto)
}

// snapshotDelete godoc
// @Summary  Удалить сохранённый план прогноза ГТ
// @Tags     dislocation
// @Security BearerAuth
// @Param    date    path  string true "YYYY-MM-DD"
// @Param    station query string true "код причальной станции"
// @Success  200 {object} map[string]string
// @Router   /api/v1/dislocation/gt-forecast/snapshots/{date} [delete]
func (h *gtForecastHandler) snapshotDelete(c *gin.Context) {
	date, err := time.Parse("2006-01-02", c.Param("date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date: " + err.Error()})
		return
	}
	if err := h.svc.DeleteSnapshot(c.Request.Context(), date, c.Query("station")); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "удалено"})
}

// snapshotAnalytics godoc
// @Summary  CSV-аналитика сохранённых планов за период (ZIP: trains / gantt_days / free_slots, прогноз vs факт)
// @Tags     dislocation
// @Security BearerAuth
// @Param    from    query string true  "YYYY-MM-DD"
// @Param    to      query string true  "YYYY-MM-DD"
// @Param    station query string false "код причальной станции (пусто — все)"
// @Success  200 {file} application/zip
// @Router   /api/v1/dislocation/gt-forecast/snapshots/analytics [get]
func (h *gtForecastHandler) snapshotAnalytics(c *gin.Context) {
	from, to, err := parsePeriod(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, err := h.svc.SnapshotAnalytics(c.Request.Context(), from, to, c.Query("station"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name := fmt.Sprintf("gt_analytics_%s_%s.zip", from.Format("2006-01-02"), to.Format("2006-01-02"))
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Data(http.StatusOK, "application/zip", data)
}

func parsePeriod(c *gin.Context) (time.Time, time.Time, error) {
	from, err := time.Parse("2006-01-02", c.Query("from"))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("from: %w", err)
	}
	to, err := time.Parse("2006-01-02", c.Query("to"))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("to: %w", err)
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("период задан наоборот: %s позже %s", c.Query("from"), c.Query("to"))
	}
	return from, to, nil
}
