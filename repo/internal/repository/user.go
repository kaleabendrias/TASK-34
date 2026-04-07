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

type UserRepository interface {
	Create(ctx context.Context, u *domain.User) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	GetByUsername(ctx context.Context, username string) (*domain.User, error)
	RecordFailedLogin(ctx context.Context, id uuid.UUID, lockUntil *time.Time) error
	ResetFailedLogin(ctx context.Context, id uuid.UUID, loginAt time.Time) error
	SetBlacklist(ctx context.Context, id uuid.UUID, blacklisted bool, reason string) error
	SetAdmin(ctx context.Context, id uuid.UUID, admin bool) error
	Anonymize(ctx context.Context, id uuid.UUID) error
}

type userRepo struct{ pool *pgxpool.Pool }

func NewUserRepository(pool *pgxpool.Pool) UserRepository {
	return &userRepo{pool: pool}
}

const userColumns = `
	id, username, password_hash, is_blacklisted, blacklist_reason,
	is_admin, anonymized_at,
	failed_attempts, locked_until, last_login_at, created_at, updated_at
`

func (r *userRepo) Create(ctx context.Context, u *domain.User) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now
	const q = `
		INSERT INTO users (id, username, password_hash, is_blacklisted, blacklist_reason,
		                   is_admin, failed_attempts, locked_until, last_login_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`
	_, err := r.pool.Exec(ctx, q,
		u.ID, u.Username, u.PasswordHash, u.IsBlacklisted, u.BlacklistReason,
		u.IsAdmin, u.FailedAttempts, u.LockedUntil, u.LastLoginAt, u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

// SetAdmin promotes or demotes a user. Used by the bootstrap seeder.
func (r *userRepo) SetAdmin(ctx context.Context, id uuid.UUID, admin bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET is_admin = $2, updated_at = NOW() WHERE id = $1`, id, admin)
	return err
}

// Anonymize hard-clears all PII fields on a user row but keeps the row so
// foreign keys (analytics, audit) remain valid.
func (r *userRepo) Anonymize(ctx context.Context, id uuid.UUID) error {
	const q = `
		UPDATE users
		SET username = 'deleted_' || substring(id::text from 1 for 8),
		    password_hash = '',
		    is_blacklisted = TRUE,
		    blacklist_reason = 'account deleted',
		    anonymized_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, q, id)
	return err
}

func (r *userRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

func (r *userRepo) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE LOWER(username) = LOWER($1)`
	row := r.pool.QueryRow(ctx, q, username)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get user by username: %w", err)
	}
	return u, nil
}

func (r *userRepo) RecordFailedLogin(ctx context.Context, id uuid.UUID, lockUntil *time.Time) error {
	const q = `
		UPDATE users
		SET failed_attempts = failed_attempts + 1,
		    locked_until    = COALESCE($2, locked_until),
		    updated_at      = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, q, id, lockUntil)
	if err != nil {
		return fmt.Errorf("record failed login: %w", err)
	}
	return nil
}

func (r *userRepo) ResetFailedLogin(ctx context.Context, id uuid.UUID, loginAt time.Time) error {
	const q = `
		UPDATE users
		SET failed_attempts = 0,
		    locked_until    = NULL,
		    last_login_at   = $2,
		    updated_at      = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, q, id, loginAt)
	if err != nil {
		return fmt.Errorf("reset failed login: %w", err)
	}
	return nil
}

func (r *userRepo) SetBlacklist(ctx context.Context, id uuid.UUID, blacklisted bool, reason string) error {
	const q = `
		UPDATE users SET is_blacklisted = $2, blacklist_reason = $3, updated_at = NOW() WHERE id = $1
	`
	tag, err := r.pool.Exec(ctx, q, id, blacklisted, reason)
	if err != nil {
		return fmt.Errorf("set blacklist: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func scanUser(s rowScanner) (*domain.User, error) {
	var (
		u            domain.User
		lockedUntil  *time.Time
		lastLogin    *time.Time
		anonymizedAt *time.Time
	)
	if err := s.Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.IsBlacklisted, &u.BlacklistReason,
		&u.IsAdmin, &anonymizedAt,
		&u.FailedAttempts, &lockedUntil, &lastLogin, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		return nil, err
	}
	u.LockedUntil = lockedUntil
	u.LastLoginAt = lastLogin
	u.AnonymizedAt = anonymizedAt
	return &u, nil
}
