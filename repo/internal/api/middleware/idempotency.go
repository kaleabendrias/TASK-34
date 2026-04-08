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

func (w *captureWriter) Write(b []byte) (int, error) {
	w.buf.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *captureWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Idempotency wraps any side-effecting handler so retries with the same
// `Idempotency-Key` header always resolve to the *same* response.
//
// At-most-once semantics are enforced via an atomic reservation: the first
// request inserts a 'pending' row, runs the handler, then promotes the row
// to 'completed' with the captured response. Concurrent requests with the
// same key see the pending row and short-circuit with HTTP 409 — they may
// retry once the original request settles. Subsequent requests see the
// completed row and replay the stored bytes.
//
// Keys are scoped per (user_id, key). Two different users may submit the
// same client-generated header value without colliding. Anonymous requests
// (no session) share an isolated null-user namespace.
//
// If a request body is reused with a different hash the request is rejected
// with 409 ErrIdempotencyMismatch.
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

		// Scope every reservation to the authenticated user. Anonymous
		// requests use a NULL user_id and live in their own namespace.
		var userID *uuid.UUID
		if u := CurrentUser(c); u != nil {
			id := u.ID
			userID = &id
		}

		ctx := c.Request.Context()
		existing, reserved, err := repo.Reserve(ctx, userID, key, hashStr, ttl)
		switch {
		case err == nil && reserved:
			// First-mover: own the slot, run the handler, capture the
			// response, then promote the reservation to completed.
		case errors.Is(err, domain.ErrIdempotencyMismatch):
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{
				"error": "idempotency key reused with a different request body",
			})
			return
		case errors.Is(err, domain.ErrConflict):
			// A pending sibling holds the slot. Tell the client to retry
			// once the in-flight request settles. 409 + Retry-After.
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{
				"error": "idempotency key is currently in flight, retry shortly",
			})
			return
		case err == nil && existing != nil && existing.Status == domain.IdempotencyStatusCompleted:
			c.Header("Content-Type", existing.ContentType)
			c.Header("Idempotent-Replay", "true")
			c.Status(existing.StatusCode)
			_, _ = c.Writer.Write(existing.ResponseBody)
			c.Abort()
			return
		default:
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "idempotency lookup failed"})
			return
		}

		// First time we see this key. Capture the response and persist it
		// once the handler returns. Make sure the pending reservation is
		// either promoted (success) or released (panic / no response) so
		// the key never gets stuck.
		cw := &captureWriter{ResponseWriter: c.Writer, status: http.StatusOK}
		c.Writer = cw

		completed := false
		defer func() {
			if !completed {
				// Handler panicked or never wrote a response — drop the
				// pending row so the client can safely retry.
				_ = repo.ReleasePending(ctx, userID, key)
			}
		}()

		c.Next()

		if err := repo.Complete(ctx, userID, key, cw.status, cw.buf.Bytes(), cw.Header().Get("Content-Type")); err == nil {
			completed = true
		}
	}
}
