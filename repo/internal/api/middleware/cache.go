package middleware

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/harborworks/booking-hub/internal/infrastructure/cache"
)

// CacheBypassHeader lets an admin force a fresh read.
const CacheBypassHeader = "X-Cache-Bypass"

// ReadThroughCache caches GET responses for the configured TTL. Cache keys
// are derived from the canonical request path + query string. Admins can
// bypass the cache by sending `X-Cache-Bypass: true`; non-admins are ignored
// for that header.
func ReadThroughCache(c *cache.Cache) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if ctx.Request.Method != http.MethodGet {
			ctx.Next()
			return
		}
		if isAdminBypass(ctx) {
			ctx.Header("X-Cache", "BYPASS")
			ctx.Next()
			return
		}
		key := ctx.Request.URL.Path + "?" + ctx.Request.URL.RawQuery
		if cached, hit := c.Get(key); hit {
			ctx.Header("Content-Type", "application/json")
			ctx.Header("X-Cache", "HIT")
			ctx.Status(http.StatusOK)
			_, _ = ctx.Writer.Write(cached)
			ctx.Abort()
			return
		}

		// Capture and store on success.
		cw := &captureWriter{ResponseWriter: ctx.Writer, status: http.StatusOK}
		ctx.Writer = cw
		ctx.Header("X-Cache", "MISS")
		ctx.Next()
		if cw.status >= 200 && cw.status < 300 && bytes.HasPrefix(cw.buf.Bytes(), []byte("{")) {
			c.Set(key, cw.buf.Bytes())
		}
	}
}

func isAdminBypass(c *gin.Context) bool {
	if !strings.EqualFold(c.GetHeader(CacheBypassHeader), "true") {
		return false
	}
	u := CurrentUser(c)
	return u != nil && u.IsAdmin
}
