package handler

import (
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// indexFormat — индекс поезда 4-3-4 (13 символов): «7438-011-1234». Форму 4-3-4 задаёт
// и фронт (сегментированный ввод), это серверная страховка от кривого клиента.
var indexFormat = regexp.MustCompile(`^\d{4}-\d{3}-\d{4}$`)

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
	g.POST("/dislocation/plan/prepare", h.prepare)          // фаза A: разбор + кандидаты с.ф. + проблемные нитки (снимок не трогаем)
	g.POST("/dislocation/plan/revalidate", h.revalidate)    // сухой пересчёт с правками индексов (снимок не трогаем)
	g.POST("/dislocation/plan/confirm", h.confirm)          // фаза B: применить с правками индексов и выбором групп с.ф.
	g.POST("/dislocation/plan/touch", h.touch)              // heartbeat: продлить токен, пока открыт диалог с.ф.
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
		if errors.Is(err, service.ErrDislStale) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()}) // 409 — дислокация устарела
			return
		}
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
		if errors.Is(err, service.ErrDislStale) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()}) // 409 — дислокация устарела
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

// revalidateRequest — тело revalidate: токен + правки индексов (ord→индекс 4-3-4).
type revalidateRequest struct {
	Token     string            `json:"token"`
	Overrides map[string]string `json:"overrides"` // ключ — ord нитки (строкой), значение — индекс
}

// revalidate — сухой пересчёт превью с ручными правками индексов; снимок НЕ изменяется,
// токен НЕ расходуется. Оператор правит индексы и видит обновлённые с.ф.-строки и
// проблемные нитки, пока не подтвердит через confirm.
func (h *planUploadHandler) revalidate(c *gin.Context) {
	var req revalidateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректное тело запроса"})
		return
	}
	if req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "не передан токен подготовки"})
		return
	}
	overrides, err := parseOverrides(req.Overrides)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	res, err := h.proc.Revalidate(c.Request.Context(), req.Token, overrides)
	if err != nil {
		if errors.Is(err, service.ErrPendingNotFound) {
			c.JSON(http.StatusGone, gin.H{"error": err.Error()}) // 410 — фронт перезагрузит план
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

// confirmRequest — тело confirm: токен + правки индексов + выбор групп по ord с.ф.-нитки.
type confirmRequest struct {
	Token      string              `json:"token"`
	Overrides  map[string]string   `json:"overrides"`  // ключ — ord нитки (строкой), значение — индекс 4-3-4
	Selections map[string][]string `json:"selections"` // ключ — ord нитки (строкой), значение — id_disl
}

// confirm — фаза B: применить план с ручными правками индексов и выбранными группами с.ф.
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
	overrides, err := parseOverrides(req.Overrides)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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

	res, err := h.proc.Confirm(c.Request.Context(), req.Token, overrides, selections)
	if err != nil {
		if errors.Is(err, service.ErrPendingNotFound) {
			c.JSON(http.StatusGone, gin.H{"error": err.Error()}) // 410 — фронт перезагрузит план
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, res)
}

// parseOverrides переводит тело {ord(строкой): индекс} в map[int]string с валидацией
// ключа (ord) и формата индекса (4-3-4). Пустой индекс — пропуск (снятие правки).
func parseOverrides(m map[string]string) (map[int]string, error) {
	out := make(map[int]string, len(m))
	for k, v := range m {
		ord, err := strconv.Atoi(k)
		if err != nil {
			return nil, errors.New("некорректный ключ правки (ожидался ord нитки): " + k)
		}
		if v == "" {
			continue
		}
		if !indexFormat.MatchString(v) {
			return nil, errors.New("индекс должен быть в формате 4-3-4 (например 7438-011-1234): " + v)
		}
		out[ord] = v
	}
	return out, nil
}

// touchRequest — тело heartbeat: токен подготовки.
type touchRequest struct {
	Token string `json:"token"`
}

// touch продлевает TTL токена, пока открыт диалог выбора с.ф. 200 — продлён,
// 410 Gone — токен уже истёк/неизвестен (фронт закроет окно и попросит перезагрузить).
func (h *planUploadHandler) touch(c *gin.Context) {
	var req touchRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "не передан токен"})
		return
	}
	if !h.proc.Touch(req.Token) {
		c.JSON(http.StatusGone, gin.H{"error": "токен истёк"})
		return
	}
	c.Status(http.StatusNoContent)
}
