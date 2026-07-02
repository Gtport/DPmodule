package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// lkProcessHandler — шаг 2 загрузки ЛК: обработка принятых файлов в снимок.
type lkProcessHandler struct {
	proc *service.LKProcessor
}

func NewLKProcessHandler(proc *service.LKProcessor) *lkProcessHandler {
	return &lkProcessHandler{proc: proc}
}

func (h *lkProcessHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.POST("/dislocation/lk/process", h.process)
}

// process godoc
// @Summary  Обработка загруженных файлов ЛК в снимок дислокации (шаг 2)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} object
// @Failure  409 {object} object
// @Router   /api/v1/dislocation/lk/process [post]
func (h *lkProcessHandler) process(c *gin.Context) {
	res, err := h.proc.Process(c.Request.Context())
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrNotReady) || errors.Is(err, service.ErrDataLoss) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
