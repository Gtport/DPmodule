package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// rearrangeHandler — экран «Перестановки/Переадресация».
type rearrangeHandler struct {
	svc *service.RearrangeService
}

func NewRearrangeHandler(svc *service.RearrangeService) *rearrangeHandler {
	return &rearrangeHandler{svc: svc}
}

func (h *rearrangeHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/rearrangement/groups", h.rearrangementGroups)
	g.POST("/dislocation/rearrangement/apply", h.applyRearrangement)
	g.GET("/dislocation/rearrangement/stations", h.stations)
	g.PATCH("/dislocation/rearrangement/stations", h.updateStationNaznach)
	g.GET("/dislocation/redirection/groups", h.redirectionGroups)
	g.POST("/dislocation/redirection/apply", h.applyRedirection)
}

// stations godoc
// @Summary  Панель станций перестановок: все пары справочника naznach_station
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {array} service.NaznachStationDTO
// @Router   /api/v1/dislocation/rearrangement/stations [get]
func (h *rearrangeHandler) stations(c *gin.Context) {
	rows, err := h.svc.Stations(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

// updateStationNaznach godoc
// @Summary  Смена дефолтного назначения пары станций (drag&drop/ПКМ панели)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} map[string]string
// @Router   /api/v1/dislocation/rearrangement/stations [patch]
func (h *rearrangeHandler) updateStationNaznach(c *gin.Context) {
	var req service.StationNaznachUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный JSON: " + err.Error()})
		return
	}
	if err := h.svc.UpdateStationNaznach(c.Request.Context(), req); err != nil {
		writeRearrangeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// rearrangementGroups godoc
// @Summary  Группировки вкладки «Перестановки» (?group_by=parent_index|collective_train)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} service.RearrGroupsDTO
// @Router   /api/v1/dislocation/rearrangement/groups [get]
func (h *rearrangeHandler) rearrangementGroups(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.RearrangementGroups(c.Query("group_by")))
}

// applyRearrangement godoc
// @Summary  Перестановка: выбранным вагонам — новый терминал (одна операция = один пересбор)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} service.RearrApplyResult
// @Router   /api/v1/dislocation/rearrangement/apply [post]
func (h *rearrangeHandler) applyRearrangement(c *gin.Context) {
	var req service.RearrApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный JSON: " + err.Error()})
		return
	}
	res, err := h.svc.ApplyRearrangement(c.Request.Context(), req)
	if err != nil {
		writeRearrangeError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// redirectionGroups godoc
// @Summary  Группировки вкладки «Переадресация» (с флагом available)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} service.RearrGroupsDTO
// @Router   /api/v1/dislocation/redirection/groups [get]
func (h *rearrangeHandler) redirectionGroups(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.RedirectionGroups())
}

// applyRedirection godoc
// @Summary  Переадресация: kind=own|ext|cancel (одна операция = один пересбор)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} service.RearrApplyResult
// @Router   /api/v1/dislocation/redirection/apply [post]
func (h *rearrangeHandler) applyRedirection(c *gin.Context) {
	var req service.RedirectApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный JSON: " + err.Error()})
		return
	}
	res, err := h.svc.ApplyRedirection(c.Request.Context(), req)
	if err != nil {
		writeRearrangeError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func writeRearrangeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrBadRearrange):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrNotReady):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
