package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/repository"
)

// BookingPolicy collects the time-based business rules. Centralising them
// keeps the constants visible to handlers and tests.
type BookingPolicy struct {
	MinLeadTime    time.Duration // 2h: earliest start time relative to now for a new booking
	ChangeCutoff   time.Duration // 10m: changes/cancellations forbidden inside this window
	MaxActivePerDay int           // 3: per-user limit on active bookings on the same calendar day
}

func DefaultBookingPolicy() BookingPolicy {
	return BookingPolicy{
		MinLeadTime:     2 * time.Hour,
		ChangeCutoff:    10 * time.Minute,
		MaxActivePerDay: 3,
	}
}

type BookingService struct {
	bookings  repository.BookingRepository
	resources repository.ResourceRepository
	users     repository.UserRepository
	policy    BookingPolicy
	log       *slog.Logger
}

func NewBookingService(
	bookings repository.BookingRepository,
	resources repository.ResourceRepository,
	users repository.UserRepository,
	log *slog.Logger,
	policy BookingPolicy,
) *BookingService {
	return &BookingService{bookings: bookings, resources: resources, users: users, log: log, policy: policy}
}

func (s *BookingService) Policy() BookingPolicy { return s.policy }

// CreateInput captures the parameters of a new booking request.
type CreateInput struct {
	UserID     uuid.UUID
	ResourceID uuid.UUID
	GroupID    *uuid.UUID
	PartySize  int
	StartTime  time.Time
	EndTime    time.Time
	Notes      string
}

// Create applies the full booking policy and persists the result. Bookings
// that conflict with another active booking on the same resource are placed
// on the waitlist instead of being rejected.
func (s *BookingService) Create(ctx context.Context, in CreateInput) (*domain.Booking, error) {
	// 0. Blacklist gate. Hard block.
	user, err := s.users.GetByID(ctx, in.UserID)
	if err != nil {
		return nil, err
	}
	if user.IsBlacklisted {
		return nil, errors.Join(domain.ErrBlacklisted, errors.New(user.BlacklistReason))
	}

	// 1. Resource must exist.
	if _, err := s.resources.Get(ctx, in.ResourceID); err != nil {
		return nil, err
	}

	// 2. Shape validation.
	now := time.Now().UTC()
	in.StartTime = in.StartTime.UTC()
	in.EndTime = in.EndTime.UTC()
	if !in.EndTime.After(in.StartTime) {
		return nil, errors.Join(domain.ErrInvalidInput, errors.New("end_time must be after start_time"))
	}
	if in.PartySize <= 0 {
		in.PartySize = 1
	}

	// 3. Lead-time gate (>= 2h from now).
	if in.StartTime.Before(now.Add(s.policy.MinLeadTime)) {
		return nil, errors.Join(domain.ErrLeadTime,
			fmt.Errorf("start_time must be at least %s in the future", s.policy.MinLeadTime))
	}

	// 4. Daily active-booking cap (3 per user per calendar day).
	count, err := s.bookings.CountActiveByUserOnDate(ctx, in.UserID, in.StartTime)
	if err != nil {
		return nil, err
	}
	if count >= s.policy.MaxActivePerDay {
		return nil, errors.Join(domain.ErrDailyLimit,
			fmt.Errorf("user already has %d active bookings on %s", count, in.StartTime.Format("2006-01-02")))
	}

	// 5. Per-user overlap (no double-booking the same window across resources).
	overlap, err := s.bookings.UserHasOverlap(ctx, in.UserID, in.StartTime, in.EndTime, nil)
	if err != nil {
		return nil, err
	}
	if overlap {
		return nil, errors.Join(domain.ErrOverlap, errors.New("you already have a booking in this window"))
	}

	// 6. Resource conflict ⇒ waitlist instead of reject.
	resourceBusy, err := s.bookings.ResourceHasOverlap(ctx, in.ResourceID, in.StartTime, in.EndTime, nil)
	if err != nil {
		return nil, err
	}
	status := domain.StatusPendingConfirmation
	if resourceBusy {
		status = domain.StatusWaitlisted
	}

	b := &domain.Booking{
		UserID:     in.UserID,
		ResourceID: in.ResourceID,
		GroupID:    in.GroupID,
		PartySize:  in.PartySize,
		StartTime:  in.StartTime,
		EndTime:    in.EndTime,
		Status:     status,
		Notes:      in.Notes,
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	if err := s.bookings.Create(ctx, b); err != nil {
		return nil, err
	}
	s.log.Info("booking created",
		"id", b.ID, "user_id", b.UserID, "resource_id", b.ResourceID,
		"status", b.Status, "start", b.StartTime, "end", b.EndTime)
	return b, nil
}

func (s *BookingService) Get(ctx context.Context, id uuid.UUID) (*domain.Booking, error) {
	return s.bookings.Get(ctx, id)
}

func (s *BookingService) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Booking, error) {
	return s.bookings.ListByUser(ctx, userID, limit, offset)
}

func (s *BookingService) List(ctx context.Context, limit, offset int) ([]domain.Booking, error) {
	return s.bookings.List(ctx, limit, offset)
}

// Transition moves a booking to a new state, enforcing both the state machine
// and the time-based cutoff window. The actor (user) must own the booking.
func (s *BookingService) Transition(ctx context.Context, actorID uuid.UUID, bookingID uuid.UUID, target domain.BookingStatus) (*domain.Booking, error) {
	b, err := s.bookings.Get(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if b.UserID != actorID {
		return nil, domain.ErrForbidden
	}
	if !domain.CanTransition(b.Status, target) {
		return nil, errors.Join(domain.ErrInvalidTransition,
			fmt.Errorf("cannot move %s -> %s", b.Status, target))
	}

	// Cutoff: cancellations and check-ins are blocked inside the cutoff window
	// (less than 10 minutes before start). Auto-completing a checked-in
	// booking is allowed because it represents the natural end of stay.
	now := time.Now().UTC()
	timeToStart := b.StartTime.Sub(now)
	cutoffActive := timeToStart > 0 && timeToStart < s.policy.ChangeCutoff
	if cutoffActive && (target == domain.StatusCanceled || target == domain.StatusWaitlisted || target == domain.StatusPendingConfirmation) {
		return nil, errors.Join(domain.ErrCutoff,
			fmt.Errorf("changes are locked %s before start_time", s.policy.ChangeCutoff))
	}

	// When promoting waitlisted -> pending_confirmation, the resource must be free again.
	if b.Status == domain.StatusWaitlisted && target == domain.StatusPendingConfirmation {
		busy, err := s.bookings.ResourceHasOverlap(ctx, b.ResourceID, b.StartTime, b.EndTime, &b.ID)
		if err != nil {
			return nil, err
		}
		if busy {
			return nil, errors.Join(domain.ErrConflict, errors.New("resource is still occupied"))
		}
	}

	if err := s.bookings.UpdateStatus(ctx, bookingID, target); err != nil {
		return nil, err
	}
	b.Status = target
	b.UpdatedAt = now
	s.log.Info("booking transition", "id", b.ID, "to", target, "actor", actorID)
	return b, nil
}
