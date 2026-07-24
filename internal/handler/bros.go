package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// brosHandler — экран «Брошенные поезда». В этой ветке только справочник кодов
// бросания (read, всем авторизованным). Активные броски, журнал и отчёт —
// следующие ветки.
type brosHandler struct {
	codes *service.BrosReasonCodes
}

func NewBrosHandler(codes *service.BrosReasonCodes) *brosHandler {
	return &brosHandler{codes: codes}
}

func (h *brosHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/bros/reason-codes", h.reasonCodes)
}

// reasonCodes godoc
// @Summary  Справочник кодов бросания (классификатор РЖД)
// @Tags     bros
// @Security BearerAuth
// @Success  200 {object} object
// @Router   /api/v1/dislocation/bros/reason-codes [get]
func (h *brosHandler) reasonCodes(c *gin.Context) {
	codes, err := h.codes.Codes(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"codes": codes, "count": len(codes)})
}

// brosOpsHandler — снимок брошенных: активные, история, отчёт (фильтр), поиск.
// Чтение всем авторизованным. Правки/журнал — следующая ветка. Требует БД
// (репозиторий bros) — монтируется только когда снимок доступен.
type brosOpsHandler struct {
	svc *service.BrosService
}

func NewBrosOpsHandler(svc *service.BrosService) *brosOpsHandler {
	return &brosOpsHandler{svc: svc}
}

func (h *brosOpsHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/bros/active", h.active)
	g.GET("/dislocation/bros/history", h.history)
	g.GET("/dislocation/bros/filter", h.filter)
	g.GET("/dislocation/bros/search", h.search)
}

// active godoc
// @Summary  Активные брошенные поезда
// @Tags     bros
// @Security BearerAuth
// @Success  200 {object} object
// @Router   /api/v1/dislocation/bros/active [get]
func (h *brosOpsHandler) active(c *gin.Context) {
	records, err := h.svc.Active(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"records": records, "count": len(records)})
}

// history godoc
// @Summary  История брошенных (завершённые)
// @Tags     bros
// @Security BearerAuth
// @Success  200 {object} object
// @Router   /api/v1/dislocation/bros/history [get]
func (h *brosOpsHandler) history(c *gin.Context) {
	limit := atoiDefault(c.Query("limit"), 150)
	offset := atoiDefault(c.Query("offset"), 0)
	records, total, err := h.svc.History(c.Request.Context(), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"records": records, "count": len(records), "total": total,
		"limit": limit, "offset": offset, "has_more": offset+limit < total,
	})
}

// filter godoc
// @Summary  Броски по фильтру (терминал/период/статус) — отчёт
// @Tags     bros
// @Security BearerAuth
// @Success  200 {object} object
// @Failure  400 {object} object
// @Router   /api/v1/dislocation/bros/filter [get]
func (h *brosOpsHandler) filter(c *gin.Context) {
	var f domain.BrosFilter
	if g := strings.TrimSpace(c.Query("gruzpol_s")); g != "" {
		for _, part := range strings.Split(g, ",") {
			if p := strings.TrimSpace(part); p != "" {
				f.GruzpolS = append(f.GruzpolS, p)
			}
		}
	}
	if s := c.Query("start_date"); s != "" {
		d, err := parseBrosDate(s)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "неверный формат start_date, ожидается YYYY-MM-DD"})
			return
		}
		f.StartDate = d
	}
	if s := c.Query("end_date"); s != "" {
		d, err := parseBrosDate(s)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "неверный формат end_date, ожидается YYYY-MM-DD"})
			return
		}
		f.EndDate = d
	}
	if s := c.Query("status"); s != "" {
		b := s == "true"
		f.Status = &b
	}
	f.Limit = atoiDefault(c.Query("limit"), 100)
	f.Offset = atoiDefault(c.Query("offset"), 0)

	records, total, err := h.svc.Filter(c.Request.Context(), f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"records": records, "count": len(records), "total": total,
		"limit": f.Limit, "offset": f.Offset, "has_more": f.Offset+len(records) < total,
	})
}

// search godoc
// @Summary  Поиск брошенных по индексу (подстрока)
// @Tags     bros
// @Security BearerAuth
// @Success  200 {object} object
// @Failure  400 {object} object
// @Router   /api/v1/dislocation/bros/search [get]
func (h *brosOpsHandler) search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if len(q) < 3 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "строка поиска должна содержать минимум 3 символа"})
		return
	}
	records, err := h.svc.Search(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"records": records, "count": len(records)})
}

// atoiDefault — целое из строки или значение по умолчанию (при пустом/ошибке).
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return n
	}
	return def
}

// parseBrosDate — дата отчёта YYYY-MM-DD → LocalTime (Московское naive, без TZ).
func parseBrosDate(s string) (*domain.LocalTime, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	lt := domain.LocalTime(t)
	return &lt, nil
}
