package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/domain"
	"github.com/Gtport/DPmodule/internal/service"
)

// adminTablesHandler — универсальный редактор справочников для страницы «Админ»
// (перенос эталона gtport /admin/tables): CRUD по таблицам реестра list_tables.
// Роль administrator требует группа маршрутов (см. server.go).
type adminTablesHandler struct {
	svc *service.AdminTables
}

func NewAdminTablesHandler(svc *service.AdminTables) *adminTablesHandler {
	return &adminTablesHandler{svc: svc}
}

func (h *adminTablesHandler) RegisterRoutes(g *gin.RouterGroup) {
	g.GET("/admin/tables", h.list)
	g.GET("/admin/tables/:table", h.data)
	g.POST("/admin/tables/:table", h.create)
	g.PUT("/admin/tables/:table/:id", h.update)
	g.DELETE("/admin/tables/:table/:id", h.remove)
}

// list godoc
// @Summary  Реестр редактируемых справочников (list_tables)
// @Tags     admin
// @Security BearerAuth
// @Success  200 {array} domain.AdminTable
// @Router   /api/v1/admin/tables [get]
func (h *adminTablesHandler) list(c *gin.Context) {
	tables, err := h.svc.Tables(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tables)
}

// data godoc
// @Summary  Колонки и строки справочника
// @Tags     admin
// @Security BearerAuth
// @Success  200 {object} object
// @Router   /api/v1/admin/tables/{table} [get]
func (h *adminTablesHandler) data(c *gin.Context) {
	t, cols, rows, err := h.svc.TableData(c.Request.Context(), c.Param("table"))
	if err != nil {
		c.JSON(statusForAdminError(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"table": t, "columns": cols, "rows": rows})
}

func (h *adminTablesHandler) create(c *gin.Context) {
	var values domain.AdminRow
	if err := c.ShouldBindJSON(&values); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.Create(c.Request.Context(), c.Param("table"), values); err != nil {
		c.JSON(statusForAdminError(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *adminTablesHandler) update(c *gin.Context) {
	var values domain.AdminRow
	if err := c.ShouldBindJSON(&values); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.Update(c.Request.Context(), c.Param("table"), c.Param("id"), values); err != nil {
		c.JSON(statusForAdminError(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *adminTablesHandler) remove(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), c.Param("table"), c.Param("id")); err != nil {
		c.JSON(statusForAdminError(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func statusForAdminError(err error) int {
	if errors.Is(err, service.ErrTableNotEditable) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
