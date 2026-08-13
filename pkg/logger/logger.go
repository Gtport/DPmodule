package logger

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/lumberjack.v2"
)

type contextKey struct{}

// Config holds logger configuration.
type Config struct {
	Level      string
	Env        string
	File       string // path to log file; empty = stdout only
	MaxSizeMB  int    // max file size before rotation
	MaxBackups int    // number of rotated files to keep
	MaxAgeDays int    // days to keep rotated files
}

// New creates a zap logger that writes to stdout and optionally to a rotating file.
//
// Неизвестный уровень — ОТКАЗ, а не тихий откат на info: опечатка в конфиге
// («infoo», «verbose») иначе оборачивалась бы молчанием, и разбор поломки шёл бы
// по файлу, где нужных записей просто нет, — а причину не видно, потому что
// сообщить о ней должен был как раз лог. Пустое значение отказом не считается:
// это «не задано», и config.Load подставляет ему info.
func New(cfg Config) (*zap.Logger, error) {
	level := zapcore.InfoLevel
	if cfg.Level != "" {
		var err error
		if level, err = zapcore.ParseLevel(cfg.Level); err != nil {
			return nil, fmt.Errorf("log.level %q: %w (допустимо: debug, info, warn, error, dpanic, panic, fatal)",
				cfg.Level, err)
		}
	}

	cores := []zapcore.Core{
		zapcore.NewCore(buildEncoder(cfg.Env), zapcore.AddSync(os.Stdout), level),
	}

	if cfg.File != "" {
		fileWriter := &lumberjack.Logger{
			Filename:   cfg.File,
			MaxSize:    maxOrDefault(cfg.MaxSizeMB, 100),
			MaxBackups: maxOrDefault(cfg.MaxBackups, 5),
			MaxAge:     maxOrDefault(cfg.MaxAgeDays, 30),
			Compress:   true,
		}
		// В ФАЙЛ всегда JSON, независимо от env. Консольный энкодер (env=dev)
		// красит уровни ANSI-кодами: на экране это удобно, а в файле мусор —
		// «\x1b[34mINFO\x1b[0m» ломает grep и разбор сборщиками логов.
		cores = append(cores, zapcore.NewCore(
			zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
			zapcore.AddSync(fileWriter), level))
	}

	core := zapcore.NewTee(cores...)

	opts := []zap.Option{zap.AddCaller()}
	if cfg.Env == "dev" {
		opts = append(opts, zap.Development())
	}

	return zap.New(core, opts...), nil
}

// FromContext extracts a logger from context; falls back to a no-op logger.
func FromContext(ctx context.Context) *zap.Logger {
	return FromContextOr(ctx, zap.NewNop())
}

// FromContextOr — то же, но с явным запасным логгером вместо молчащего.
// Нужен там, где потеря записи хуже лишней строки: фон и кроны работают вне
// запроса (в контексте логгера нет), а Nop проглотил бы сообщение целиком.
func FromContextOr(ctx context.Context, def *zap.Logger) *zap.Logger {
	if l, ok := ctx.Value(contextKey{}).(*zap.Logger); ok && l != nil {
		return l
	}
	return def
}

// WithContext returns a child context carrying the logger.
func WithContext(ctx context.Context, l *zap.Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

func buildEncoder(env string) zapcore.Encoder {
	if env == "dev" {
		cfg := zap.NewDevelopmentEncoderConfig()
		cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		return zapcore.NewConsoleEncoder(cfg)
	}
	return zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
}

func maxOrDefault(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}
