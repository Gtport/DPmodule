package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// mapHandler — экран «Карта»: группы поездов с координатами, вагоны группы по
// требованию, пометка диспетчера. Подложка (тайлы) — отдельный публичный
// хендлер tiles.go: она без JWT, данные дислокации — только здесь, под JWT.
type mapHandler struct {
	svc *service.MapService
}

func NewMapHandler(svc *service.MapService) *mapHandler {
	return &mapHandler{svc: svc}
}

func (h *mapHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/map", h.data)
	g.GET("/dislocation/map/wagons", h.wagons)
	g.POST("/dislocation/map/mark", h.mark)
}

// data godoc
// @Summary  Карта: все группы «поезд на станции» с координатами + терминалы для чипов
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} service.MapDataDTO
// @Router   /api/v1/dislocation/map [get]
func (h *mapHandler) data(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Data(c.Request.Context()))
}

// wagons godoc
// @Summary  Карта: вагоны одной группы (drill-down из попапа маркера)
// @Tags     dislocation
// @Security BearerAuth
// @Param    key query string true "ключ группы (key из /dislocation/map)"
// @Success  200 {object} service.MapWagonsDTO
// @Router   /api/v1/dislocation/map/wagons [get]
func (h *mapHandler) wagons(c *gin.Context) {
	key := c.Query("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "не указан ключ группы (?key=)"})
		return
	}
	c.JSON(http.StatusOK, h.svc.Wagons(key))
}

// mark godoc
// @Summary  Карта: пометка выбранных групп (текст+цвет в info_1/info_2; оба пустых = снять)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} service.RearrApplyResult
// @Router   /api/v1/dislocation/map/mark [post]
func (h *mapHandler) mark(c *gin.Context) {
	var req service.MapMarkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "некорректный JSON: " + err.Error()})
		return
	}
	res, err := h.svc.ApplyMark(c.Request.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrBadMap):
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		case errors.Is(err, service.ErrNotReady):
			c.JSON(http.StatusConflict, ErrorResponse{Error: err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, res)
}
