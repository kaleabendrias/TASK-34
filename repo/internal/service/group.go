package service

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/repository"
)

// GroupService coordinates group reservation aggregates and reads their child
// bookings. Adding bookings to a group is done via BookingService.Create with
// a non-nil GroupID, so all booking-policy rules apply uniformly.
type GroupService struct {
	groups   repository.GroupRepository
	bookings repository.BookingRepository
	log      *slog.Logger
}

func NewGroupService(groups repository.GroupRepository, bookings repository.BookingRepository, log *slog.Logger) *GroupService {
	return &GroupService{groups: groups, bookings: bookings, log: log}
}

func (s *GroupService) Create(ctx context.Context, g *domain.GroupReservation) (*domain.GroupReservation, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	if err := s.groups.Create(ctx, g); err != nil {
		return nil, err
	}
	s.log.Info("group created", "id", g.ID, "name", g.Name)
	return g, nil
}

func (s *GroupService) Get(ctx context.Context, id uuid.UUID) (*domain.GroupReservation, error) {
	g, err := s.groups.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	bookings, err := s.bookings.ListByGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	g.Bookings = bookings
	return g, nil
}

func (s *GroupService) List(ctx context.Context, limit, offset int) ([]domain.GroupReservation, error) {
	return s.groups.List(ctx, limit, offset)
}

func (s *GroupService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.groups.Delete(ctx, id)
}
