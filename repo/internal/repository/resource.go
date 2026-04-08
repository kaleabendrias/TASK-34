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

type ResourceRepository interface {
	List(ctx context.Context) ([]domain.Resource, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Resource, error)
	Create(ctx context.Context, r *domain.Resource) error
	// InsertManyTx persists every supplied resource inside a single
	// transaction. The whole batch rolls back if any insert fails. Conflicts
	// on the unique `name` index also abort the transaction so the import
	// remains all-or-nothing.
	InsertManyTx(ctx context.Context, rows []domain.Resource) (int, error)
	// SumActivePartySizesInWindow returns the total party_size of every
	// non-canceled booking that overlaps the supplied window. Used by the
	// per-slot capacity accounting.
	SumActivePartySizesInWindow(ctx context.Context, resourceID uuid.UUID, start, end time.Time) (int, error)
}

type resourceRepo struct{ pool *pgxpool.Pool }

func NewResourceRepository(pool *pgxpool.Pool) ResourceRepository {
	return &resourceRepo{pool: pool}
}

func (r *resourceRepo) List(ctx context.Context) ([]domain.Resource, error) {
	const q = `SELECT id, name, description, capacity, created_at FROM resources ORDER BY name`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list resources: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Resource, 0)
	for rows.Next() {
		var x domain.Resource
		if err := rows.Scan(&x.ID, &x.Name, &x.Description, &x.Capacity, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (r *resourceRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Resource, error) {
	const q = `SELECT id, name, description, capacity, created_at FROM resources WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	var x domain.Resource
	if err := row.Scan(&x.ID, &x.Name, &x.Description, &x.Capacity, &x.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get resource: %w", err)
	}
	return &x, nil
}

func (r *resourceRepo) Create(ctx context.Context, x *domain.Resource) error {
	if x.ID == uuid.Nil {
		x.ID = uuid.New()
	}
	if x.CreatedAt.IsZero() {
		x.CreatedAt = time.Now().UTC()
	}
	if x.Capacity <= 0 {
		x.Capacity = 1
	}
	const q = `INSERT INTO resources (id, name, description, capacity, created_at) VALUES ($1,$2,$3,$4,$5)`
	if _, err := r.pool.Exec(ctx, q, x.ID, x.Name, x.Description, x.Capacity, x.CreatedAt); err != nil {
		return fmt.Errorf("insert resource: %w", err)
	}
	return nil
}

func (r *resourceRepo) InsertManyTx(ctx context.Context, rows []domain.Resource) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	// Defer rollback; commit at end is the only success path. Rollback after
	// commit is a no-op so this is safe.
	defer func() { _ = tx.Rollback(ctx) }()

	const q = `INSERT INTO resources (id, name, description, capacity, created_at) VALUES ($1,$2,$3,$4,$5)`
	now := time.Now().UTC()
	inserted := 0
	for i := range rows {
		row := &rows[i]
		if row.ID == uuid.Nil {
			row.ID = uuid.New()
		}
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
		if _, err := tx.Exec(ctx, q, row.ID, row.Name, row.Description, row.Capacity, row.CreatedAt); err != nil {
			return 0, fmt.Errorf("row %d (%q): %w", i+1, row.Name, err)
		}
		inserted++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	return inserted, nil
}

func (r *resourceRepo) SumActivePartySizesInWindow(ctx context.Context, resourceID uuid.UUID, start, end time.Time) (int, error) {
	const q = `
		SELECT COALESCE(SUM(party_size), 0)::int
		FROM bookings
		WHERE resource_id = $1
		  AND status IN ('pending_confirmation','waitlisted','checked_in')
		  AND start_time < $3 AND end_time > $2
	`
	var total int
	if err := r.pool.QueryRow(ctx, q, resourceID, start, end).Scan(&total); err != nil {
		return 0, fmt.Errorf("sum party sizes: %w", err)
	}
	return total, nil
}
