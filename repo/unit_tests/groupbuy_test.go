package unit_tests

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
)

func makeGoodGroupBuy(now time.Time) *domain.GroupBuy {
	organizer := uuid.New()
	return &domain.GroupBuy{
		ID:             uuid.New(),
		ResourceID:     uuid.New(),
		OrganizerID:    &organizer,
		Title:          "Sunset cruise",
		Threshold:      5,
		Capacity:       10,
		RemainingSlots: 10,
		StartsAt:       now.Add(2 * time.Hour),
		EndsAt:         now.Add(4 * time.Hour),
		Deadline:       now.Add(time.Hour),
		Status:         domain.GroupBuyOpen,
	}
}

func TestGroupBuyConfirmedAndProgress(t *testing.T) {
	now := time.Now().UTC()
	g := makeGoodGroupBuy(now)
	if g.Confirmed() != 0 {
		t.Fatalf("confirmed should be 0")
	}
	if g.ThresholdMet() {
		t.Fatal("not met yet")
	}
	if g.Progress() != 0 {
		t.Fatalf("progress = %v", g.Progress())
	}

	g.RemainingSlots = 7 // 3 confirmed
	if g.Confirmed() != 3 {
		t.Fatalf("confirmed = %d", g.Confirmed())
	}
	if got := g.Progress(); got <= 0.59 || got >= 0.61 {
		t.Fatalf("progress ≈ 0.6, got %v", got)
	}

	g.RemainingSlots = 3 // 7 confirmed > threshold (5)
	if !g.ThresholdMet() {
		t.Fatal("should be met")
	}
	if g.Progress() != 1.0 {
		t.Fatalf("capped at 1.0, got %v", g.Progress())
	}

	// Edge: zero threshold returns 0 (avoids divide-by-zero).
	g.Threshold = 0
	if g.Progress() != 0 {
		t.Fatalf("zero threshold → 0, got %v", g.Progress())
	}
}

func TestGroupBuyValidate(t *testing.T) {
	now := time.Now().UTC()
	g := makeGoodGroupBuy(now)
	if err := g.Validate(now); err != nil {
		t.Fatalf("good: %v", err)
	}

	cases := []struct {
		name string
		mut  func(g *domain.GroupBuy)
	}{
		{"missing title", func(g *domain.GroupBuy) { g.Title = "" }},
		{"non-positive threshold", func(g *domain.GroupBuy) { g.Threshold = 0 }},
		{"non-positive capacity", func(g *domain.GroupBuy) { g.Capacity = 0 }},
		{"threshold > capacity", func(g *domain.GroupBuy) { g.Threshold = 100 }},
		{"end before start", func(g *domain.GroupBuy) { g.EndsAt = g.StartsAt }},
		{"deadline in past", func(g *domain.GroupBuy) { g.Deadline = now.Add(-time.Minute) }},
		{"deadline after start", func(g *domain.GroupBuy) { g.Deadline = g.StartsAt.Add(time.Hour) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gg := *makeGoodGroupBuy(now)
			c.mut(&gg)
			err := gg.Validate(now)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}
