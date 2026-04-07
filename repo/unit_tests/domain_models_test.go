package unit_tests

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
)

func TestMaskName(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"a":        "a*",
		"alice":    "a****",
		"bob":      "b**",
		"José":     "J***",
	}
	for in, want := range cases {
		if got := domain.MaskName(in); got != want {
			t.Errorf("MaskName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBookingStatusActiveAndTerminal(t *testing.T) {
	active := []domain.BookingStatus{
		domain.StatusPendingConfirmation,
		domain.StatusWaitlisted,
		domain.StatusCheckedIn,
	}
	for _, s := range active {
		if !s.IsActive() {
			t.Errorf("%s should be active", s)
		}
		if s.IsTerminal() {
			t.Errorf("%s should not be terminal", s)
		}
	}
	terminal := []domain.BookingStatus{
		domain.StatusCompleted,
		domain.StatusCanceled,
	}
	for _, s := range terminal {
		if s.IsActive() {
			t.Errorf("%s should not be active", s)
		}
		if !s.IsTerminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
}

func TestUserCaptchaRequired(t *testing.T) {
	u := &domain.User{}
	if u.CaptchaRequired() {
		t.Fatal("0 attempts should not require captcha")
	}
	u.FailedAttempts = 1
	if u.CaptchaRequired() {
		t.Fatal("1 attempt should not require captcha")
	}
	u.FailedAttempts = 2
	if !u.CaptchaRequired() {
		t.Fatal("2 attempts should require captcha")
	}
	u.FailedAttempts = 5
	if !u.CaptchaRequired() {
		t.Fatal("5 attempts should require captcha")
	}
}

func TestUserIsLocked(t *testing.T) {
	now := time.Now().UTC()
	u := &domain.User{}
	if u.IsLocked(now) {
		t.Fatal("nil locked_until → not locked")
	}
	past := now.Add(-time.Minute)
	u.LockedUntil = &past
	if u.IsLocked(now) {
		t.Fatal("locked_until in the past → not locked")
	}
	future := now.Add(time.Minute)
	u.LockedUntil = &future
	if !u.IsLocked(now) {
		t.Fatal("locked_until in the future → locked")
	}
}

func TestBookingValidate(t *testing.T) {
	uid := uuid.New()
	rid := uuid.New()
	now := time.Now().UTC()

	good := &domain.Booking{
		UserID: uid, ResourceID: rid, PartySize: 1,
		StartTime: now, EndTime: now.Add(time.Hour),
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("good booking should validate: %v", err)
	}

	cases := []struct {
		name string
		mut  func(b *domain.Booking)
	}{
		{"missing user", func(b *domain.Booking) { b.UserID = uuid.Nil }},
		{"missing resource", func(b *domain.Booking) { b.ResourceID = uuid.Nil }},
		{"non-positive party size", func(b *domain.Booking) { b.PartySize = 0 }},
		{"end before start", func(b *domain.Booking) { b.EndTime = b.StartTime }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := *good
			c.mut(&b)
			if err := b.Validate(); err == nil {
				t.Fatal("expected error")
			} else if !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("expected ErrInvalidInput, got %v", err)
			}
		})
	}
}

func TestGroupReservationValidateAndHeadcount(t *testing.T) {
	g := &domain.GroupReservation{Name: "X", OrganizerEmail: "x@example.com", Capacity: 10}
	if err := g.Validate(); err != nil {
		t.Fatalf("good: %v", err)
	}
	g.Bookings = []domain.Booking{
		{Status: domain.StatusPendingConfirmation, PartySize: 2},
		{Status: domain.StatusCheckedIn, PartySize: 3},
		{Status: domain.StatusCanceled, PartySize: 100},
	}
	if got := g.CurrentHeadcount(); got != 5 {
		t.Fatalf("headcount = %d, want 5", got)
	}

	for _, mut := range []func(*domain.GroupReservation){
		func(g *domain.GroupReservation) { g.Name = "" },
		func(g *domain.GroupReservation) { g.OrganizerEmail = "" },
		func(g *domain.GroupReservation) { g.Capacity = 0 },
	} {
		gg := *g
		gg.Bookings = nil
		mut(&gg)
		if err := gg.Validate(); err == nil {
			t.Fatal("expected error")
		}
	}
}
