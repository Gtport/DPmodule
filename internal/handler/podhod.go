package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// podhodHandler — отчёт «Подход» страницы «Справки и отчёты» (перенос gtport
// PortReportTable), только чтение, все авторизованные роли.
type podhodHandler struct {
	svc *service.PodhodService
}

func NewPodhodHandler(svc *service.PodhodService) *podhodHandler {
	return &podhodHandler{svc: svc}
}

func (h *podhodHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/reports/podhod", h.report)
	g.GET("/reports/podhod/presets", h.presets)
}

// report godoc
// @Summary  Отчёт «Подход»: поезда, идущие на терминал (двухуровневая группировка из снимка)
// @Tags     reports
// @Security BearerAuth
// @Produce  json
// @Param    terminal query string true  "терминал (ports.name_s: АЭ/ГУТ-2/УТ-1)"
// @Param    clients  query string false "фильтр клиентов, имена через | (формат gtport client_filter)"
// @Success  200 {object} service.PodhodReport
// @Router   /api/v1/reports/podhod [get]
func (h *podhodHandler) report(c *gin.Context) {
	rep, err := h.svc.Report(c.Query("terminal"), c.Query("clients"))
	if err != nil {
		// Единственный источник ошибок — валидация параметров (данные в RAM).
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rep)
}

// presets godoc
// @Summary  Пресеты отчёта «Подход» (клиентские варианты карточек, напр. «Марис»)
// @Tags     reports
// @Security BearerAuth
// @Produce  json
// @Success  200 {array} domain.ReportPreset
// @Router   /api/v1/reports/podhod/presets [get]
func (h *podhodHandler) presets(c *gin.Context) {
	list, err := h.svc.Presets(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if list == nil {
		list = []domain.ReportPreset{}
	}
	c.JSON(http.StatusOK, list)
}
