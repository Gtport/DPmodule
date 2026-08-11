package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// destStandHandler — «Долгостой» карточки «Работа»: вагоны, стоящие на станции
// назначения дольше порога (дефолт 48 ч с прибытия), статусы ≥ 10.
// Только чтение (GET), все авторизованные роли.
type destStandHandler struct {
	svc *service.DestStandService
}

func NewDestStandHandler(svc *service.DestStandService) *destStandHandler {
	return &destStandHandler{svc: svc}
}

func (h *destStandHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/long-stand", h.list)
}

// destStandResponse — список плюс действующий порог: интерфейс подписывает им
// заголовок («дольше 48 ч»), чтобы подпись не разъезжалась с настройкой.
type destStandResponse struct {
	ThresholdHours int                         `json:"threshold_hours"`
	Vagons         []service.DestStandVagonDTO `json:"vagons"`
}

// list godoc
// @Summary  «Долгостой»: вагоны на станции назначения дольше порога (статусы ≥ 10)
// @Tags     dislocation
// @Security BearerAuth
// @Produce  json
// @Success  200 {object} destStandResponse
// @Router   /api/v1/dislocation/long-stand [get]
func (h *destStandHandler) list(c *gin.Context) {
	rows := h.svc.List()
	if rows == nil {
		rows = []service.DestStandVagonDTO{}
	}
	c.JSON(http.StatusOK, destStandResponse{
		ThresholdHours: h.svc.ThresholdHours(),
		Vagons:         rows,
	})
}
