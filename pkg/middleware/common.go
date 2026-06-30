package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Gtport/DPmodule/pkg/logger"
)

const requestIDHeader = "X-Request-Id"

// InjectLogger puts the root zap logger into every request's context.
func InjectLogger(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(logger.WithContext(c.Request.Context(), log))
		c.Next()
	}
}

// Recover catches panics, logs them, and returns 500.
func Recover(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if p := recover(); p != nil {
				log.Error("panic recovered", zap.Any("panic", p))
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			}
		}()
		c.Next()
	}
}

// RequestID injects a request-id into the context and response headers.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader(requestIDHeader)
		if rid == "" {
			rid = uuid.NewString()
		}
		c.Header(requestIDHeader, rid)

		log := logger.FromContext(c.Request.Context()).With(zap.String("request_id", rid))
		c.Request = c.Request.WithContext(logger.WithContext(c.Request.Context(), log))
		c.Next()
	}
}

// RequestLogger logs method, path, status, and latency for every request.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.FromContext(c.Request.Context()).Info("http",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)),
		)
	}
}
