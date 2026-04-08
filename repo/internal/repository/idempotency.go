package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harborworks/booking-hub/internal/domain"
)

// IdempotencyRepository persists at-most-once outcomes for side-effecting
// requests, scoped per (user_id, key) so two users can never collide on the
// same client-supplied header value.
//
// The middleware uses Reserve to atomically claim a key with status='pending'
// before the handler runs. The first caller wins; concurrent callers either
// see a pending row (and must wait/retry) or a completed row (replay).
// Once the handler returns, Complete promotes the row to status='completed'
// with the captured response. ReleasePending lets the middleware drop a
// pending reservation if the handler panics or the request is canceled
// before any response is written.
type IdempotencyRepository interface {
	// Reserve attempts to insert a fresh pending row. Returns
	//   (rec=nil, reserved=true,  nil)              — caller owns the slot
	//   (rec=existing, reserved=false, nil)         — completed row exists, replay
	//   (rec=pending,  reserved=false, ErrConflict) — pending row exists, retry
	//   (rec=existing, reserved=false, ErrIdempotencyMismatch) — body hash mismatch
	Reserve(ctx context.Context, userID *uuid.UUID, key, requestHash string, ttl time.Duration) (*domain.IdempotencyRecord, bool, error)

	// Complete promotes a pending row to a completed response.
	Complete(ctx context.Context, userID *uuid.UUID, key string, statusCode int, body []byte, contentType string) error

	// ReleasePending deletes a pending row so the key can be retried. Used
	// when the handler panics or the response is never produced.
	ReleasePending(ctx context.Context, userID *uuid.UUID, key string) error

	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

type idemRepo struct{ pool *pgxpool.Pool }

func NewIdempotencyRepository(pool *pgxpool.Pool) IdempotencyRepository {
	return &idemRepo{pool: pool}
}

// scopeMatch builds the user_id equality clause that matches both
// authenticated (user_id = $1) and anonymous (user_id IS NULL) callers.
const scopeMatch = `((user_id = $1 AND $1 IS NOT NULL) OR (user_id IS NULL AND $1 IS NULL))`

func (r *idemRepo) Reserve(ctx context.Context, userID *uuid.UUID, key, requestHash string, ttl time.Duration) (*domain.IdempotencyRecord, bool, error) {
	now := time.Now().UTC()
	expires := now.Add(ttl)

	// Try to claim the slot. ON CONFLICT DO NOTHING relies on the partial
	// unique indexes added in migration 0004 (one for user_id IS NOT NULL,
	// one for user_id IS NULL).
	const insert = `
		INSERT INTO idempotency_keys
		    (key, user_id, request_hash, status_code, response_body, content_type, status, created_at, expires_at)
		VALUES ($1, $2, $3, NULL, NULL, '', 'pending', $4, $5)
		ON CONFLICT DO NOTHING
	`
	tag, err := r.pool.Exec(ctx, insert, key, userID, requestHash, now, expires)
	if err != nil {
		return nil, false, fmt.Errorf("reserve idempotency: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil, true, nil
	}

	// Someone already holds the slot — fetch the current row to decide
	// whether to replay, mismatch, or report a pending conflict.
	existing, err := r.getScoped(ctx, userID, key)
	if err != nil {
		return nil, false, err
	}
	if existing.RequestHash != requestHash {
		return existing, false, domain.ErrIdempotencyMismatch
	}
	if existing.Status == domain.IdempotencyStatusPending {
		return existing, false, domain.ErrConflict
	}
	return existing, false, nil
}

func (r *idemRepo) Complete(ctx context.Context, userID *uuid.UUID, key string, statusCode int, body []byte, contentType string) error {
	const upd = `
		UPDATE idempotency_keys
		   SET status        = 'completed',
		       status_code   = $3,
		       response_body = $4,
		       content_type  = $5
		 WHERE key = $2 AND ` + scopeMatch + `
		   AND status = 'pending'
	`
	tag, err := r.pool.Exec(ctx, upd, userID, key, statusCode, body, contentType)
	if err != nil {
		return fmt.Errorf("complete idempotency: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *idemRepo) ReleasePending(ctx context.Context, userID *uuid.UUID, key string) error {
	const del = `
		DELETE FROM idempotency_keys
		 WHERE key = $2 AND ` + scopeMatch + ` AND status = 'pending'
	`
	_, err := r.pool.Exec(ctx, del, userID, key)
	return err
}

func (r *idemRepo) getScoped(ctx context.Context, userID *uuid.UUID, key string) (*domain.IdempotencyRecord, error) {
	const q = `
		SELECT key, user_id, request_hash,
		       COALESCE(status_code, 0),
		       COALESCE(response_body, ''::bytea),
		       content_type, status, created_at, expires_at
		  FROM idempotency_keys
		 WHERE key = $2 AND ` + scopeMatch
	row := r.pool.QueryRow(ctx, q, userID, key)
	var rec domain.IdempotencyRecord
	if err := row.Scan(
		&rec.Key, &rec.UserID, &rec.RequestHash,
		&rec.StatusCode, &rec.ResponseBody,
		&rec.ContentType, &rec.Status, &rec.CreatedAt, &rec.ExpiresAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &rec, nil
}

func (r *idemRepo) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM idempotency_keys WHERE expires_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
