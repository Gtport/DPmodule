package handler

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/internal/domain"
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
	g.GET("/reports/nmtp", h.report)
	g.GET("/reports/nmtp/terminals", h.terminals)
	g.GET("/reports/nmtp/excel", h.excel)
	g.POST("/reports/nmtp/excel", h.excelEdited)
	g.POST("/reports/nmtp/move", h.move)
}

// naznachOnly — режим отбора из query: mode=naznach («скрыть перестановки»,
// строго по назначению, как gtport UseNaznachOnly); иначе получатель ИЛИ назначение.
func naznachOnly(c *gin.Context) bool { return c.Query("mode") == "naznach" }

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

// report godoc
// @Summary  «Подход вагонов» по форме порта (НМТП): данные формы для экрана
// @Tags     reports
// @Security BearerAuth
// @Param    terminal query string true  "терминал (ports.name_s)"
// @Param    mode     query string false "naznach — скрыть перестановки (строго по назначению)"
// @Success  200 {object} domain.NmtpReport
// @Failure  400 {object} object "терминал не задан / неизвестен / раскладка не настроена"
// @Router   /api/v1/reports/nmtp [get]
func (h *nmtpHandler) report(c *gin.Context) {
	terminal := c.Query("terminal")
	if terminal == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "не задан терминал"})
		return
	}
	rep, err := h.svc.Report(c.Request.Context(), terminal, naznachOnly(c))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rep)
}

// excel godoc
// @Summary  «Подход вагонов» по форме порта (НМТП): книга .xlsx из текущего снимка
// @Tags     reports
// @Security BearerAuth
// @Param    terminal query string true  "терминал (ports.name_s)"
// @Param    mode     query string false "naznach — скрыть перестановки (строго по назначению)"
// @Success  200 {file} binary "книга .xlsx"
// @Failure  400 {object} object "терминал не задан / неизвестен / раскладка не настроена"
// @Router   /api/v1/reports/nmtp/excel [get]
func (h *nmtpHandler) excel(c *gin.Context) {
	terminal := c.Query("terminal")
	if terminal == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "не задан терминал"})
		return
	}
	rep, err := h.svc.Report(c.Request.Context(), terminal, naznachOnly(c))
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

// excelEdited godoc
// @Summary  Книга .xlsx из ПРАВЛЕНОГО на экране отчёта (правки нигде не хранятся)
// @Tags     reports
// @Security BearerAuth
// @Param    report body domain.NmtpReport true "отчёт с ручными правками (как отображён)"
// @Success  200 {file} binary "книга .xlsx"
// @Router   /api/v1/reports/nmtp/excel [post]
func (h *nmtpHandler) excelEdited(c *gin.Context) {
	var rep domain.NmtpReport
	if err := c.ShouldBindJSON(&rep); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "не разобран отчёт: " + err.Error()})
		return
	}
	if rep.Terminal == "" || len(rep.Columns) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "пустой отчёт (нет терминала или колонок)"})
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

// nmtpMoveRequest — перенос поезда в колонку. Ключ строки — как в агрегации
// отчёта: индекс + станция + прогноз. column_id = 0 — снять привязку.
// from_column_id сужает перенос до одной группы состава: id исходной колонки,
// -1 — «прочее», 0 — весь состав (сборные поезда).
type nmtpMoveRequest struct {
	Terminal     string            `json:"terminal" binding:"required"`
	Index        string            `json:"index" binding:"required"`
	StationOper  string            `json:"station_oper" binding:"required"`
	Prog         *domain.LocalTime `json:"prog" binding:"required"`
	ColumnID     int64             `json:"column_id"`
	FromColumnID int64             `json:"from_column_id"`
}

// move godoc
// @Summary  Перенос поезда в колонку формы: привязка вагонов состава (nmtp_vagon_column)
// @Tags     reports
// @Security BearerAuth
// @Param    body body nmtpMoveRequest true "поезд (ключ строки) и колонка; column_id=0 — вернуть по правилам"
// @Success  200 {object} object "vagons — сколько вагонов привязано/отвязано"
// @Router   /api/v1/reports/nmtp/move [post]
func (h *nmtpHandler) move(c *gin.Context) {
	var req nmtpMoveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "не разобран запрос: " + err.Error()})
		return
	}
	who := "unknown"
	if cl := auth.ClaimsFromContext(c.Request.Context()); cl != nil && cl.Username != "" {
		who = cl.Username
	}
	n, err := h.svc.MoveTrain(c.Request.Context(), req.Terminal, req.Index, req.StationOper,
		req.Prog, req.ColumnID, req.FromColumnID, who)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"vagons": n})
}
