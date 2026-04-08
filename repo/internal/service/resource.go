package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/repository"
)

// SlotCapacity is one row in the per-slot availability report. Capacity is
// the resource's seat budget; ActivePartySize is the sum of party_size of
// every active booking that overlaps the slot. RemainingSeats is the
// difference, clamped to zero.
type SlotCapacity struct {
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	Capacity        int       `json:"capacity"`
	ActivePartySize int       `json:"active_party_size"`
	RemainingSeats  int       `json:"remaining_seats"`
	Available       bool      `json:"available"`
}

// AvailabilityResult bundles the resource, the bookings already on its
// schedule for the requested day, and a per-slot capacity table.
type AvailabilityResult struct {
	Resource Resource         `json:"resource"`
	Date     string           `json:"date"`
	Booked   []domain.Booking `json:"booked"`
	Slots    []SlotCapacity   `json:"slots"`
	// Day-level summary so the UI can render a one-line headline next to
	// the resource without iterating Slots.
	TotalCapacity   int `json:"total_capacity"`
	PeakActiveParty int `json:"peak_active_party"`
	MinRemaining    int `json:"min_remaining"`
}

// Resource is exported here so handlers don't have to import domain just to
// shape the API response. It mirrors domain.Resource.
type Resource = domain.Resource

type ResourceService struct {
	resources repository.ResourceRepository
	bookings  repository.BookingRepository
	log       *slog.Logger
}

func NewResourceService(resources repository.ResourceRepository, bookings repository.BookingRepository, log *slog.Logger) *ResourceService {
	return &ResourceService{resources: resources, bookings: bookings, log: log}
}

func (s *ResourceService) List(ctx context.Context) ([]domain.Resource, error) {
	return s.resources.List(ctx)
}

func (s *ResourceService) Get(ctx context.Context, id uuid.UUID) (*domain.Resource, error) {
	return s.resources.Get(ctx, id)
}

// Day window: 08:00–22:00 UTC, sliced into hourly slots. The slice can be
// adjusted later by reading from a per-resource opening hours table.
const (
	openHourUTC  = 8
	closeHourUTC = 22
	slotMinutes  = 60
)

// Availability returns the per-slot capacity report for one resource on one
// day. For each hourly slot in [08:00,22:00) the response carries the
// resource's total seat budget, the sum of party sizes for every overlapping
// active booking, and the remaining seats.
func (s *ResourceService) Availability(ctx context.Context, resourceID uuid.UUID, day time.Time) (*AvailabilityResult, error) {
	res, err := s.resources.Get(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	booked, err := s.bookings.ListByResourceOnDate(ctx, resourceID, day)
	if err != nil {
		return nil, err
	}

	open := time.Date(day.Year(), day.Month(), day.Day(), openHourUTC, 0, 0, 0, time.UTC)
	close := time.Date(day.Year(), day.Month(), day.Day(), closeHourUTC, 0, 0, 0, time.UTC)

	slots := ComputeSlotCapacities(open, close, slotMinutes*time.Minute, res.Capacity, booked)

	peak := 0
	minRemaining := res.Capacity
	for _, sl := range slots {
		if sl.ActivePartySize > peak {
			peak = sl.ActivePartySize
		}
		if sl.RemainingSeats < minRemaining {
			minRemaining = sl.RemainingSeats
		}
	}
	if len(slots) == 0 {
		minRemaining = res.Capacity
	}

	return &AvailabilityResult{
		Resource:        *res,
		Date:            day.Format("2006-01-02"),
		Booked:          booked,
		Slots:           slots,
		TotalCapacity:   res.Capacity,
		PeakActiveParty: peak,
		MinRemaining:    minRemaining,
	}, nil
}

// RemainingSeats reports the remaining seats for a specific (resource,
// window) pair. The owner-side check (`is the proposed party size <= remain
// ing`) lives in BookingService.Create. Exposed via the API so the UI can
// quote a number before the user submits a booking form.
func (s *ResourceService) RemainingSeats(ctx context.Context, resourceID uuid.UUID, start, end time.Time) (capacity int, active int, remaining int, err error) {
	res, err := s.resources.Get(ctx, resourceID)
	if err != nil {
		return 0, 0, 0, err
	}
	active, err = s.resources.SumActivePartySizesInWindow(ctx, resourceID, start, end)
	if err != nil {
		return 0, 0, 0, err
	}
	remaining = res.Capacity - active
	if remaining < 0 {
		remaining = 0
	}
	return res.Capacity, active, remaining, nil
}

// ComputeSlotCapacities slices [open, close) into fixed-width slots and, for
// each slot, computes:
//   - capacity        = the resource's total seat budget
//   - active_party    = sum of party_size for bookings that overlap the slot
//                       and are in an active state
//   - remaining_seats = max(0, capacity - active_party)
//
// It is intentionally side-effect free so unit tests can exercise every
// branch (no overlap, partial overlap, full saturation, oversold edge).
func ComputeSlotCapacities(open, close time.Time, slot time.Duration, capacity int, booked []domain.Booking) []SlotCapacity {
	if !close.After(open) || slot <= 0 || capacity < 0 {
		return nil
	}
	out := make([]SlotCapacity, 0, int(close.Sub(open)/slot))
	for cur := open; cur.Before(close); cur = cur.Add(slot) {
		end := cur.Add(slot)
		if end.After(close) {
			end = close
		}
		active := 0
		for _, b := range booked {
			if !b.Status.IsActive() {
				continue
			}
			// Waitlisted bookings are "active" for per-user limits and the
			// state machine, but they must NOT consume seats here: they only
			// occupy capacity once they get promoted to pending_confirmation.
			if b.Status == domain.StatusWaitlisted {
				continue
			}
			if !b.StartTime.Before(end) || !b.EndTime.After(cur) {
				continue
			}
			active += b.PartySize
		}
		remaining := capacity - active
		if remaining < 0 {
			remaining = 0
		}
		out = append(out, SlotCapacity{
			StartTime:       cur,
			EndTime:         end,
			Capacity:        capacity,
			ActivePartySize: active,
			RemainingSeats:  remaining,
			Available:       remaining > 0,
		})
	}
	return out
}

// ErrSlotFull is returned by callers that need a sentinel for "no remaining
// seats" — kept here so the booking service can errors.Is against it. It is
// joined with domain.ErrCapacityExceed to interoperate with writeServiceError.
var ErrSlotFull = errors.New("no remaining seats for this resource window")
