package logger

import (
	"context"
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
func New(cfg Config) (*zap.Logger, error) {
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		level = zapcore.InfoLevel
	}

	encoder := buildEncoder(cfg.Env)
	cores := []zapcore.Core{
		zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level),
	}

	if cfg.File != "" {
		fileWriter := &lumberjack.Logger{
			Filename:   cfg.File,
			MaxSize:    maxOrDefault(cfg.MaxSizeMB, 100),
			MaxBackups: maxOrDefault(cfg.MaxBackups, 5),
			MaxAge:     maxOrDefault(cfg.MaxAgeDays, 30),
			Compress:   true,
		}
		cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(fileWriter), level))
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
	if l, ok := ctx.Value(contextKey{}).(*zap.Logger); ok && l != nil {
		return l
	}
	return zap.NewNop()
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
