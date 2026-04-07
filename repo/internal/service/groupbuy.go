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

// GroupBuyDefaults are the policy-mandated defaults for new group buys.
var GroupBuyDefaults = struct {
	Threshold int
	Deadline  time.Duration
}{
	Threshold: 5,
	Deadline:  24 * time.Hour,
}

type GroupBuyService struct {
	repo          repository.GroupBuyRepository
	resources     repository.ResourceRepository
	users         repository.UserRepository
	notifications repository.NotificationRepository
	log           *slog.Logger
}

func NewGroupBuyService(
	repo repository.GroupBuyRepository,
	resources repository.ResourceRepository,
	users repository.UserRepository,
	notifications repository.NotificationRepository,
	log *slog.Logger,
) *GroupBuyService {
	return &GroupBuyService{repo: repo, resources: resources, users: users, notifications: notifications, log: log}
}

type CreateGroupBuyInput struct {
	OrganizerID uuid.UUID
	ResourceID  uuid.UUID
	Title       string
	Description string
	Threshold   int       // 0 → default 5
	Capacity    int
	StartsAt    time.Time
	EndsAt      time.Time
	Deadline    *time.Time // nil → starts_at - 1ns or now+24h, whichever is earlier
}

// Create persists a new group buy with policy defaults filled in.
func (s *GroupBuyService) Create(ctx context.Context, in CreateGroupBuyInput) (*domain.GroupBuy, error) {
	now := time.Now().UTC()

	if user, err := s.users.GetByID(ctx, in.OrganizerID); err != nil {
		return nil, err
	} else if user.IsBlacklisted {
		return nil, errors.Join(domain.ErrBlacklisted, errors.New(user.BlacklistReason))
	}

	if _, err := s.resources.Get(ctx, in.ResourceID); err != nil {
		return nil, err
	}

	threshold := in.Threshold
	if threshold <= 0 {
		threshold = GroupBuyDefaults.Threshold
	}

	deadline := time.Time{}
	if in.Deadline != nil {
		deadline = in.Deadline.UTC()
	} else {
		deadline = now.Add(GroupBuyDefaults.Deadline)
		if deadline.After(in.StartsAt) {
			deadline = in.StartsAt
		}
	}

	g := &domain.GroupBuy{
		ResourceID:  in.ResourceID,
		OrganizerID: in.OrganizerID,
		Title:       in.Title,
		Description: in.Description,
		Threshold:   threshold,
		Capacity:    in.Capacity,
		StartsAt:    in.StartsAt.UTC(),
		EndsAt:      in.EndsAt.UTC(),
		Deadline:    deadline,
		Status:      domain.GroupBuyOpen,
	}
	if err := g.Validate(now); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, g); err != nil {
		return nil, err
	}
	s.log.Info("group buy created", "id", g.ID, "threshold", g.Threshold, "deadline", g.Deadline)
	return g, nil
}

// Join adds a participant. Oversell is impossible because the underlying
// repository performs an atomic optimistic-locked decrement and rejects
// races. Idempotency is enforced one layer up via the idempotency middleware.
func (s *GroupBuyService) Join(ctx context.Context, gbID, userID uuid.UUID, qty int) (*domain.GroupBuy, *domain.GroupBuyParticipant, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if user.IsBlacklisted {
		return nil, nil, errors.Join(domain.ErrBlacklisted, errors.New(user.BlacklistReason))
	}

	const maxRetries = 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		gb, part, err := s.repo.JoinAtomic(ctx, gbID, userID, qty)
		if err == nil {
			s.notifyOrganizerProgress(ctx, gb)
			return gb, part, nil
		}
		if errors.Is(err, domain.ErrOptimisticLock) && attempt < maxRetries {
			s.log.Info("optimistic lock retry", "group_buy", gbID, "attempt", attempt)
			continue
		}
		return nil, nil, err
	}
	return nil, nil, fmt.Errorf("join group buy: exhausted retries")
}

func (s *GroupBuyService) Get(ctx context.Context, id uuid.UUID) (*domain.GroupBuy, error) {
	return s.repo.Get(ctx, id)
}

func (s *GroupBuyService) Participants(ctx context.Context, id uuid.UUID) ([]domain.GroupBuyParticipant, error) {
	return s.repo.ListParticipants(ctx, id)
}

func (s *GroupBuyService) List(ctx context.Context, limit, offset int) ([]domain.GroupBuy, error) {
	return s.repo.List(ctx, limit, offset)
}

// Progress is the structured payload returned to the UI for real-time
// rendering. It is intentionally cheap to compute so it can be polled.
type Progress struct {
	GroupBuyID     uuid.UUID `json:"group_buy_id"`
	Threshold      int       `json:"threshold"`
	Confirmed      int       `json:"confirmed"`
	RemainingSlots int       `json:"remaining_slots"`
	Capacity       int       `json:"capacity"`
	Ratio          float64   `json:"ratio"`
	ThresholdMet   bool      `json:"threshold_met"`
	Deadline       time.Time `json:"deadline"`
	SecondsLeft    int64     `json:"seconds_left"`
	Status         string    `json:"status"`
}

func (s *GroupBuyService) Progress(ctx context.Context, id uuid.UUID) (*Progress, error) {
	g, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	left := int64(g.Deadline.Sub(now).Seconds())
	if left < 0 {
		left = 0
	}
	return &Progress{
		GroupBuyID:     g.ID,
		Threshold:      g.Threshold,
		Confirmed:      g.Confirmed(),
		RemainingSlots: g.RemainingSlots,
		Capacity:       g.Capacity,
		Ratio:          g.Progress(),
		ThresholdMet:   g.ThresholdMet(),
		Deadline:       g.Deadline,
		SecondsLeft:    left,
		Status:         string(g.Status),
	}, nil
}

// SweepExpired is a job step: any open or met group buy whose deadline has
// passed gets a terminal state assigned (finalized if threshold met, expired
// otherwise) and the organizer is notified.
func (s *GroupBuyService) SweepExpired(ctx context.Context) error {
	now := time.Now().UTC()
	expiring, err := s.repo.ListExpiringBefore(ctx, now)
	if err != nil {
		return err
	}
	for i := range expiring {
		g := &expiring[i]
		target := domain.GroupBuyExpired
		if g.ThresholdMet() {
			target = domain.GroupBuyFinalized
		}
		if err := s.repo.UpdateStatus(ctx, g.ID, target); err != nil {
			s.log.Warn("sweep update failed", "id", g.ID, "error", err)
			continue
		}
		_ = s.notifications.CreateNotification(ctx, &domain.Notification{
			UserID: g.OrganizerID,
			Kind:   "group_buy_" + string(target),
			Title:  fmt.Sprintf("Group buy %q %s", g.Title, target),
			Body:   fmt.Sprintf("%d / %d confirmed at deadline.", g.Confirmed(), g.Threshold),
		})
	}
	return nil
}

func (s *GroupBuyService) notifyOrganizerProgress(ctx context.Context, g *domain.GroupBuy) {
	if !g.ThresholdMet() {
		return
	}
	if g.Status != domain.GroupBuyMet {
		return
	}
	_ = s.notifications.CreateNotification(ctx, &domain.Notification{
		UserID: g.OrganizerID,
		Kind:   "group_buy_threshold_met",
		Title:  fmt.Sprintf("Group buy %q reached its threshold", g.Title),
		Body:   fmt.Sprintf("%d / %d seats confirmed.", g.Confirmed(), g.Threshold),
	})
}
