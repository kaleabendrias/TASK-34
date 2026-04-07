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

type SessionRepository interface {
	Create(ctx context.Context, s *domain.Session) error
	Get(ctx context.Context, id string) (*domain.Session, error)
	Touch(ctx context.Context, id string, at time.Time, newExpires time.Time) error
	Delete(ctx context.Context, id string) error
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

type sessionRepo struct{ pool *pgxpool.Pool }

func NewSessionRepository(pool *pgxpool.Pool) SessionRepository {
	return &sessionRepo{pool: pool}
}

func (r *sessionRepo) Create(ctx context.Context, s *domain.Session) error {
	const q = `
		INSERT INTO sessions (id, user_id, created_at, last_activity_at, expires_at, user_agent, ip)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`
	_, err := r.pool.Exec(ctx, q,
		s.ID, s.UserID, s.CreatedAt, s.LastActivityAt, s.ExpiresAt, s.UserAgent, s.IP,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *sessionRepo) Get(ctx context.Context, id string) (*domain.Session, error) {
	const q = `
		SELECT id, user_id, created_at, last_activity_at, expires_at, user_agent, ip
		FROM sessions WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, q, id)
	var s domain.Session
	if err := row.Scan(&s.ID, &s.UserID, &s.CreatedAt, &s.LastActivityAt, &s.ExpiresAt, &s.UserAgent, &s.IP); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &s, nil
}

func (r *sessionRepo) Touch(ctx context.Context, id string, at time.Time, newExpires time.Time) error {
	const q = `
		UPDATE sessions SET last_activity_at = $2, expires_at = $3 WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, q, id, at, newExpires)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (r *sessionRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

func (r *sessionRepo) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
