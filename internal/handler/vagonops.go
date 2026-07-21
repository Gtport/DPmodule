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
