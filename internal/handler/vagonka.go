package handler

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/report"
	"github.com/Gtport/DPmodule/internal/service"
)

// vagonkaHandler — выгрузка «Повагонка» страницы «Справки и отчёты» (перенос
// gtport export_handler): текущий снимок дислокации книгой .xlsx. Книга
// собирается на сервере — полный снимок до фронта не доезжает.
type vagonkaHandler struct {
	actual *service.ActualCache
	dir    *service.DirectoryCache
}

func NewVagonkaHandler(actual *service.ActualCache, dir *service.DirectoryCache) *vagonkaHandler {
	return &vagonkaHandler{actual: actual, dir: dir}
}

func (h *vagonkaHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/reports/vagonka/excel", h.excel)
}

// excel godoc
// @Summary  «Повагонка»: текущий снимок дислокации книгой .xlsx (полная либо по терминалу)
// @Tags     reports
// @Security BearerAuth
// @Param    terminal query string false "терминал (ports.name_s); пусто — весь снимок"
// @Success  200 {file} binary "книга .xlsx"
// @Failure  400 {object} object "неизвестный терминал"
// @Router   /api/v1/reports/vagonka/excel [get]
func (h *vagonkaHandler) excel(c *gin.Context) {
	terminal := c.Query("terminal")
	if terminal != "" {
		if _, ok := h.dir.PortByNameS(terminal); !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "неизвестный терминал: " + terminal})
			return
		}
	}
	data, filename, err := report.VagonkaXLSX(h.actual.All(), terminal)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Имя кириллическое — кодируем по RFC 5987 (скачивающий фронт всё равно
	// именует файл сам, заголовок — для прямых переходов по ссылке).
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	c.Data(http.StatusOK, xlsxContentType, data)
}
