package gormrepo

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/Gtport/DPmodule/pkg/logger"
)

// sqlLimit — сколько байт запроса кладём в лог. Заливка снимка идёт пачками
// (CreateInBatches), и один такой INSERT — сотни килобайт значений: целиком он
// вытеснит из файла всё остальное, а для опознания места хватает начала.
const sqlLimit = 1 << 10

// zapGorm — мост между логом GORM и нашим zap. До него соединение открывалось
// с gormlogger.Silent: SQL-ошибки и медленные запросы не попадали в лог ВООБЩЕ,
// наружу выходил только текст ошибки, поднятый вызывающим кодом.
//
// Уровни: ошибка запроса — Error, превышение порога — Warn, всё прочее — Debug
// (на боевом info трасс SQL в файле нет, включаются сменой log.level).
type zapGorm struct {
	log           *zap.Logger
	slowThreshold time.Duration
}

// NewGormLogger собирает адаптер. Порог ≤ 0 — «медленных» не отмечаем.
func NewGormLogger(log *zap.Logger, slowThreshold time.Duration) gormlogger.Interface {
	return &zapGorm{log: log, slowThreshold: slowThreshold}
}

// LogMode — часть интерфейса GORM. Уровнем управляет zap (log.level в конфиге),
// поэтому переключатель GORM ничего не меняет и возвращает тот же адаптер.
func (l *zapGorm) LogMode(gormlogger.LogLevel) gormlogger.Interface { return l }

func (l *zapGorm) Info(ctx context.Context, msg string, args ...any) {
	l.named(ctx).Sugar().Debugf(msg, args...)
}

func (l *zapGorm) Warn(ctx context.Context, msg string, args ...any) {
	l.named(ctx).Sugar().Warnf(msg, args...)
}

func (l *zapGorm) Error(ctx context.Context, msg string, args ...any) {
	l.named(ctx).Sugar().Errorf(msg, args...)
}

// Trace вызывается GORM ПОСЛЕ КАЖДОГО запроса, включая построчные проходы
// пересборки снимка. Поэтому порядок здесь обратный привычному: сначала решаем,
// будем ли писать, и только потом собираем запись. fc() рендерит текст SQL со
// всеми значениями — на закрытом уровне это чистая трата на каждый запрос;
// логгер с именем тоже строим лишь тогда, когда строка реально пойдёт в файл.
func (l *zapGorm) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	elapsed := time.Since(begin)

	lvl, msg := zapcore.DebugLevel, "sql"
	switch {
	// ErrRecordNotFound — не поломка, а обычный ответ «строки нет»: так работают
	// проверки существования. На Error он поднимал бы ложную тревогу.
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		lvl, msg = zapcore.ErrorLevel, "sql: запрос не выполнен"
	case l.slowThreshold > 0 && elapsed > l.slowThreshold:
		lvl, msg = zapcore.WarnLevel, "sql: медленный запрос"
	}

	// Core общий у корневого и запросного логгеров, так что уровень проверяем
	// по нему — до всякой сборки.
	if !l.log.Core().Enabled(lvl) {
		return
	}

	sql, rows := fc()
	fields := []zap.Field{
		zap.Duration("took", elapsed),
		zap.Int64("rows", rows),
		zap.String("sql", truncateSQL(sql)),
	}
	if lvl == zapcore.ErrorLevel {
		fields = append(fields, zap.Error(err))
	}
	if lvl == zapcore.WarnLevel {
		fields = append(fields, zap.Duration("threshold", l.slowThreshold))
	}

	if ce := l.named(ctx).Check(lvl, msg); ce != nil {
		ce.Write(fields...)
	}
}

// named — логгер запроса, если GORM позвали внутри HTTP-обработки: тогда у
// строки SQL тот же request_id, что у строки http, и падение ручки читается
// сверху вниз. Крон и фоновые воркеры работают вне запроса — там корневой.
func (l *zapGorm) named(ctx context.Context) *zap.Logger {
	return logger.FromContextOr(ctx, l.log).Named("sql")
}

// truncateSQL режет запрос по длине. ToValidUTF8 — потому что режем по байтам,
// а в значениях русские имена станций и грузов: обрезка попадает в середину
// буквы, и в JSON-логе оставался бы символ замены.
func truncateSQL(sql string) string {
	if len(sql) <= sqlLimit {
		return sql
	}
	return strings.ToValidUTF8(sql[:sqlLimit], "") + "…"
}
