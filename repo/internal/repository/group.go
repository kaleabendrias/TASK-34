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

type GroupRepository interface {
	Create(ctx context.Context, g *domain.GroupReservation) error
	Get(ctx context.Context, id uuid.UUID) (*domain.GroupReservation, error)
	List(ctx context.Context, limit, offset int) ([]domain.GroupReservation, error)
	Update(ctx context.Context, g *domain.GroupReservation) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type groupRepo struct {
	pool *pgxpool.Pool
}

func NewGroupRepository(pool *pgxpool.Pool) GroupRepository {
	return &groupRepo{pool: pool}
}

func (r *groupRepo) Create(ctx context.Context, g *domain.GroupReservation) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	now := time.Now().UTC()
	g.CreatedAt = now
	g.UpdatedAt = now

	const q = `
		INSERT INTO group_reservations
		  (id, name, organizer_name, organizer_email, capacity, notes, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`
	if _, err := r.pool.Exec(ctx, q,
		g.ID, g.Name, g.OrganizerName, g.OrganizerEmail, g.Capacity, g.Notes, g.CreatedAt, g.UpdatedAt,
	); err != nil {
		return fmt.Errorf("insert group: %w", err)
	}
	return nil
}

func (r *groupRepo) Get(ctx context.Context, id uuid.UUID) (*domain.GroupReservation, error) {
	const q = `
		SELECT id, name, organizer_name, organizer_email, capacity, notes, created_at, updated_at
		FROM group_reservations WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, q, id)
	g, err := scanGroup(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get group: %w", err)
	}
	return g, nil
}

func (r *groupRepo) List(ctx context.Context, limit, offset int) ([]domain.GroupReservation, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	const q = `
		SELECT id, name, organizer_name, organizer_email, capacity, notes, created_at, updated_at
		FROM group_reservations ORDER BY created_at DESC LIMIT $1 OFFSET $2
	`
	rows, err := r.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	out := make([]domain.GroupReservation, 0, 16)
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

func (r *groupRepo) Update(ctx context.Context, g *domain.GroupReservation) error {
	g.UpdatedAt = time.Now().UTC()
	const q = `
		UPDATE group_reservations
		SET name=$2, organizer_name=$3, organizer_email=$4, capacity=$5, notes=$6, updated_at=$7
		WHERE id=$1
	`
	tag, err := r.pool.Exec(ctx, q,
		g.ID, g.Name, g.OrganizerName, g.OrganizerEmail, g.Capacity, g.Notes, g.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *groupRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM group_reservations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func scanGroup(s rowScanner) (*domain.GroupReservation, error) {
	var g domain.GroupReservation
	if err := s.Scan(
		&g.ID, &g.Name, &g.OrganizerName, &g.OrganizerEmail, &g.Capacity, &g.Notes, &g.CreatedAt, &g.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &g, nil
}
