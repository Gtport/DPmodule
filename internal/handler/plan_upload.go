package handler

import (
	"io"
	"net/http"
	"strconv"

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
	g.POST("/dislocation/plan/upload", h.upload)            // одношаговая загрузка (без выбора с.ф.)
	g.POST("/dislocation/plan/prepare", h.prepare)          // фаза A: разбор + кандидаты с.ф. (снимок не трогаем)
	g.POST("/dislocation/plan/confirm", h.confirm)          // фаза B: применить с выбранными группами с.ф.
	g.GET("/dislocation/plan/:code", h.get)                 // ?id=N — конкретная загрузка, иначе свежая
	g.GET("/dislocation/plan/:code/history", h.history)     // список загрузок станции
}

// get godoc
// @Summary  Сетка плана подвода (заголовок + нитки): свежая или по ?id
// @Tags     dislocation
// @Security BearerAuth
// @Param    code path  string true  "код станции плана: ma|nk"
// @Param    id   query int    false "id конкретной загрузки (иначе — самая свежая)"
// @Success  200 {object} object
// @Failure  404 {object} object
// @Router   /api/v1/dislocation/plan/{code} [get]
func (h *planUploadHandler) get(c *gin.Context) {
	code := c.Param("code")
	ctx := c.Request.Context()

	// ?id=N — конкретная загрузка из истории (id глобально уникален, code для маршрута).
	if idStr := c.Query("id"); idStr != "" {
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный id загрузки"})
			return
		}
		header, nitki, err := h.proc.GetPlanByID(ctx, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if header.PlanCode == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "загрузка плана не найдена"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"plan": header, "nitki": nitki})
		return
	}

	header, nitki, err := h.proc.GetLatestPlan(ctx, code)
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

// history godoc
// @Summary  Список загрузок плана станции (свежие первыми) для выбора
// @Tags     dislocation
// @Security BearerAuth
// @Param    code path string true "код станции плана: ma|nk"
// @Success  200 {object} object
// @Router   /api/v1/dislocation/plan/{code}/history [get]
func (h *planUploadHandler) history(c *gin.Context) {
	list, err := h.proc.ListPlans(c.Request.Context(), c.Param("code"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"plans": list})
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

// prepare — фаза A: разбор плана + кандидаты для с.ф.; снимок НЕ изменяется.
// Возвращает токен (для confirm) и с.ф.-строки с группами-кандидатами.
func (h *planUploadHandler) prepare(c *gin.Context) {
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

	res, err := h.proc.Prepare(c.Request.Context(), code, fh.Filename, data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

// confirmRequest — тело confirm: токен + выбор групп по ord с.ф.-нитки.
type confirmRequest struct {
	Token      string              `json:"token"`
	Selections map[string][]string `json:"selections"` // ключ — ord нитки (строкой), значение — id_disl
}

// confirm — фаза B: применить план с выбранными пользователем группами с.ф.
func (h *planUploadHandler) confirm(c *gin.Context) {
	var req confirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректное тело запроса"})
		return
	}
	if req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "не передан токен подготовки"})
		return
	}
	selections := make(map[int][]string, len(req.Selections))
	for k, v := range req.Selections {
		ord, err := strconv.Atoi(k)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "некорректный ключ выбора (ожидался ord нитки): " + k})
			return
		}
		selections[ord] = v
	}

	res, err := h.proc.Confirm(c.Request.Context(), req.Token, selections)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}
