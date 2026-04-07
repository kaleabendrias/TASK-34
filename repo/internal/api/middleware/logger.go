package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// RequestLogger emits a structured slog entry per HTTP request.
func RequestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		entry := log.With(
			"method", c.Request.Method,
			"path", path,
			"query", query,
			"status", status,
			"latency_ms", latency.Milliseconds(),
			"client_ip", c.ClientIP(),
		)
		switch {
		case status >= 500:
			entry.Error("request")
		case status >= 400:
			entry.Warn("request")
		default:
			entry.Info("request")
		}
	}
}

// Recovery converts panics into 500 responses without crashing the process.
// Backed by gin's built-in recovery, which is well-tested and writes a clean
// 500 with no body. The slog.Logger argument is accepted for symmetry with
// the other middleware factories.
func Recovery(_ *slog.Logger) gin.HandlerFunc {
	return gin.Recovery()
}
