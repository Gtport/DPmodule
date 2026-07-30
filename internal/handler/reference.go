package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/service"
)

// referenceHandler — памятки ГУ-45 на подачу/уборку из внешнего сервиса.
// Инкремент раскладывает вехи подачи/уборки по рейсам vagon_history (крон +
// ручной триггер здесь); забор по номеру отдаёт сырой документ провайдера
// как есть — это другой контракт, в историю он не раскладывается.
type referenceHandler struct {
	svc *service.ReferenceService
}

func NewReferenceHandler(svc *service.ReferenceService) *referenceHandler {
	return &referenceHandler{svc: svc}
}

func (h *referenceHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/reference", h.byNumber)                // ?number=...&date_create=...
	g.GET("/reference/excel", h.excelByNumber)     // ?number=...&date_create=... — бланк ГУ-45 в Excel
	g.POST("/reference/update/pull", h.updatePull) // ручной триггер крон-инкремента
}

// byNumber godoc
// @Summary  Памятка по номеру и дате создания — сырой документ провайдера (диагностика)
// @Tags     reference
// @Security BearerAuth
// @Param    number      query string true  "номер памятки (NUMBER_PAMYATKA)"
// @Param    date_create query string true  "DATE_CREATE памятки из инкремента, дословно (MM-DD-YYYY); у документа без даты — пустое значение"
// @Param    client      query string false "код клиента у провайдера; по умолчанию первый из reference.clients"
// @Success  200 {object} object
// @Failure  400 {object} object "не задан number или date_create"
// @Failure  502 {object} object "провайдер недоступен / ошибка забора"
// @Router   /api/v1/reference [get]
func (h *referenceHandler) byNumber(c *gin.Context) {
	number, dateCreate, ok := pamyatkaKey(c)
	if !ok {
		return
	}
	body, err := h.svc.FetchByNumber(c.Request.Context(), c.Query("client"), number, dateCreate)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

// pamyatkaKey — ключ памятки из запроса: номер и дата создания. Номер один
// памятку не определяет (повторяется у портов и переиспользуется внутри порта),
// поэтому провайдер требует и date_create; сам ответ на 400 пишем здесь.
//
// ПУСТОЕ значение date_create законно (у документа без даты создания DATE_CREATE
// приходит пустой строкой), а вот отсутствие параметра — нет: иначе забыть его
// в вызове означало бы молча получить чужую памятку.
func pamyatkaKey(c *gin.Context) (number, dateCreate string, ok bool) {
	number = c.Query("number")
	if number == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "требуется параметр number"})
		return "", "", false
	}
	dateCreate, exists := c.GetQuery("date_create")
	if !exists {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "требуется параметр date_create — значение DATE_CREATE этой памятки из инкремента (MM-DD-YYYY); у документа без даты создания передавать пустым",
		})
		return "", "", false
	}
	return number, dateCreate, true
}

// excelByNumber godoc
// @Summary  Памятка по номеру бланком ГУ-45 в Excel
// @Tags     reference
// @Security BearerAuth
// @Produce  application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Param    number      query string true  "номер памятки (NUMBER_PAMYATKA)"
// @Param    date_create query string true  "DATE_CREATE памятки из инкремента, дословно (MM-DD-YYYY); у документа без даты — пустое значение"
// @Param    client      query string false "код клиента у провайдера; по умолчанию первый из reference.clients"
// @Success  200 {file} binary "книга .xlsx с бланком памятки"
// @Failure  400 {object} object "не задан number или date_create"
// @Failure  502 {object} object "провайдер недоступен / документа нет в ответе / ошибка сборки"
// @Router   /api/v1/reference/excel [get]
func (h *referenceHandler) excelByNumber(c *gin.Context) {
	number, dateCreate, ok := pamyatkaKey(c)
	if !ok {
		return
	}
	data, filename, err := h.svc.FetchExcelByNumber(c.Request.Context(), c.Query("client"), number, dateCreate)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Data(http.StatusOK, xlsxContentType, data)
}

// xlsxContentType — MIME книги Excel (OOXML).
const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// updatePull godoc
// @Summary  Инкремент памяток по всем клиентам: разнос вех подачи/уборки по рейсам
// @Tags     reference
// @Security BearerAuth
// @Success  200 {object} object "по клиенту: разобрано памяток/вагонов, обновлено рейсов, пропущено с причинами"
// @Failure  502 {object} object "провайдер недоступен / ошибка забора или записи"
// @Router   /api/v1/reference/update/pull [post]
func (h *referenceHandler) updatePull(c *gin.Context) {
	res, err := h.svc.PullUpdatesDetailed(c.Request.Context())
	if err != nil {
		// Часть клиентов могла отработать — отдаём и результаты, и ошибку.
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "results": res})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "results": res})
}
