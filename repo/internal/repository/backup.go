package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harborworks/booking-hub/internal/domain"
)

type BackupRepository interface {
	Insert(ctx context.Context, b *domain.Backup) error
	List(ctx context.Context, limit int) ([]domain.Backup, error)
	LastFull(ctx context.Context) (*domain.Backup, error)
	// LastSuccessful returns the most recent backup of any kind (full or
	// incremental). The incremental scheduler uses this as its baseline so
	// each new incremental only contains rows changed since the previous
	// successful backup, instead of re-emitting everything since the last
	// full snapshot.
	LastSuccessful(ctx context.Context) (*domain.Backup, error)
	IncrementalsAfter(ctx context.Context, after time.Time) ([]domain.Backup, error)
}

type backupRepo struct{ pool *pgxpool.Pool }

func NewBackupRepository(pool *pgxpool.Pool) BackupRepository {
	return &backupRepo{pool: pool}
}

func (r *backupRepo) Insert(ctx context.Context, b *domain.Backup) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	if b.TakenAt.IsZero() {
		b.TakenAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO backups (id, kind, path, size_bytes, taken_at, detail)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, b.ID, b.Kind, b.Path, b.SizeBytes, b.TakenAt, b.Detail)
	return err
}

func (r *backupRepo) List(ctx context.Context, limit int) ([]domain.Backup, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, kind, path, size_bytes, taken_at, detail
		FROM backups ORDER BY taken_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Backup, 0)
	for rows.Next() {
		var b domain.Backup
		if err := rows.Scan(&b.ID, &b.Kind, &b.Path, &b.SizeBytes, &b.TakenAt, &b.Detail); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *backupRepo) LastSuccessful(ctx context.Context) (*domain.Backup, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, kind, path, size_bytes, taken_at, detail
		FROM backups ORDER BY taken_at DESC LIMIT 1
	`)
	var b domain.Backup
	if err := row.Scan(&b.ID, &b.Kind, &b.Path, &b.SizeBytes, &b.TakenAt, &b.Detail); err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *backupRepo) LastFull(ctx context.Context) (*domain.Backup, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, kind, path, size_bytes, taken_at, detail
		FROM backups WHERE kind = 'full' ORDER BY taken_at DESC LIMIT 1
	`)
	var b domain.Backup
	if err := row.Scan(&b.ID, &b.Kind, &b.Path, &b.SizeBytes, &b.TakenAt, &b.Detail); err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *backupRepo) IncrementalsAfter(ctx context.Context, after time.Time) ([]domain.Backup, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, kind, path, size_bytes, taken_at, detail
		FROM backups WHERE kind = 'incremental' AND taken_at > $1 ORDER BY taken_at
	`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Backup, 0)
	for rows.Next() {
		var b domain.Backup
		if err := rows.Scan(&b.ID, &b.Kind, &b.Path, &b.SizeBytes, &b.TakenAt, &b.Detail); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
