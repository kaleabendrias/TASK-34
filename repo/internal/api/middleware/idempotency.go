package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/repository"
)

// IdempotencyHeader is the request header consumed by this middleware.
const IdempotencyHeader = "Idempotency-Key"

// captureWriter buffers the response so we can persist it as the canonical
// reply for any future request that reuses the same idempotency key.
type captureWriter struct {
	gin.ResponseWriter
	buf    bytes.Buffer
	status int
}

// Write is the only writer override we need; idempotency-wrapped handlers
// in this codebase always serialize via c.JSON, which routes through Write.
// The embedded gin.ResponseWriter supplies the rest of the interface.
func (w *captureWriter) Write(b []byte) (int, error) {
	w.buf.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *captureWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Idempotency wraps any side-effecting handler so retries with the same
// `Idempotency-Key` header always resolve to the *same* response. The first
// request executes the handler and persists the outcome; later requests with
// the same key short-circuit and replay the stored bytes. If a request body
// is reused with a different hash the request is rejected with 409.
//
// TTL: 24 hours, matching the default group-buy deadline.
func Idempotency(repo repository.IdempotencyRepository) gin.HandlerFunc {
	const ttl = 24 * time.Hour
	return func(c *gin.Context) {
		key := c.GetHeader(IdempotencyHeader)
		if key == "" {
			// Header is optional; without it the handler runs as usual but
			// without at-most-once guarantees.
			c.Next()
			return
		}

		// Read and replay the request body so the downstream handler still
		// sees it.
		var bodyBytes []byte
		if c.Request.Body != nil {
			b, err := io.ReadAll(c.Request.Body)
			if err == nil {
				bodyBytes = b
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}
		hash := sha256.Sum256(append([]byte(c.Request.Method+" "+c.Request.URL.Path+"?"), bodyBytes...))
		hashStr := hex.EncodeToString(hash[:])

		ctx := c.Request.Context()
		if existing, err := repo.Get(ctx, key); err == nil {
			if existing.RequestHash != hashStr {
				c.AbortWithStatusJSON(http.StatusConflict, gin.H{
					"error": "idempotency key reused with a different request body",
				})
				return
			}
			c.Header("Content-Type", existing.ContentType)
			c.Header("Idempotent-Replay", "true")
			c.Status(existing.StatusCode)
			_, _ = c.Writer.Write(existing.ResponseBody)
			c.Abort()
			return
		} else if !errors.Is(err, domain.ErrNotFound) {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "idempotency lookup failed"})
			return
		}

		// First time we see this key. Capture the response and persist it
		// once the handler returns.
		cw := &captureWriter{ResponseWriter: c.Writer, status: http.StatusOK}
		c.Writer = cw

		// Stash the user id (if available) for the audit row.
		var userID *uuid.UUID
		if u := CurrentUser(c); u != nil {
			id := u.ID
			userID = &id
		}

		c.Next()

		now := time.Now().UTC()
		_ = repo.Put(ctx, &domain.IdempotencyRecord{
			Key:          key,
			UserID:       userID,
			RequestHash:  hashStr,
			StatusCode:   cw.status,
			ResponseBody: cw.buf.Bytes(),
			ContentType:  cw.Header().Get("Content-Type"),
			CreatedAt:    now,
			ExpiresAt:    now.Add(ttl),
		})
	}
}
