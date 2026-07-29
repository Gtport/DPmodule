package handler

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/report"
	"github.com/Gtport/DPmodule/internal/service"
)

// nmtpHandler — отчёт «Подход вагонов» по форме порта (НМТП): книга .xlsx по
// раскладке справочника nmtp_column (собирает сервер) + список терминалов с
// настроенной раскладкой (кнопки карточки на «Справках и отчётах»).
type nmtpHandler struct {
	svc *service.NmtpService
}

func NewNmtpHandler(svc *service.NmtpService) *nmtpHandler {
	return &nmtpHandler{svc: svc}
}

func (h *nmtpHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/reports/nmtp/terminals", h.terminals)
	g.GET("/reports/nmtp/excel", h.excel)
}

// terminals godoc
// @Summary  Терминалы с настроенной раскладкой НМТП-отчёта (nmtp_column)
// @Tags     reports
// @Security BearerAuth
// @Success  200 {array} string
// @Router   /api/v1/reports/nmtp/terminals [get]
func (h *nmtpHandler) terminals(c *gin.Context) {
	list, err := h.svc.Terminals(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// excel godoc
// @Summary  «Подход вагонов» по форме порта (НМТП): книга .xlsx из текущего снимка
// @Tags     reports
// @Security BearerAuth
// @Param    terminal query string true "терминал (ports.name_s)"
// @Success  200 {file} binary "книга .xlsx"
// @Failure  400 {object} object "терминал не задан / неизвестен / раскладка не настроена"
// @Router   /api/v1/reports/nmtp/excel [get]
func (h *nmtpHandler) excel(c *gin.Context) {
	terminal := c.Query("terminal")
	if terminal == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "не задан терминал"})
		return
	}
	rep, err := h.svc.Report(c.Request.Context(), terminal)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, filename, err := report.NmtpXLSX(rep)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	c.Data(http.StatusOK, xlsxContentType, data)
}
