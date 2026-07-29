package handler

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/report"
	"github.com/Gtport/DPmodule/internal/service"
)

// perestanovkaHandler — блок «Перестановки» страницы «Справки и отчёты»:
// выгрузка текущих перестановок на терминал (.xlsx собирает сервер, как
// повагонку) и «Факт перестановок» за период из vagon_history (JSON, Excel
// собирает фронт).
type perestanovkaHandler struct {
	svc *service.PerestanovkaService
}

func NewPerestanovkaHandler(svc *service.PerestanovkaService) *perestanovkaHandler {
	return &perestanovkaHandler{svc: svc}
}

func (h *perestanovkaHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/reports/perestanovka/excel", h.excel)
	g.GET("/reports/perestanovka/fact", h.fact)
}

// excel godoc
// @Summary  «Перестановка на {терминал}»: текущие перестановки книгой .xlsx (Поезда + Вагоны)
// @Tags     reports
// @Security BearerAuth
// @Param    terminal query string true "терминал-цель (ports.name_s)"
// @Success  200 {file} binary "книга .xlsx"
// @Failure  400 {object} object "терминал не задан / неизвестен / нет вагонов"
// @Router   /api/v1/reports/perestanovka/excel [get]
func (h *perestanovkaHandler) excel(c *gin.Context) {
	terminal := c.Query("terminal")
	if terminal == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "не задан терминал"})
		return
	}
	data, filename, err := h.svc.Excel(terminal)
	if err != nil {
		if errors.Is(err, report.ErrPerestanovkaEmpty) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "нет вагонов на перестановку в " + terminal})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	c.Data(http.StatusOK, xlsxContentType, data)
}

// fact godoc
// @Summary  «Факт перестановок» за период (строки vagon_history с gruzpol_s ≠ naznach)
// @Tags     reports
// @Security BearerAuth
// @Param    from     query string false "начало периода yyyy-MM-dd (дефолт: вчера)"
// @Param    to       query string false "конец периода yyyy-MM-dd (дефолт: сегодня)"
// @Param    by       query string false "поле периода: prib (дефолт) | vigr"
// @Param    terminal query string false "срез по терминалу-цели (naznach); пусто — все"
// @Success  200 {object} service.PerestanovkaFactDTO
// @Failure  400 {object} object "некорректные параметры"
// @Router   /api/v1/reports/perestanovka/fact [get]
func (h *perestanovkaHandler) fact(c *gin.Context) {
	by := c.DefaultQuery("by", "prib")
	if by != "prib" && by != "vigr" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный by (ожидается prib|vigr): " + by})
		return
	}
	res, err := h.svc.Fact(c.Request.Context(), c.Query("from"), c.Query("to"), by == "vigr", c.Query("terminal"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
