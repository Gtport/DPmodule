package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// planFormHandler — форма «План подвода» для экрана «Рассылка»: по терминалу
// сводная карточка (вчера факт + сегодня прогноз по подходу) и список поездов.
type planFormHandler struct {
	svc *service.PlanFormService
}

func NewPlanFormHandler(svc *service.PlanFormService) *planFormHandler {
	return &planFormHandler{svc: svc}
}

func (h *planFormHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/plan-form", h.form)
}

// form godoc
// @Summary  Форма «План подвода» по терминалам (вчера факт + сегодня прогноз + поезда)
// @Tags     dislocation
// @Security BearerAuth
// @Param    date query string false "ЖД-сутки yyyy-MM-dd; пусто — сегодня по МСК"
// @Success  200 {array} service.PlanFormTerminalDTO
// @Failure  400 {object} handler.ErrorResponse
// @Failure  500 {object} handler.ErrorResponse
// @Router   /api/v1/dislocation/plan-form [get]
func (h *planFormHandler) form(c *gin.Context) {
	date := time.Now()
	if raw := c.Query("date"); raw != "" {
		d, err := time.Parse("2006-01-02", raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "дата в формате yyyy-MM-dd"})
			return
		}
		date = d
	}
	res, err := h.svc.Form(c.Request.Context(), date)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
