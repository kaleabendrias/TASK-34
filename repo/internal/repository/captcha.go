package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harborworks/booking-hub/internal/domain"
)

type CaptchaRepository interface {
	Create(ctx context.Context, c *domain.CaptchaChallenge) error
	Get(ctx context.Context, token string) (*domain.CaptchaChallenge, error)
	Consume(ctx context.Context, token string) error
}

type captchaRepo struct{ pool *pgxpool.Pool }

func NewCaptchaRepository(pool *pgxpool.Pool) CaptchaRepository {
	return &captchaRepo{pool: pool}
}

func (r *captchaRepo) Create(ctx context.Context, c *domain.CaptchaChallenge) error {
	const q = `
		INSERT INTO captcha_challenges (token, question, answer, created_at, expires_at, consumed)
		VALUES ($1,$2,$3,$4,$5,$6)
	`
	_, err := r.pool.Exec(ctx, q, c.Token, c.Question, c.Answer, c.CreatedAt, c.ExpiresAt, c.Consumed)
	if err != nil {
		return fmt.Errorf("insert captcha: %w", err)
	}
	return nil
}

func (r *captchaRepo) Get(ctx context.Context, token string) (*domain.CaptchaChallenge, error) {
	const q = `
		SELECT token, question, answer, created_at, expires_at, consumed
		FROM captcha_challenges WHERE token = $1
	`
	row := r.pool.QueryRow(ctx, q, token)
	var c domain.CaptchaChallenge
	if err := row.Scan(&c.Token, &c.Question, &c.Answer, &c.CreatedAt, &c.ExpiresAt, &c.Consumed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get captcha: %w", err)
	}
	return &c, nil
}

func (r *captchaRepo) Consume(ctx context.Context, token string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE captcha_challenges SET consumed = TRUE WHERE token = $1 AND consumed = FALSE`, token)
	if err != nil {
		return fmt.Errorf("consume captcha: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
