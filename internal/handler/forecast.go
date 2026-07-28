package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// forecastHandler — экран «Новый прогноз»: сырьё доски одним ответом
// (едущие поезда с прогнозом + прибывшие за сутки + линии выгрузки).
type forecastHandler struct {
	board *service.ForecastBoard
}

func NewForecastHandler(board *service.ForecastBoard) *forecastHandler {
	return &forecastHandler{board: board}
}

func (h *forecastHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/forecast/board", h.forecastBoard)
}

// forecastBoard godoc
// @Summary  Доска «Новый прогноз»: поезда в подходе, прибывшие за сутки, линии выгрузки
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} service.ForecastBoardDTO
// @Router   /api/v1/dislocation/forecast/board [get]
func (h *forecastHandler) forecastBoard(c *gin.Context) {
	dto, err := h.board.Board(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, dto)
}
