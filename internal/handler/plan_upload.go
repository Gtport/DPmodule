package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// planUploadHandler — приём файла плана подвода: разбор + сопоставление вагонов с
// нитками + простановка планового прибытия в снимок (один шаг).
type planUploadHandler struct {
	proc *service.PlanProcessor
}

func NewPlanUploadHandler(proc *service.PlanProcessor) *planUploadHandler {
	return &planUploadHandler{proc: proc}
}

func (h *planUploadHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.POST("/dislocation/plan/upload", h.upload)
	g.GET("/dislocation/plan/:code", h.get)
}

// get godoc
// @Summary  Сетка загруженного плана подвода (заголовок + нитки) для фронта
// @Tags     dislocation
// @Security BearerAuth
// @Param    code path string true "код станции плана: ma|nk"
// @Success  200 {object} object
// @Failure  404 {object} object
// @Router   /api/v1/dislocation/plan/{code} [get]
func (h *planUploadHandler) get(c *gin.Context) {
	code := c.Param("code")
	header, nitki, err := h.proc.GetPlan(c.Request.Context(), code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if header.PlanCode == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "план не загружен для " + code})
		return
	}
	c.JSON(http.StatusOK, gin.H{"plan": header, "nitki": nitki})
}

// upload godoc
// @Summary  Загрузка плана подвода (разбор + матч + простановка PlanMsk в снимок)
// @Tags     dislocation
// @Security BearerAuth
// @Accept   multipart/form-data
// @Param    file formData file   true "xlsx-файл плана подвода"
// @Param    code formData string true "код станции плана: ma|nk"
// @Success  200 {object} object
// @Failure  400 {object} object
// @Router   /api/v1/dislocation/plan/upload [post]
func (h *planUploadHandler) upload(c *gin.Context) {
	code := c.PostForm("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "не передан код станции (поле 'code': ma|nk)"})
		return
	}

	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "файл не передан (поле 'file')"})
		return
	}
	if fh.Size == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "пустой файл"})
		return
	}

	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось открыть файл"})
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "не удалось прочитать файл"})
		return
	}

	res, err := h.proc.ProcessFile(c.Request.Context(), code, fh.Filename, data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}
