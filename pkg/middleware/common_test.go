package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/Gtport/DPmodule/internal/auth"
	"github.com/Gtport/DPmodule/pkg/middleware"
)

// newLogRouter — цепочка мидлварей как в server.Build, с логгером-наблюдателем
// вместо файла. Уровень Debug: тихие пути (пробы, тайлы) пишутся именно им, и
// на Info их бы просто не было видно в тесте.
func newLogRouter(t *testing.T) (*gin.Engine, *observer.ObservedLogs) {
	t.Helper()

	core, logs := observer.New(zapcore.DebugLevel)
	log := zap.New(core)

	r := gin.New()
	r.Use(
		middleware.InjectLogger(log),
		middleware.RequestID(),
		middleware.RequestLogger(),
		middleware.Recover(log),
	)
	return r, logs
}

// httpEntry — строка доступа (их всего одна на запрос).
func httpEntry(t *testing.T, logs *observer.ObservedLogs) observer.LoggedEntry {
	t.Helper()
	found := logs.FilterMessage("http").All()
	require.Len(t, found, 1, "ожидалась ровно одна строка http")
	return found[0]
}

func fieldsOf(e observer.LoggedEntry) map[string]any { return e.ContextMap() }

func TestRequestLogger_ServerErrorLogsReasonAtErrorLevel(t *testing.T) {
	r, logs := newLogRouter(t)
	r.GET("/api/v1/boom", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "разбор АСУ: неожиданный конец файла"})
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/boom?client=01126022", nil))

	e := httpEntry(t, logs)
	assert.Equal(t, zapcore.ErrorLevel, e.Level)

	f := fieldsOf(e)
	assert.Equal(t, int64(500), f["status"])
	assert.Equal(t, "/api/v1/boom", f["path"])
	// Причина отказа — главное, чего не было в логе до этой правки.
	assert.Contains(t, f["error"], "неожиданный конец файла")
	// Query у неуспешных нужен: в нём фильтр, с которым ручка сломалась.
	assert.Equal(t, "client=01126022", f["query"])
	assert.NotEmpty(t, f["request_id"])
}

func TestRequestLogger_ClientErrorIsWarn(t *testing.T) {
	r, logs := newLogRouter(t)
	r.POST("/api/v1/arrivals/confirm", func(c *gin.Context) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "прибытие позже текущего момента"})
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/arrivals/confirm", nil))

	e := httpEntry(t, logs)
	assert.Equal(t, zapcore.WarnLevel, e.Level)
	assert.Contains(t, fieldsOf(e)["error"], "прибытие позже")
}

func TestRequestLogger_SuccessBodyNotCaptured(t *testing.T) {
	r, logs := newLogRouter(t)
	// Успешная выгрузка: держать её копию в памяти незачем (история — 11 МБ).
	big := strings.Repeat("x", 64<<10)
	r.GET("/api/v1/history/excel", func(c *gin.Context) {
		c.String(http.StatusOK, big)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/history/excel?from=2026-01-01", nil))

	e := httpEntry(t, logs)
	assert.Equal(t, zapcore.InfoLevel, e.Level)
	f := fieldsOf(e)
	assert.NotContains(t, f, "error")
	// Query успешного запроса — шум: фильтр отчёта интересен только при отказе.
	assert.NotContains(t, f, "query")
}

func TestRequestLogger_ErrorBodyTruncated(t *testing.T) {
	r, logs := newLogRouter(t)
	r.GET("/api/v1/verbose", func(c *gin.Context) {
		c.String(http.StatusBadGateway, strings.Repeat("я", 8<<10))
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/verbose", nil))

	got, _ := fieldsOf(httpEntry(t, logs))["error"].(string)
	assert.NotEmpty(t, got)
	assert.LessOrEqual(t, len(got), 2<<10, "тело ошибки должно обрезаться по errBodyLimit")
}

// Пробы и тайлы дёргаются постоянно (healthcheck раз в несколько секунд, один
// экран карты — десятки тайлов): на Info они забивают файл.
func TestRequestLogger_QuietPathsDemotedButFailuresNot(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		status int
		want   zapcore.Level
	}{
		{"проба готовности жива", "/ready", http.StatusOK, zapcore.DebugLevel},
		{"тайл найден", "/api/v1/map/tiles/8/1/2", http.StatusOK, zapcore.DebugLevel},
		{"база недоступна", "/ready", http.StatusServiceUnavailable, zapcore.ErrorLevel},
		{"тайла нет в кэше", "/api/v1/map/tiles/8/1/2", http.StatusNotFound, zapcore.WarnLevel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, logs := newLogRouter(t)
			r.GET(tc.path, func(c *gin.Context) { c.Status(tc.status) })

			r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tc.path, nil))

			assert.Equal(t, tc.want, httpEntry(t, logs).Level)
		})
	}
}

func TestRequestLogger_ActorFromClaims(t *testing.T) {
	r, logs := newLogRouter(t)
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(
			auth.WithClaims(c.Request.Context(), &auth.Claims{Username: "dispatcher1"}))
		c.Next()
	})
	r.PUT("/api/v1/dislocation/arrivals/vagons", func(c *gin.Context) {
		c.JSON(http.StatusForbidden, gin.H{"error": "правки за пределами смены — senior-operator"})
	})

	r.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPut, "/api/v1/dislocation/arrivals/vagons", nil))

	assert.Equal(t, "dispatcher1", fieldsOf(httpEntry(t, logs))["user"])
}

// Паника должна оставлять ОБЕ строки, связанные одним request_id: раньше
// Recover стоял выше RequestID и писал корневым логгером, без него.
func TestRecover_PanicLinkedToRequestByID(t *testing.T) {
	r, logs := newLogRouter(t)
	r.GET("/api/v1/panic", func(c *gin.Context) { panic("nil map write") })

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/panic", nil))

	assert.Equal(t, http.StatusInternalServerError, rr.Code)

	panics := logs.FilterMessage("panic recovered").All()
	require.Len(t, panics, 1)
	pf := fieldsOf(panics[0])
	assert.Equal(t, "nil map write", pf["panic"])
	assert.Equal(t, "/api/v1/panic", pf["path"])
	assert.NotEmpty(t, pf["stack"])

	// Строка http пишется в defer, поэтому переживает разворот стека.
	hf := fieldsOf(httpEntry(t, logs))
	assert.Equal(t, int64(500), hf["status"])
	assert.NotEmpty(t, hf["request_id"])
	assert.Equal(t, pf["request_id"], hf["request_id"], "паника и запрос должны сшиваться по request_id")
}

func TestRequestID_ReusesIncomingHeader(t *testing.T) {
	r, logs := newLogRouter(t)
	r.GET("/api/v1/ping", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	req.Header.Set("X-Request-Id", "from-ingress")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, "from-ingress", rr.Header().Get("X-Request-Id"))
	assert.Equal(t, "from-ingress", fieldsOf(httpEntry(t, logs))["request_id"])
}
