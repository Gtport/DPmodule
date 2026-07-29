package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/clock"
	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// cargoWorkHandler — «Грузовая работа»: суточный учётный лист терминала.
//
// Терминал в ПУТИ — имя из реестра ports (АЭ/УТ-1/ГУТ-2), а не код порта:
// в gtport путь был /cargowork/vigr/{at|ut|gut} с валидацией по трём зашитым
// константам, здесь состав терминалов задаётся справочником.
type cargoWorkHandler struct {
	svc *service.CargoWorkService
}

func NewCargoWorkHandler(svc *service.CargoWorkService) *cargoWorkHandler {
	return &cargoWorkHandler{svc: svc}
}

func (h *cargoWorkHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/cargo-work/:date/:terminal", h.day)
	g.PUT("/cargo-work/:date/:terminal", h.save)
	g.POST("/cargo-work/:date/:terminal/recalc", h.recalc)
	g.DELETE("/cargo-work/:date/:terminal", h.remove)
	// Отчёт «Выгрузка за период» страницы «Справки и отчёты» — те же данные
	// «Грузовой работы», поэтому живёт в этом же обработчике, но в
	// пространстве /reports (как остальные отчёты страницы).
	g.GET("/reports/vygruzka", h.period)
}

// parseDay разбирает дату учётных суток из пути (yyyy-MM-dd).
func parseCargoWorkDay(c *gin.Context) (time.Time, bool) {
	day, err := time.Parse("2006-01-02", c.Param("date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "дата должна быть в формате ГГГГ-ММ-ДД"})
		return time.Time{}, false
	}
	return day, true
}

// writeCargoWorkErr — 403 на запрет по датам, иначе 500. Неизвестный терминал —
// ошибка запроса, а не сервера.
func writeCargoWorkErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrCargoWorkAccess):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

// period godoc
// @Summary  Отчёт «Выгрузка за период»: сохранённые учётные листы терминала по суткам
// @Tags     reports
// @Security BearerAuth
// @Produce  json
// @Param    terminal query string true  "Терминал (ports.name_s)"
// @Param    from     query string false "начало периода YYYY-MM-DD (дефолт: 1-е число месяца даты to)"
// @Param    to       query string false "конец периода YYYY-MM-DD, включительно (дефолт: вчера МСК)"
// @Success  200 {object} service.CargoWorkPeriodDTO
// @Router   /api/v1/reports/vygruzka [get]
func (h *cargoWorkHandler) period(c *gin.Context) {
	terminal := c.Query("terminal")
	if terminal == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "требуется параметр terminal"})
		return
	}
	to, ok := parseReportDate(c, c.Query("to"), time.Time(clock.Now()).AddDate(0, 0, -1))
	if !ok {
		return
	}
	from, ok := parseReportDate(c, c.Query("from"), time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, to.Location()))
	if !ok {
		return
	}
	if to.Before(from) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "конец периода раньше начала"})
		return
	}
	dto, err := h.svc.Period(c.Request.Context(), from, to, terminal)
	switch {
	case errors.Is(err, domain.ErrBadRequest):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusOK, dto)
	}
}

// day godoc
// @Summary  Учётный лист «Грузовой работы» терминала за сутки
// @Tags     cargo-work
// @Security BearerAuth
// @Param    date     path string true "ЖД-сутки, ГГГГ-ММ-ДД"
// @Param    terminal path string true "Терминал (ports.name_s)"
// @Success  200 {object} service.CargoWorkDayDTO
// @Router   /api/v1/cargo-work/{date}/{terminal} [get]
func (h *cargoWorkHandler) day(c *gin.Context) {
	day, ok := parseCargoWorkDay(c)
	if !ok {
		return
	}
	res, err := h.svc.Day(c.Request.Context(), day, c.Param("terminal"))
	if err != nil {
		writeCargoWorkErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// save godoc
// @Summary  Правка учётного листа (только ручные поля: план, факт выгрузки, комментарий, погрузка)
// @Tags     cargo-work
// @Security BearerAuth
// @Param    date     path string true "ЖД-сутки, ГГГГ-ММ-ДД"
// @Param    terminal path string true "Терминал (ports.name_s)"
// @Param    body     body service.CargoWorkManual true "Ручные поля по линиям"
// @Success  200 {object} service.CargoWorkDayDTO
// @Failure  403 {object} map[string]string "оператору доступны только вчерашние сутки"
// @Router   /api/v1/cargo-work/{date}/{terminal} [put]
func (h *cargoWorkHandler) save(c *gin.Context) {
	day, ok := parseCargoWorkDay(c)
	if !ok {
		return
	}
	var manual service.CargoWorkManual
	if err := c.ShouldBindJSON(&manual); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "некорректное тело запроса"})
		return
	}
	res, err := h.svc.Save(c.Request.Context(), day, c.Param("terminal"), manual)
	if err != nil {
		writeCargoWorkErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// recalc godoc
// @Summary  Пересчитать авто-слой суток (прибытие, выгрузка по станции, аналитика); ручные поля сохраняются
// @Tags     cargo-work
// @Security BearerAuth
// @Param    date     path string true "ЖД-сутки, ГГГГ-ММ-ДД"
// @Param    terminal path string true "Терминал (ports.name_s)"
// @Success  200 {object} service.CargoWorkDayDTO
// @Failure  403 {object} map[string]string "оператору доступны только вчерашние сутки"
// @Router   /api/v1/cargo-work/{date}/{terminal}/recalc [post]
func (h *cargoWorkHandler) recalc(c *gin.Context) {
	day, ok := parseCargoWorkDay(c)
	if !ok {
		return
	}
	res, err := h.svc.Recalc(c.Request.Context(), day, c.Param("terminal"))
	if err != nil {
		writeCargoWorkErr(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// remove godoc
// @Summary  Удалить учёт суток по терминалу (выгрузка и погрузка)
// @Tags     cargo-work
// @Security BearerAuth
// @Param    date     path string true "ЖД-сутки, ГГГГ-ММ-ДД"
// @Param    terminal path string true "Терминал (ports.name_s)"
// @Success  200 {object} map[string]string
// @Failure  403 {object} map[string]string "оператору доступны только вчерашние сутки"
// @Router   /api/v1/cargo-work/{date}/{terminal} [delete]
func (h *cargoWorkHandler) remove(c *gin.Context) {
	day, ok := parseCargoWorkDay(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), day, c.Param("terminal")); err != nil {
		writeCargoWorkErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
