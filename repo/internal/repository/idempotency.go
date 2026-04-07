package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harborworks/booking-hub/internal/domain"
)

type IdempotencyRepository interface {
	Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error)
	Put(ctx context.Context, rec *domain.IdempotencyRecord) error
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

type idemRepo struct{ pool *pgxpool.Pool }

func NewIdempotencyRepository(pool *pgxpool.Pool) IdempotencyRepository {
	return &idemRepo{pool: pool}
}

func (r *idemRepo) Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	const q = `
		SELECT key, user_id, request_hash, status_code, response_body, content_type, created_at, expires_at
		FROM idempotency_keys WHERE key = $1
	`
	row := r.pool.QueryRow(ctx, q, key)
	var rec domain.IdempotencyRecord
	if err := row.Scan(&rec.Key, &rec.UserID, &rec.RequestHash, &rec.StatusCode, &rec.ResponseBody, &rec.ContentType, &rec.CreatedAt, &rec.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &rec, nil
}

func (r *idemRepo) Put(ctx context.Context, rec *domain.IdempotencyRecord) error {
	const q = `
		INSERT INTO idempotency_keys (key, user_id, request_hash, status_code, response_body, content_type, created_at, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (key) DO NOTHING
	`
	tag, err := r.pool.Exec(ctx, q,
		rec.Key, rec.UserID, rec.RequestHash, rec.StatusCode,
		rec.ResponseBody, rec.ContentType, rec.CreatedAt, rec.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("put idempotency: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Already existed: caller should re-fetch and serve cached.
		return domain.ErrConflict
	}
	return nil
}

func (r *idemRepo) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM idempotency_keys WHERE expires_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
