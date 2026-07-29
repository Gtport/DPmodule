package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// loadingHandler — отчёт «Погрузка» страницы «Справки и отчёты» (перенос gtport
// loading-report/loading-report-period одной ручкой), только чтение.
type loadingHandler struct {
	svc *service.LoadingReportService
}

func NewLoadingHandler(svc *service.LoadingReportService) *loadingHandler {
	return &loadingHandler{svc: svc}
}

func (h *loadingHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/reports/loading", h.report)
}

// report godoc
// @Summary  Отчёт «Погрузка»: дневные агрегаты погрузки в адрес терминалов за период
// @Tags     reports
// @Security BearerAuth
// @Produce  json
// @Param    from query string false "начало периода YYYY-MM-DD (дефолт: 1-е число месяца даты to)"
// @Param    to   query string false "конец периода YYYY-MM-DD, включительно (дефолт: сегодня МСК)"
// @Success  200 {object} service.LoadingReportDTO
// @Router   /api/v1/reports/loading [get]
func (h *loadingHandler) report(c *gin.Context) {
	to, ok := parseReportDate(c, c.Query("to"), time.Time(clock.Now()))
	if !ok {
		return
	}
	monthStart := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, to.Location())
	from, ok := parseReportDate(c, c.Query("from"), monthStart)
	if !ok {
		return
	}
	if to.Before(from) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "конец периода раньше начала"})
		return
	}
	rep, err := h.svc.Report(c.Request.Context(), domain.LocalTime(from), domain.LocalTime(to))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rep)
}
