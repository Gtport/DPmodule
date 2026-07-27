package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// delaysHandler — отчёт «Простои за период» (память о задержках vagon_delay),
// только чтение, все авторизованные роли.
type delaysHandler struct {
	svc *service.DelayService
}

func NewDelaysHandler(svc *service.DelayService) *delaysHandler {
	return &delaysHandler{svc: svc}
}

func (h *delaysHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/delays/current", h.current)
	g.GET("/dislocation/delays/report", h.report)
}

// current godoc
// @Summary  «Задержанные вагоны»: открытые эпизоды задержек (стоят прямо сейчас)
// @Tags     dislocation
// @Security BearerAuth
// @Produce  json
// @Success  200 {array} domain.VagonDelayRow
// @Router   /api/v1/dislocation/delays/current [get]
func (h *delaysHandler) current(c *gin.Context) {
	rows, err := h.svc.Current(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []domain.VagonDelayRow{}
	}
	c.JSON(http.StatusOK, rows)
}

// report godoc
// @Summary  Отчёт «Простои за период»: эпизоды задержек (статусы 4/5) + агрегат по станциям
// @Tags     dislocation
// @Security BearerAuth
// @Produce  json
// @Param    from     query string false "начало периода YYYY-MM-DD (дефолт: to − 6 дней)"
// @Param    to       query string false "конец периода YYYY-MM-DD, включительно (дефолт: сегодня МСК)"
// @Param    terminal query string false "терминал (gruzpol_s); пусто — все"
// @Success  200 {object} domain.DelayReport
// @Router   /api/v1/dislocation/delays/report [get]
func (h *delaysHandler) report(c *gin.Context) {
	to, ok := parseReportDate(c, c.Query("to"), time.Time(clock.Now()))
	if !ok {
		return
	}
	from, ok := parseReportDate(c, c.Query("from"), to.AddDate(0, 0, -6))
	if !ok {
		return
	}
	if to.Before(from) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "конец периода раньше начала"})
		return
	}
	rep, err := h.svc.Report(c.Request.Context(), domain.LocalTime(from), domain.LocalTime(to), c.Query("terminal"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rep)
}

// parseReportDate — дата фильтра YYYY-MM-DD; пусто → дефолт; мусор → 400 (ok=false).
func parseReportDate(c *gin.Context, raw string, def time.Time) (time.Time, bool) {
	if raw == "" {
		return def, true
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "дата должна быть в формате YYYY-MM-DD: " + raw})
		return time.Time{}, false
	}
	return t, true
}
