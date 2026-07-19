package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// arrivalsHandler — «История прибывших» домашней страницы (перенос gtport
// /api/history/groups): чтение бизнес-истории vagon_history, только просмотр.
type arrivalsHandler struct {
	svc *service.ArrivalsService
}

func NewArrivalsHandler(svc *service.ArrivalsService) *arrivalsHandler {
	return &arrivalsHandler{svc: svc}
}

func (h *arrivalsHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/arrivals", h.groups)
	g.GET("/dislocation/terminals", h.terminals)
	g.PUT("/dislocation/arrivals/vagons", h.updateVagons)
}

// groups godoc
// @Summary  Прибывшие поезда за период (группы index_pp+date_prib из vagon_history)
// @Tags     dislocation
// @Security BearerAuth
// @Param    from    query string false "начало периода yyyy-MM-dd (дефолт: вчера)"
// @Param    to      query string false "конец периода yyyy-MM-dd (дефолт: сегодня)"
// @Param    naznach query string false "терминалы через запятую (АЭ,ГУТ-2); пусто — все"
// @Success  200 {object} service.ArrivalsDTO
// @Router   /api/v1/dislocation/arrivals [get]
func (h *arrivalsHandler) groups(c *gin.Context) {
	var naznach []string
	if raw := strings.TrimSpace(c.Query("naznach")); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			if s = strings.TrimSpace(s); s != "" {
				naznach = append(naznach, s)
			}
		}
	}
	res, err := h.svc.Groups(c.Request.Context(), c.Query("from"), c.Query("to"), naznach)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

// terminals godoc
// @Summary  Реестр терминалов с их станциями (раскладка домашней страницы, ports)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {array} service.TargetDTO
// @Router   /api/v1/dislocation/terminals [get]
func (h *arrivalsHandler) terminals(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Terminals())
}

// updateVagons godoc
// @Summary  Правка выбранных вагонов истории прибывших (прибытие/отмена/выгрузка/назначение)
// @Tags     dislocation
// @Security BearerAuth
// @Param    body body service.ArrivalsUpdateRequest true "vagon_ids + применяемые поля"
// @Success  200 {object} service.ArrivalsUpdateResult
// @Router   /api/v1/dislocation/arrivals/vagons [put]
func (h *arrivalsHandler) updateVagons(c *gin.Context) {
	var req service.ArrivalsUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректное тело запроса: " + err.Error()})
		return
	}
	res, err := h.svc.UpdateVagons(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, service.ErrArrivalsAccess) {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
