package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/port"
)

// tilesHandler — подложка карты: тайлы OSM из собственного кэша (БД tiles).
// Политика промаха — честный 404, в OSM не ходим (решение владельца 06.08.2026,
// docs/MAP_TILES.md); фронт рисует прозрачный квадрат, карта живёт без подложки.
type tilesHandler struct {
	repo port.TileRepository
}

func NewTilesHandler(repo port.TileRepository) *tilesHandler {
	return &tilesHandler{repo: repo}
}

// RegisterPublicRoutes вешает ручку на router ВНЕ JWT-группы: Leaflet грузит
// тайлы обычным <img> без заголовка Authorization. Утечки здесь нет — тайлы
// это публичные картинки OSM, данных дислокации в них нет.
func (h *tilesHandler) RegisterPublicRoutes(r *gin.Engine) {
	r.GET("/api/v1/map/tiles/:z/:x/:y", h.tile)
}

// tile godoc
// @Summary  Тайл подложки карты из кэша OSM (публичная, без JWT)
// @Tags     map
// @Produce  png
// @Param    z path int true "зум (0–11; сплошное покрытие кэша — 4–8)"
// @Param    x path int true "колонка тайла"
// @Param    y path int true "строка тайла"
// @Success  200 {file} binary
// @Failure  404 {object} handler.ErrorResponse "тайла нет в кэше"
// @Router   /api/v1/map/tiles/{z}/{x}/{y} [get]
func (h *tilesHandler) tile(c *gin.Context) {
	z, errZ := strconv.Atoi(c.Param("z"))
	x, errX := strconv.Atoi(c.Param("x"))
	y, errY := strconv.Atoi(c.Param("y"))
	if errZ != nil || errX != nil || errY != nil || z < 0 || z > 11 || x < 0 || y < 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "некорректные координаты тайла"})
		return
	}

	data, err := h.repo.Tile(c.Request.Context(), z, x, y)
	if err != nil {
		// Любой сбой чтения для клиента равен промаху: подложка — вспомогательная
		// картинка, различать «нет тайла» и «БД недоступна» фронту незачем.
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "тайла нет в кэше"})
		return
	}

	// Тайлы статичны (кэш наполнен переносом) — браузеру можно держать сутки.
	c.Header("Cache-Control", "public, max-age=86400")
	c.Data(http.StatusOK, "image/png", data)
}
