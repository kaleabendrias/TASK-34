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

type GroupBuyRepository interface {
	Create(ctx context.Context, g *domain.GroupBuy) error
	Get(ctx context.Context, id uuid.UUID) (*domain.GroupBuy, error)
	List(ctx context.Context, limit, offset int) ([]domain.GroupBuy, error)
	// JoinAtomic decrements remaining_slots and inserts a participant in a
	// single transaction with optimistic locking. Returns the updated group
	// buy and the participant row, or ErrOversold / ErrOptimisticLock /
	// ErrAlreadyJoined as appropriate.
	JoinAtomic(ctx context.Context, gbID, userID uuid.UUID, qty int) (*domain.GroupBuy, *domain.GroupBuyParticipant, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.GroupBuyStatus) error
	// MarkFailedAndReleaseSlots atomically sets the group buy to a failed
	// terminal state AND resets remaining_slots back to capacity, so any
	// pre-allocated seats are returned to the underlying resource.
	MarkFailedAndReleaseSlots(ctx context.Context, id uuid.UUID) error
	ListParticipants(ctx context.Context, gbID uuid.UUID) ([]domain.GroupBuyParticipant, error)
	ListExpiringBefore(ctx context.Context, t time.Time) ([]domain.GroupBuy, error)
}

type groupBuyRepo struct{ pool *pgxpool.Pool }

func NewGroupBuyRepository(pool *pgxpool.Pool) GroupBuyRepository {
	return &groupBuyRepo{pool: pool}
}

const groupBuyColumns = `
	id, resource_id, organizer_id, title, description,
	threshold, capacity, remaining_slots, starts_at, ends_at, deadline,
	status, version, created_at, updated_at
`

func (r *groupBuyRepo) Create(ctx context.Context, g *domain.GroupBuy) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	now := time.Now().UTC()
	g.CreatedAt = now
	g.UpdatedAt = now
	g.RemainingSlots = g.Capacity
	g.Version = 0
	if g.Status == "" {
		g.Status = domain.GroupBuyOpen
	}

	const q = `
		INSERT INTO group_buys (id, resource_id, organizer_id, title, description,
		                        threshold, capacity, remaining_slots, starts_at, ends_at, deadline,
		                        status, version, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
	`
	var organizer uuid.NullUUID
	if g.OrganizerID != nil {
		organizer = uuid.NullUUID{UUID: *g.OrganizerID, Valid: true}
	}
	_, err := r.pool.Exec(ctx, q,
		g.ID, g.ResourceID, organizer, g.Title, g.Description,
		g.Threshold, g.Capacity, g.RemainingSlots, g.StartsAt, g.EndsAt, g.Deadline,
		string(g.Status), g.Version, g.CreatedAt, g.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert group_buy: %w", err)
	}
	return nil
}

func (r *groupBuyRepo) Get(ctx context.Context, id uuid.UUID) (*domain.GroupBuy, error) {
	q := `SELECT ` + groupBuyColumns + ` FROM group_buys WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	g, err := scanGroupBuy(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get group_buy: %w", err)
	}
	return g, nil
}

func (r *groupBuyRepo) List(ctx context.Context, limit, offset int) ([]domain.GroupBuy, error) {
	limit, offset = sanitisePaging(limit, offset)
	q := `SELECT ` + groupBuyColumns + ` FROM group_buys ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list group_buys: %w", err)
	}
	defer rows.Close()
	out := make([]domain.GroupBuy, 0, 16)
	for rows.Next() {
		g, err := scanGroupBuy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

func (r *groupBuyRepo) JoinAtomic(ctx context.Context, gbID, userID uuid.UUID, qty int) (*domain.GroupBuy, *domain.GroupBuyParticipant, error) {
	if qty <= 0 {
		qty = 1
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Read current state.
	row := tx.QueryRow(ctx, `SELECT `+groupBuyColumns+` FROM group_buys WHERE id = $1`, gbID)
	g, err := scanGroupBuy(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, domain.ErrNotFound
		}
		return nil, nil, err
	}
	if g.Status != domain.GroupBuyOpen && g.Status != domain.GroupBuyMet {
		return nil, nil, errors.Join(domain.ErrConflict, fmt.Errorf("group buy is %s", g.Status))
	}
	now := time.Now().UTC()
	if !g.Deadline.After(now) {
		return nil, nil, domain.ErrDeadlinePassed
	}
	if g.RemainingSlots < qty {
		return nil, nil, domain.ErrOversold
	}

	// Optimistic-locked decrement: only succeeds if version unchanged.
	const upd = `
		UPDATE group_buys
		SET remaining_slots = remaining_slots - $2,
		    version         = version + 1,
		    status          = CASE
		        WHEN (capacity - (remaining_slots - $2)) >= threshold THEN 'met'
		        ELSE status
		    END,
		    updated_at = NOW()
		WHERE id = $1 AND version = $3 AND remaining_slots >= $2
		RETURNING ` + groupBuyColumns
	row = tx.QueryRow(ctx, upd, gbID, qty, g.Version)
	updated, err := scanGroupBuy(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, domain.ErrOptimisticLock
		}
		return nil, nil, fmt.Errorf("decrement remaining_slots: %w", err)
	}

	// Insert participant. Unique (group_buy_id, user_id) prevents double join.
	part := &domain.GroupBuyParticipant{
		ID:         uuid.New(),
		GroupBuyID: gbID,
		UserID:     userID,
		Quantity:   qty,
		JoinedAt:   now,
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO group_buy_participants (id, group_buy_id, user_id, quantity, joined_at)
		VALUES ($1,$2,$3,$4,$5)
	`, part.ID, part.GroupBuyID, part.UserID, part.Quantity, part.JoinedAt)
	if err != nil {
		// 23505 = unique_violation in pg
		if isUniqueViolation(err) {
			return nil, nil, domain.ErrAlreadyJoined
		}
		return nil, nil, fmt.Errorf("insert participant: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return updated, part, nil
}

func (r *groupBuyRepo) MarkFailedAndReleaseSlots(ctx context.Context, id uuid.UUID) error {
	// One transaction so the status flip and the slot release commit
	// together. Without the BEGIN/COMMIT a crash between the two updates
	// would leave the row "failed" but with seats still consumed.
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin failed-release tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const upd = `
		UPDATE group_buys
		   SET status          = 'failed',
		       remaining_slots = capacity,
		       version         = version + 1,
		       updated_at      = NOW()
		 WHERE id = $1
		   AND status IN ('open','met')
	`
	tag, err := tx.Exec(ctx, upd, id)
	if err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return tx.Commit(ctx)
}

func (r *groupBuyRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.GroupBuyStatus) error {
	tag, err := r.pool.Exec(ctx, `UPDATE group_buys SET status = $2, updated_at = NOW() WHERE id = $1`, id, string(status))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *groupBuyRepo) ListParticipants(ctx context.Context, gbID uuid.UUID) ([]domain.GroupBuyParticipant, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, group_buy_id, user_id, quantity, joined_at
		FROM group_buy_participants WHERE group_buy_id = $1 ORDER BY joined_at
	`, gbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.GroupBuyParticipant, 0)
	for rows.Next() {
		var p domain.GroupBuyParticipant
		if err := rows.Scan(&p.ID, &p.GroupBuyID, &p.UserID, &p.Quantity, &p.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *groupBuyRepo) ListExpiringBefore(ctx context.Context, t time.Time) ([]domain.GroupBuy, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+groupBuyColumns+` FROM group_buys WHERE deadline <= $1 AND status IN ('open','met')
	`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.GroupBuy, 0)
	for rows.Next() {
		g, err := scanGroupBuy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

func scanGroupBuy(s rowScanner) (*domain.GroupBuy, error) {
	var (
		g      domain.GroupBuy
		status string
	)
	// organizer_id is nullable in the schema (ON DELETE SET NULL), so scan
	// into a pgx-friendly nullable UUID and copy onto the *uuid.UUID field.
	var organizer uuid.NullUUID
	if err := s.Scan(
		&g.ID, &g.ResourceID, &organizer, &g.Title, &g.Description,
		&g.Threshold, &g.Capacity, &g.RemainingSlots, &g.StartsAt, &g.EndsAt, &g.Deadline,
		&status, &g.Version, &g.CreatedAt, &g.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if organizer.Valid {
		id := organizer.UUID
		g.OrganizerID = &id
	}
	g.Status = domain.GroupBuyStatus(status)
	return &g, nil
}

func isUniqueViolation(err error) bool {
	// pgx surfaces SQLSTATE via *pgconn.PgError. Use a tiny inline check to
	// avoid pulling pgconn into every repo file.
	type sqlState interface{ SQLState() string }
	for cur := err; cur != nil; {
		if s, ok := cur.(sqlState); ok && s.SQLState() == "23505" {
			return true
		}
		// Unwrap manually
		type unwrap interface{ Unwrap() error }
		u, ok := cur.(unwrap)
		if !ok {
			return false
		}
		cur = u.Unwrap()
	}
	return false
}
