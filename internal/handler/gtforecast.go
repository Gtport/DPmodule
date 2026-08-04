package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

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
