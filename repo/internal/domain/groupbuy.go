package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// GroupBuyStatus is the lifecycle of a group-buy.
type GroupBuyStatus string

const (
	GroupBuyOpen      GroupBuyStatus = "open"
	GroupBuyMet       GroupBuyStatus = "met"
	GroupBuyExpired   GroupBuyStatus = "expired"
	GroupBuyCanceled  GroupBuyStatus = "canceled"
	GroupBuyFinalized GroupBuyStatus = "finalized"
	// GroupBuyFailed is the terminal state assigned when a campaign reaches
	// its deadline with at least one participant but without meeting the
	// threshold. The sweep job releases the pre-allocated resource slots
	// (resets remaining_slots back to capacity) so the underlying resource
	// can be re-used.
	GroupBuyFailed GroupBuyStatus = "failed"
)

// IsTerminal reports whether the group buy can no longer accept joins.
func (s GroupBuyStatus) IsTerminal() bool {
	switch s {
	case GroupBuyExpired, GroupBuyCanceled, GroupBuyFinalized, GroupBuyFailed:
		return true
	}
	return false
}

// GroupBuy represents a session-anchored collective purchase. The threshold
// is the minimum number of confirmed seats required for the group buy to be
// considered "met". Capacity is the absolute maximum the underlying session
// can supply; remaining_slots is decremented atomically with optimistic
// locking on every join.
type GroupBuy struct {
	ID             uuid.UUID      `json:"id"`
	ResourceID     uuid.UUID      `json:"resource_id"`
	// OrganizerID is nullable: the FK uses ON DELETE SET NULL so a group buy
	// outlives the user that created it. Reads via scanGroupBuy must
	// therefore tolerate a NULL column value.
	OrganizerID    *uuid.UUID     `json:"organizer_id,omitempty"`
	Title          string         `json:"title"`
	Description    string         `json:"description"`
	Threshold      int            `json:"threshold"`
	Capacity       int            `json:"capacity"`
	RemainingSlots int            `json:"remaining_slots"`
	StartsAt       time.Time      `json:"starts_at"`
	EndsAt         time.Time      `json:"ends_at"`
	Deadline       time.Time      `json:"deadline"`
	Status         GroupBuyStatus `json:"status"`
	Version        int64          `json:"version"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// Confirmed counts the number of seats currently committed.
func (g *GroupBuy) Confirmed() int { return g.Capacity - g.RemainingSlots }

// Progress returns a 0..1 ratio of confirmed seats vs threshold.
func (g *GroupBuy) Progress() float64 {
	if g.Threshold == 0 {
		return 0
	}
	p := float64(g.Confirmed()) / float64(g.Threshold)
	if p > 1 {
		p = 1
	}
	return p
}

func (g *GroupBuy) ThresholdMet() bool { return g.Confirmed() >= g.Threshold }

// Validate checks the static invariants of a group buy.
func (g *GroupBuy) Validate(now time.Time) error {
	if g.Title == "" {
		return errors.Join(ErrInvalidInput, errors.New("title is required"))
	}
	if g.Threshold <= 0 {
		return errors.Join(ErrInvalidInput, errors.New("threshold must be > 0"))
	}
	if g.Capacity <= 0 {
		return errors.Join(ErrInvalidInput, errors.New("capacity must be > 0"))
	}
	if g.Threshold > g.Capacity {
		return errors.Join(ErrInvalidInput, errors.New("threshold cannot exceed capacity"))
	}
	if !g.EndsAt.After(g.StartsAt) {
		return errors.Join(ErrInvalidInput, errors.New("ends_at must be after starts_at"))
	}
	if !g.Deadline.After(now) {
		return errors.Join(ErrInvalidInput, errors.New("deadline must be in the future"))
	}
	if g.Deadline.After(g.StartsAt) {
		return errors.Join(ErrInvalidInput, errors.New("deadline must be on or before starts_at"))
	}
	return nil
}

// GroupBuyParticipant is one user's commitment to a group buy.
type GroupBuyParticipant struct {
	ID         uuid.UUID `json:"id"`
	GroupBuyID uuid.UUID `json:"group_buy_id"`
	UserID     uuid.UUID `json:"user_id"`
	Quantity   int       `json:"quantity"`
	JoinedAt   time.Time `json:"joined_at"`
}
