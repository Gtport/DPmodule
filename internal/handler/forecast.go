package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// forecastHandler — экран «Прогнозы»: сводка поездов с прогнозными полями Stage 3/4.
type forecastHandler struct {
	board *service.ForecastBoard
}

func NewForecastHandler(board *service.ForecastBoard) *forecastHandler {
	return &forecastHandler{board: board}
}

func (h *forecastHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/forecast", h.forecast)
}

// forecast godoc
// @Summary  Сводка прогнозов прибытия по поездам (Stage 3/4)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {array} service.ForecastTrain
// @Router   /api/v1/dislocation/forecast [get]
func (h *forecastHandler) forecast(c *gin.Context) {
	c.JSON(http.StatusOK, h.board.Trains())
}
