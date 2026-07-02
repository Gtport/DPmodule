package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// lkUploadHandler — шаг 1 загрузки ЛК: приём файла и сохранение в папку.
type lkUploadHandler struct {
	intake *service.LKIntake
}

func NewLKUploadHandler(intake *service.LKIntake) *lkUploadHandler {
	return &lkUploadHandler{intake: intake}
}

func (h *lkUploadHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.POST("/dislocation/lk/upload", h.upload)
	g.GET("/dislocation/lk/files", h.files)
}

// files godoc
// @Summary  Список загруженных файлов ЛК + контроль приёма (шаг между загрузкой и обработкой)
// @Tags     dislocation
// @Security BearerAuth
// @Success  200 {object} object
// @Router   /api/v1/dislocation/lk/files [get]
func (h *lkUploadHandler) files(c *gin.Context) {
	st, err := h.intake.Status()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, st)
}

// upload godoc
// @Summary  Загрузка файла дислокации из ЛК (шаг 1: сохранение в папку)
// @Tags     dislocation
// @Security BearerAuth
// @Accept   multipart/form-data
// @Param    file formData file true "xlsx-файл выгрузки ЛК"
// @Success  200 {object} object
// @Failure  400 {object} object
// @Failure  409 {object} object
// @Router   /api/v1/dislocation/lk/upload [post]
func (h *lkUploadHandler) upload(c *gin.Context) {
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

	res, err := h.intake.Store(fh.Filename, data)
	if err != nil {
		c.JSON(statusForIntakeError(err), gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"okpo":         res.Okpo,
		"organisation": res.Organisation,
		"terminals":    res.Terminals,
		"formation_ts": res.FormationTS,
		"filename":     res.Filename,
		"replaced":     res.Replaced,
	})
}

// statusForIntakeError маппит доменные ошибки приёма в HTTP-статусы.
func statusForIntakeError(err error) int {
	switch {
	case errors.Is(err, service.ErrOlderThanExisting):
		return http.StatusConflict
	case errors.Is(err, service.ErrNoLKSource):
		return http.StatusServiceUnavailable
	case errors.Is(err, service.ErrBadExt),
		errors.Is(err, service.ErrTooLarge),
		errors.Is(err, service.ErrNotLK),
		errors.Is(err, service.ErrInspect),
		errors.Is(err, service.ErrUnknownOkpo):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
