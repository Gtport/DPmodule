package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// markaUnmatchedHandler — «Без атрибуции» карточки «Информация»: несматченные с
// marka вагоны снимка группами по ключу матчинга + назначение атрибуции.
type markaUnmatchedHandler struct {
	svc *service.MarkaUnmatchedService
}

func NewMarkaUnmatchedHandler(svc *service.MarkaUnmatchedService) *markaUnmatchedHandler {
	return &markaUnmatchedHandler{svc: svc}
}

// RegisterRoutes — чтение списка: любой авторизованный (счётчик и модалка видны всем).
func (h *markaUnmatchedHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/dislocation/unmatched", h.groups)
}

// RegisterAssignRoutes — назначение атрибуции: монтируется в группу с гейтом
// AccessDicts (та же граница, что у правки словарей в Админе).
func (h *markaUnmatchedHandler) RegisterAssignRoutes(g *gin.RouterGroup) {
	g.POST("/dislocation/unmatched/assign", h.assign)
}

// groups godoc
// @Summary  Вагоны без бизнес-атрибуции (не сматчены с marka) группами по ключу матчинга
// @Tags     dislocation
// @Security BearerAuth
// @Produce  json
// @Success  200 {array} service.UnmatchedGroupDTO
// @Router   /api/v1/dislocation/unmatched [get]
func (h *markaUnmatchedHandler) groups(c *gin.Context) {
	c.JSON(http.StatusOK, h.svc.Groups())
}

// assign godoc
// @Summary  Назначить атрибуцию комбинации ключа (опционально — записать в словарь marka)
// @Tags     dislocation
// @Security BearerAuth
// @Accept   json
// @Produce  json
// @Param    request body service.MarkaAssignRequest true "ключ группы + поля атрибуции"
// @Success  200 {object} service.MarkaAssignResult
// @Failure  400 {object} object "некорректный запрос"
// @Failure  409 {object} object "конвейер/снимок не готов"
// @Router   /api/v1/dislocation/unmatched/assign [post]
func (h *markaUnmatchedHandler) assign(c *gin.Context) {
	var req service.MarkaAssignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректное тело запроса: " + err.Error()})
		return
	}
	res, err := h.svc.Assign(c.Request.Context(), req)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, service.ErrBadAssign):
			status = http.StatusBadRequest
		case errors.Is(err, service.ErrNotReady):
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
