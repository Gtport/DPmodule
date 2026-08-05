package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Gtport/DPmodule/internal/port"
)

// fakeTileRepo — кэш из одного тайла 5/28/11.
type fakeTileRepo struct{}

func (fakeTileRepo) Tile(_ context.Context, z, x, y int) ([]byte, error) {
	if z == 5 && x == 28 && y == 11 {
		return []byte("png-bytes"), nil
	}
	return nil, port.ErrTileNotFound
}

func tilesRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewTilesHandler(fakeTileRepo{}).RegisterPublicRoutes(r)
	return r
}

func TestTiles_HitServesPNGWithCacheHeader(t *testing.T) {
	w := httptest.NewRecorder()
	tilesRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/map/tiles/5/28/11", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("код = %d, ждали 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, ждали image/png", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc == "" {
		t.Error("Cache-Control пуст — браузер будет дёргать тайл на каждый рендер")
	}
	if w.Body.String() != "png-bytes" {
		t.Errorf("тело = %q, ждали байты тайла как есть", w.Body.String())
	}
}

func TestTiles_MissIs404(t *testing.T) {
	w := httptest.NewRecorder()
	tilesRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/map/tiles/8/0/0", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("код = %d, ждали 404 (промах кэша — честный 404, в OSM не ходим)", w.Code)
	}
}

func TestTiles_BadCoordsIs400(t *testing.T) {
	for _, url := range []string{
		"/api/v1/map/tiles/abc/1/1", // не число
		"/api/v1/map/tiles/12/1/1",  // зум за пределом
		"/api/v1/map/tiles/5/-1/1",  // отрицательная колонка
	} {
		w := httptest.NewRecorder()
		tilesRouter().ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: код = %d, ждали 400", url, w.Code)
		}
	}
}
