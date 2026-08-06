package handler

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// historySearchHandler — экран «Работа с историческими данными» (Инструменты
// оператора, перенос gtport OperatorToolsHistory): поиск по vagon_history с
// серверными пагинацией и сортировкой, данные панели фильтров, Excel по всему
// фильтру. Поиск и экспорт — POST: списки вагонов вставляются из Excel сотнями
// номеров, query string упёрся бы в лимиты URL (так же работал и gtport).
// Побочно POST попадает под RequireForWrites (operator+) — приемлемо,
// страница и так закрыта ролью OPER.
type historySearchHandler struct {
	svc *service.HistorySearchService
}

func NewHistorySearchHandler(svc *service.HistorySearchService) *historySearchHandler {
	return &historySearchHandler{svc: svc}
}

func (h *historySearchHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.POST("/history/search", h.search)
	g.GET("/history/meta", h.meta)
	g.POST("/history/excel", h.excel)
}

// search godoc
// @Summary  История вагонов: страница по фильтру (серверные пагинация и сортировка)
// @Tags     history
// @Security BearerAuth
// @Param    body body service.HistorySearchRequest true "фильтр + сортировка + страница"
// @Success  200 {object} service.HistorySearchDTO
// @Failure  400 {object} object "ошибка валидации фильтра"
// @Router   /api/v1/history/search [post]
func (h *historySearchHandler) search(c *gin.Context) {
	var req service.HistorySearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректное тело запроса: " + err.Error()})
		return
	}
	dto, err := h.svc.Search(c.Request.Context(), req)
	if err != nil {
		historySearchError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto)
}

// meta godoc
// @Summary  История вагонов: данные панели фильтров (терминалы с цветами, станции погрузки)
// @Tags     history
// @Security BearerAuth
// @Success  200 {object} service.HistoryMetaDTO
// @Router   /api/v1/history/meta [get]
func (h *historySearchHandler) meta(c *gin.Context) {
	dto, err := h.svc.Meta(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, dto)
}

// excel godoc
// @Summary  История вагонов: книга .xlsx по ВСЕМУ отфильтрованному набору
// @Tags     history
// @Security BearerAuth
// @Param    body body service.HistorySearchRequest true "фильтр + сортировка (page игнорируется)"
// @Success  200 {file} binary "книга .xlsx"
// @Failure  400 {object} object "ошибка фильтра либо превышен потолок строк"
// @Router   /api/v1/history/excel [post]
func (h *historySearchHandler) excel(c *gin.Context) {
	var req service.HistorySearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректное тело запроса: " + err.Error()})
		return
	}
	data, filename, err := h.svc.Excel(c.Request.Context(), req)
	if err != nil {
		historySearchError(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	c.Data(http.StatusOK, xlsxContentType, data)
}

// historySearchError — 400 на ошибки валидации (текст понятен диспетчеру),
// 500 на всё остальное.
func historySearchError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrHistorySearchInvalid) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}
