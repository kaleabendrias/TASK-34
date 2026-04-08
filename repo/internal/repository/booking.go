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

// BookingRepository defines the persistence contract for bookings.
type BookingRepository interface {
	Create(ctx context.Context, b *domain.Booking) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Booking, error)
	List(ctx context.Context, limit, offset int) ([]domain.Booking, error)
	ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Booking, error)
	ListByGroup(ctx context.Context, groupID uuid.UUID) ([]domain.Booking, error)
	ListByResourceOnDate(ctx context.Context, resourceID uuid.UUID, day time.Time) ([]domain.Booking, error)
	CountActiveByUserOnDate(ctx context.Context, userID uuid.UUID, day time.Time) (int, error)
	UserHasOverlap(ctx context.Context, userID uuid.UUID, start, end time.Time, exclude *uuid.UUID) (bool, error)
	ResourceHasOverlap(ctx context.Context, resourceID uuid.UUID, start, end time.Time, exclude *uuid.UUID) (bool, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.BookingStatus) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type bookingRepo struct {
	pool *pgxpool.Pool
}

func NewBookingRepository(pool *pgxpool.Pool) BookingRepository {
	return &bookingRepo{pool: pool}
}

const bookingColumns = `
	id, user_id, resource_id, group_id, party_size,
	start_time, end_time, status, notes, secure_notes, created_at, updated_at
`

func (r *bookingRepo) Create(ctx context.Context, b *domain.Booking) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	now := time.Now().UTC()
	b.CreatedAt = now
	b.UpdatedAt = now
	if b.PartySize <= 0 {
		b.PartySize = 1
	}

	const q = `
		INSERT INTO bookings (id, user_id, resource_id, group_id, party_size,
		                      start_time, end_time, status, notes, secure_notes, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`
	_, err := r.pool.Exec(ctx, q,
		b.ID, b.UserID, b.ResourceID, b.GroupID, b.PartySize,
		b.StartTime, b.EndTime, string(b.Status), b.Notes, b.SecureNotes, b.CreatedAt, b.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert booking: %w", err)
	}
	return nil
}

func (r *bookingRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Booking, error) {
	q := `SELECT ` + bookingColumns + ` FROM bookings WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	b, err := scanBooking(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get booking: %w", err)
	}
	return b, nil
}

func (r *bookingRepo) List(ctx context.Context, limit, offset int) ([]domain.Booking, error) {
	limit, offset = sanitisePaging(limit, offset)
	q := `SELECT ` + bookingColumns + ` FROM bookings ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list bookings: %w", err)
	}
	defer rows.Close()
	return collectBookings(rows)
}

func (r *bookingRepo) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Booking, error) {
	limit, offset = sanitisePaging(limit, offset)
	q := `SELECT ` + bookingColumns + ` FROM bookings WHERE user_id = $1 ORDER BY start_time DESC LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list user bookings: %w", err)
	}
	defer rows.Close()
	return collectBookings(rows)
}

func (r *bookingRepo) ListByGroup(ctx context.Context, groupID uuid.UUID) ([]domain.Booking, error) {
	q := `SELECT ` + bookingColumns + ` FROM bookings WHERE group_id = $1 ORDER BY start_time ASC`
	rows, err := r.pool.Query(ctx, q, groupID)
	if err != nil {
		return nil, fmt.Errorf("list group bookings: %w", err)
	}
	defer rows.Close()
	return collectBookings(rows)
}

func (r *bookingRepo) ListByResourceOnDate(ctx context.Context, resourceID uuid.UUID, day time.Time) ([]domain.Booking, error) {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	dayEnd := dayStart.Add(24 * time.Hour)
	q := `SELECT ` + bookingColumns + `
		FROM bookings
		WHERE resource_id = $1
		  AND status <> 'canceled'
		  AND start_time < $3 AND end_time > $2
		ORDER BY start_time ASC`
	rows, err := r.pool.Query(ctx, q, resourceID, dayStart, dayEnd)
	if err != nil {
		return nil, fmt.Errorf("list resource day bookings: %w", err)
	}
	defer rows.Close()
	return collectBookings(rows)
}

func (r *bookingRepo) CountActiveByUserOnDate(ctx context.Context, userID uuid.UUID, day time.Time) (int, error) {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	dayEnd := dayStart.Add(24 * time.Hour)
	const q = `
		SELECT COUNT(*) FROM bookings
		WHERE user_id = $1
		  AND status IN ('pending_confirmation','waitlisted','checked_in')
		  AND start_time >= $2 AND start_time < $3
	`
	var count int
	if err := r.pool.QueryRow(ctx, q, userID, dayStart, dayEnd).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active bookings: %w", err)
	}
	return count, nil
}

func (r *bookingRepo) UserHasOverlap(ctx context.Context, userID uuid.UUID, start, end time.Time, exclude *uuid.UUID) (bool, error) {
	q := `
		SELECT EXISTS (
			SELECT 1 FROM bookings
			WHERE user_id = $1
			  AND status <> 'canceled'
			  AND start_time < $3 AND end_time > $2
			  AND ($4::uuid IS NULL OR id <> $4)
		)
	`
	var exists bool
	if err := r.pool.QueryRow(ctx, q, userID, start, end, exclude).Scan(&exists); err != nil {
		return false, fmt.Errorf("user overlap check: %w", err)
	}
	return exists, nil
}

func (r *bookingRepo) ResourceHasOverlap(ctx context.Context, resourceID uuid.UUID, start, end time.Time, exclude *uuid.UUID) (bool, error) {
	q := `
		SELECT EXISTS (
			SELECT 1 FROM bookings
			WHERE resource_id = $1
			  AND status IN ('pending_confirmation','checked_in')
			  AND start_time < $3 AND end_time > $2
			  AND ($4::uuid IS NULL OR id <> $4)
		)
	`
	var exists bool
	if err := r.pool.QueryRow(ctx, q, resourceID, start, end, exclude).Scan(&exists); err != nil {
		return false, fmt.Errorf("resource overlap check: %w", err)
	}
	return exists, nil
}

func (r *bookingRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.BookingStatus) error {
	const q = `UPDATE bookings SET status = $2, updated_at = NOW() WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, id, string(status))
	if err != nil {
		return fmt.Errorf("update booking status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *bookingRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM bookings WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete booking: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// --- helpers ---

type rowScanner interface {
	Scan(dest ...any) error
}

func sanitisePaging(limit, offset int) (int, int) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func scanBooking(s rowScanner) (*domain.Booking, error) {
	var (
		b           domain.Booking
		groupID     *uuid.UUID
		status      string
		secureNotes []byte
	)
	if err := s.Scan(
		&b.ID, &b.UserID, &b.ResourceID, &groupID, &b.PartySize,
		&b.StartTime, &b.EndTime, &status, &b.Notes, &secureNotes, &b.CreatedAt, &b.UpdatedAt,
	); err != nil {
		return nil, err
	}
	b.GroupID = groupID
	b.Status = domain.BookingStatus(status)
	b.SecureNotes = secureNotes
	return &b, nil
}

func collectBookings(rows pgx.Rows) ([]domain.Booking, error) {
	out := make([]domain.Booking, 0, 16)
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
