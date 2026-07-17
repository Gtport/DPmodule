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
	g.GET("/dislocation/redirection/groups", h.redirectionGroups)
	g.POST("/dislocation/redirection/apply", h.applyRedirection)
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
