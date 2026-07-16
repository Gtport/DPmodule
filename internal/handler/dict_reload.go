package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// dictReloadHandler — механизм «Обновить справочники»: горячая перезагрузка
// словарей в RAM + гибридный пересчёт снимка + Stage 3–4 (перенос эталона gtport).
type dictReloadHandler struct {
	proc *service.LKProcessor
}

func NewDictReloadHandler(proc *service.LKProcessor) *dictReloadHandler {
	return &dictReloadHandler{proc: proc}
}

func (h *dictReloadHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.POST("/dislocation/directories/reload", h.reload)
}

// reload godoc
// @Summary  Обновить справочники: перезагрузка словарей + пересчёт снимка (Stage 3–4)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} service.DictReloadResult
// @Failure  409 {object} object "снимок дислокации не загружен"
// @Router   /api/v1/dislocation/directories/reload [post]
func (h *dictReloadHandler) reload(c *gin.Context) {
	res, err := h.proc.ReloadDirectories(c.Request.Context())
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrNotReady) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
