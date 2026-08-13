package gormrepo_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"

	gormrepo "github.com/Gtport/DPmodule/internal/repository/gorm"
)

func newSQLLogger(t *testing.T, lvl zapcore.Level, slow time.Duration) (interface {
	Trace(context.Context, time.Time, func() (string, int64), error)
}, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(lvl)
	return gormrepo.NewGormLogger(zap.New(core), slow), logs
}

func traceOnce(l interface {
	Trace(context.Context, time.Time, func() (string, int64), error)
}, took time.Duration, sql string, err error) {
	l.Trace(context.Background(), time.Now().Add(-took), func() (string, int64) { return sql, 1 }, err)
}

func TestGormLogger_FailedQueryIsError(t *testing.T) {
	l, logs := newSQLLogger(t, zapcore.DebugLevel, 500*time.Millisecond)

	traceOnce(l, time.Millisecond, `INSERT INTO vagon_history ...`,
		errors.New(`duplicate key value violates unique constraint "vagon_history_pkey"`))

	entries := logs.All()
	require.Len(t, entries, 1)
	assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
	assert.Contains(t, entries[0].ContextMap()["error"], "vagon_history_pkey")
}

// ErrRecordNotFound — обычный ответ «строки нет», а не поломка: на Error он
// поднимал бы ложную тревогу при каждой проверке существования.
func TestGormLogger_RecordNotFoundIsNotError(t *testing.T) {
	l, logs := newSQLLogger(t, zapcore.DebugLevel, 500*time.Millisecond)

	traceOnce(l, time.Millisecond, `SELECT * FROM ports WHERE ...`, gorm.ErrRecordNotFound)

	entries := logs.All()
	require.Len(t, entries, 1)
	assert.Equal(t, zapcore.DebugLevel, entries[0].Level)
}

func TestGormLogger_SlowQueryIsWarn(t *testing.T) {
	l, logs := newSQLLogger(t, zapcore.DebugLevel, 100*time.Millisecond)

	traceOnce(l, 300*time.Millisecond, `SELECT * FROM vagon_history`, nil)

	entries := logs.All()
	require.Len(t, entries, 1)
	assert.Equal(t, zapcore.WarnLevel, entries[0].Level)
	assert.Equal(t, 100*time.Millisecond, entries[0].ContextMap()["threshold"])
}

func TestGormLogger_SlowThresholdDisabledByNonPositive(t *testing.T) {
	l, logs := newSQLLogger(t, zapcore.DebugLevel, 0)

	traceOnce(l, 10*time.Second, `SELECT pg_sleep(10)`, nil)

	require.Len(t, logs.All(), 1)
	assert.Equal(t, zapcore.DebugLevel, logs.All()[0].Level, "без порога медленных не отмечаем")
}

// На боевом уровне info трасса SQL не пишется — и, что важнее, не собирается:
// fc() рендерит запрос со всеми значениями и звался бы на КАЖДЫЙ запрос.
func TestGormLogger_SuccessNotRenderedWhenDebugOff(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	l := gormrepo.NewGormLogger(zap.New(core), 500*time.Millisecond)

	rendered := false
	l.Trace(context.Background(), time.Now(), func() (string, int64) {
		rendered = true
		return `SELECT 1`, 1
	}, nil)

	assert.Empty(t, logs.All())
	assert.False(t, rendered, "текст запроса не должен собираться на закрытом уровне")
}

func TestGormLogger_LongSQLTruncated(t *testing.T) {
	l, logs := newSQLLogger(t, zapcore.DebugLevel, 500*time.Millisecond)

	// Заливка снимка идёт пачками: такой INSERT — сотни килобайт значений.
	// В значениях русские имена станций, поэтому обрезка проверяется на них:
	// рез по байтам не должен оставить в логе битую букву.
	huge := `INSERT INTO disl_new VALUES ` + strings.Repeat(`'НАХОДКА-ВОСТОЧНАЯ',`, 5000)
	traceOnce(l, time.Millisecond, huge, nil)

	got, _ := logs.All()[0].ContextMap()["sql"].(string)
	assert.Less(t, len(got), len(huge))
	assert.True(t, strings.HasSuffix(got, "…"), "обрезка должна быть видна")
	assert.True(t, utf8Valid(got), "в логе не должно быть недобитых рун")
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "�") == s }
