package handler

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// overdueHandler — «Просрочка доставки» карточки «Работа»: вагоны снимка с
// истекшим нормативным сроком группами по накладной + Excel «для претензии»
// из истории рейсов. Только чтение (GET), все авторизованные роли.
type overdueHandler struct {
	svc *service.OverdueService
}

func NewOverdueHandler(svc *service.OverdueService) *overdueHandler {
	return &overdueHandler{svc: svc}
}

func (h *overdueHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/overdue", h.groups)
	g.GET("/reports/overdue/excel", h.claimExcel)
}

// groups godoc
// @Summary  «Просрочка доставки»: вагоны снимка с delay > 0 группами по накладной
// @Tags     dislocation
// @Security BearerAuth
// @Produce  json
// @Success  200 {array} service.OverdueGroupDTO
// @Router   /api/v1/dislocation/overdue [get]
func (h *overdueHandler) groups(c *gin.Context) {
	groups := h.svc.Groups()
	if groups == nil {
		groups = []service.OverdueGroupDTO{}
	}
	c.JSON(http.StatusOK, groups)
}

// claimExcel godoc
// @Summary  Отчёт для претензии: накладные/вагоны с фактом просрочки доставки, книга .xlsx
// @Tags     reports
// @Security BearerAuth
// @Param    from     query string false "начало периода прибытия YYYY-MM-DD (дефолт: to − 44 дня — срок претензии по пеням 45 дней, ст. 123 УЖТ)"
// @Param    to       query string false "конец периода YYYY-MM-DD, включительно (дефолт: сегодня МСК)"
// @Param    terminal query string false "терминал (gruzpol_s); пусто — все"
// @Success  200 {file} binary "книга .xlsx"
// @Failure  400 {object} object "некорректный период либо пустой/слишком большой набор"
// @Router   /api/v1/reports/overdue/excel [get]
func (h *overdueHandler) claimExcel(c *gin.Context) {
	to, ok := parseReportDate(c, c.Query("to"), time.Time(clock.Now()))
	if !ok {
		return
	}
	from, ok := parseReportDate(c, c.Query("from"), to.AddDate(0, 0, -44))
	if !ok {
		return
	}
	if to.Before(from) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "конец периода раньше начала"})
		return
	}
	data, filename, err := h.svc.ClaimExcel(c.Request.Context(),
		domain.LocalTime(from), domain.LocalTime(to), c.Query("terminal"))
	if err != nil {
		if errors.Is(err, service.ErrHistorySearchInvalid) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filename))
	c.Data(http.StatusOK, xlsxContentType, data)
}
