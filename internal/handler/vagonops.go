package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// vagonOpsHandler — история продвижения вагона (запрос 601): сохранённый трейл
// текущего рейса и ручной запрос к провайдеру (синхронный, мимо очереди).
type vagonOpsHandler struct {
	svc *service.VagonOpService
}

func NewVagonOpsHandler(svc *service.VagonOpService) *vagonOpsHandler {
	return &vagonOpsHandler{svc: svc}
}

func (h *vagonOpsHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/vagons/:vagon/operations", h.operations)    // сохранённый трейл
	g.POST("/dislocation/vagons/:vagon/operations/pull", h.pullNow) // ручной запрос 601

	// «История движения вагона» из интерфейса: рейс адресуется id строки
	// vagon_history (id вида «вагон/станция/дата» → только query-параметр).
	g.GET("/dislocation/vagons/trail", h.trail)
	g.POST("/dislocation/vagons/trail/pull", h.trailPull)
}

type trailOpDTO struct {
	DateOp string `json:"date_op"`
	KopVmd string `json:"kop_vmd"`
	Oper   string `json:"oper"`
	OperS  string `json:"oper_s"`
	Index  string `json:"index"`
}

type trailVisitDTO struct {
	StanOp  string       `json:"stan_op"`
	Station string       `json:"station"`
	Road    string       `json:"road"`
	First   trailOpDTO   `json:"first"`
	Last    trailOpDTO   `json:"last"`
	Count   int          `json:"count"`
	Ops     []trailOpDTO `json:"ops"`
}

type trailResponse struct {
	ID       string          `json:"id"`
	Vagon    string          `json:"vagon"`
	DateNach string          `json:"date_nach"`
	Terminal string          `json:"terminal"`
	From     string          `json:"from"`
	To       string          `json:"to"`
	Count    int             `json:"count"`
	Visits   []trailVisitDTO `json:"visits"`
}

// trail godoc
// @Summary  История движения вагона: сохранённый трейл рейса со справочниками
// @Tags     dislocation
// @Security BearerAuth
// @Produce  json
// @Param    id query string true "id рейса (строка vagon_history)"
// @Success  200 {object} trailResponse
// @Router   /api/v1/dislocation/vagons/trail [get]
func (h *vagonOpsHandler) trail(c *gin.Context) {
	v, err := h.svc.TrailByHistoryID(c.Request.Context(), c.Query("id"))
	h.respondTrail(c, v, err)
}

// trailPull godoc
// @Summary  Обновить историю движения вагона из АСУ (запрос 601, синхронно)
// @Tags     dislocation
// @Security BearerAuth
// @Produce  json
// @Param    id query string true "id рейса (строка vagon_history)"
// @Success  200 {object} trailResponse
// @Router   /api/v1/dislocation/vagons/trail/pull [post]
func (h *vagonOpsHandler) trailPull(c *gin.Context) {
	v, err := h.svc.PullTrailByHistoryID(c.Request.Context(), c.Query("id"))
	h.respondTrail(c, v, err)
}

func (h *vagonOpsHandler) respondTrail(c *gin.Context, v service.TrailView, err error) {
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTripNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrProviderClient):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		default:
			// Отказ провайдера (частый случай по выбывшим вагонам) — на экране
			// остаётся то, что уже сохранено в базе.
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		}
		return
	}
	resp := trailResponse{
		ID: v.ID, Vagon: v.Vagon, Terminal: v.Terminal, Count: v.Count,
		DateNach: timeOrEmpty(v.DateNach), From: timeOrEmpty(v.From), To: timeOrEmpty(v.To),
		Visits: make([]trailVisitDTO, len(v.Visits)),
	}
	for i, vis := range v.Visits {
		ops := make([]trailOpDTO, len(vis.Ops))
		for j, o := range vis.Ops {
			ops[j] = trailOpToDTO(o)
		}
		resp.Visits[i] = trailVisitDTO{
			StanOp: vis.StanOp, Station: vis.Station, Road: vis.Road,
			First: trailOpToDTO(vis.First), Last: trailOpToDTO(vis.Last),
			Count: vis.Count, Ops: ops,
		}
	}
	c.JSON(http.StatusOK, resp)
}

func trailOpToDTO(o service.TrailOp) trailOpDTO {
	return trailOpDTO{
		DateOp: o.DateOp.String(), KopVmd: o.KopVmd,
		Oper: o.Oper, OperS: o.OperS, Index: o.Index,
	}
}

func timeOrEmpty(t *domain.LocalTime) string {
	if t == nil {
		return ""
	}
	return t.String()
}

type vagonOperationDTO struct {
	DateOp     string `json:"date_op"`
	KopVmd     string `json:"kop_vmd"`
	StanOp     string `json:"stan_op"`
	IndexPoezd string `json:"index_poezd"`
}

type vagonOperationsResponse struct {
	Vagon      string              `json:"vagon"`
	Operations []vagonOperationDTO `json:"operations"`
}

// operations godoc
// @Summary  Сохранённый трейл продвижения текущего рейса вагона (запрос 601)
// @Tags     dislocation
// @Security BearerAuth
// @Produce  json
// @Param    vagon path string true "номер вагона"
// @Success  200 {object} vagonOperationsResponse
// @Router   /api/v1/dislocation/vagons/{vagon}/operations [get]
func (h *vagonOpsHandler) operations(c *gin.Context) {
	ops, err := h.svc.Operations(c.Request.Context(), c.Param("vagon"))
	h.respond(c, ops, err)
}

// pullNow godoc
// @Summary  Запросить историю продвижения у провайдера сейчас (оператор, синхронно)
// @Tags     dislocation
// @Security BearerAuth
// @Produce  json
// @Param    vagon path string true "номер вагона"
// @Success  200 {object} vagonOperationsResponse
// @Router   /api/v1/dislocation/vagons/{vagon}/operations/pull [post]
func (h *vagonOpsHandler) pullNow(c *gin.Context) {
	ops, err := h.svc.RequestNow(c.Request.Context(), c.Param("vagon"))
	h.respond(c, ops, err)
}

func (h *vagonOpsHandler) respond(c *gin.Context, ops []domain.VagonOperation, err error) {
	if err != nil {
		if errors.Is(err, service.ErrVagonNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	dto := make([]vagonOperationDTO, len(ops))
	for i, o := range ops {
		dto[i] = vagonOperationDTO{
			DateOp: o.DateOp.String(), KopVmd: o.KopVmd,
			StanOp: o.StanOp, IndexPoezd: o.IndexPoezd,
		}
	}
	c.JSON(http.StatusOK, vagonOperationsResponse{Vagon: c.Param("vagon"), Operations: dto})
}
