package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/repository"
)

// AvailabilityWindow represents a single free slot returned by an availability
// query. The UI can render these directly.
type AvailabilityWindow struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

// AvailabilityResult bundles the resource, the bookings already on its
// schedule for the requested day, and the derived free windows.
type AvailabilityResult struct {
	Resource Resource             `json:"resource"`
	Date     string               `json:"date"`
	Booked   []domain.Booking     `json:"booked"`
	Free     []AvailabilityWindow `json:"free"`
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

// Availability returns booked + free windows for a resource on a given date.
// The "open day" is interpreted as 08:00–22:00 local time of the supplied date.
func (s *ResourceService) Availability(ctx context.Context, resourceID uuid.UUID, day time.Time) (*AvailabilityResult, error) {
	res, err := s.resources.Get(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	booked, err := s.bookings.ListByResourceOnDate(ctx, resourceID, day)
	if err != nil {
		return nil, err
	}
	open := time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, day.Location())
	close := time.Date(day.Year(), day.Month(), day.Day(), 22, 0, 0, 0, day.Location())

	free := ComputeFreeWindows(open, close, booked)

	return &AvailabilityResult{
		Resource: *res,
		Date:     day.Format("2006-01-02"),
		Booked:   booked,
		Free:     free,
	}, nil
}

// ComputeFreeWindows subtracts the union of booking intervals from [open,close).
func ComputeFreeWindows(open, close time.Time, booked []domain.Booking) []AvailabilityWindow {
	if !close.After(open) {
		return nil
	}
	cursor := open
	out := make([]AvailabilityWindow, 0)
	for _, b := range booked {
		bs := b.StartTime
		if bs.Before(open) {
			bs = open
		}
		be := b.EndTime
		if be.After(close) {
			be = close
		}
		if !be.After(bs) {
			continue
		}
		if bs.After(cursor) {
			out = append(out, AvailabilityWindow{StartTime: cursor, EndTime: bs})
		}
		if be.After(cursor) {
			cursor = be
		}
	}
	if cursor.Before(close) {
		out = append(out, AvailabilityWindow{StartTime: cursor, EndTime: close})
	}
	return out
}
