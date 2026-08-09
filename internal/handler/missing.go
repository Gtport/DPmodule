package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// missingHandler — пропавшие (записи-8 для станционных карточек «Кандидаты на
// прибытие») и доноры перегруза (статус 6, карточка «Информация»).
type missingHandler struct {
	svc *service.MissingService
}

func NewMissingHandler(svc *service.MissingService) *missingHandler {
	return &missingHandler{svc: svc}
}

func (h *missingHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/missing/groups", h.groups)
	g.GET("/dislocation/status6", h.donors)
}

// groups godoc
// @Summary  Пропавшие вагоны агрегированно: поезд → подгруппа → вагоны (карточка «Кандидаты на прибытие»)
// @Tags     dislocation
// @Security BearerAuth
// @Param    naznach query string false "терминалы через запятую (АЭ,ГУТ-2); пусто — все"
// @Success  200 {array} service.MissingGroupDTO
// @Router   /api/v1/dislocation/missing/groups [get]
func (h *missingHandler) groups(c *gin.Context) {
	rows, err := h.svc.Groups(c.Request.Context(), naznachParam(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rows)
}

// donors godoc
// @Summary  Доноры перегруза (статус 6): последняя позиция и груз
// @Tags     dislocation
// @Security BearerAuth
// @Produce  json
// @Success  200 {array} service.Status6VagonDTO
// @Router   /api/v1/dislocation/status6 [get]
func (h *missingHandler) donors(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Donors())
}
