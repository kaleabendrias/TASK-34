package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harborworks/booking-hub/internal/domain"
)

type ResourceRepository interface {
	List(ctx context.Context) ([]domain.Resource, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Resource, error)
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
