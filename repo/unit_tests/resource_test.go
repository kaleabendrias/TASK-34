package unit_tests

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/service"
)

func TestComputeSlotCapacities(t *testing.T) {
	day := time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC)
	open := day.Add(8 * time.Hour)  // 08:00
	close := day.Add(22 * time.Hour) // 22:00

	t.Run("no bookings → every slot is full capacity", func(t *testing.T) {
		got := service.ComputeSlotCapacities(open, close, time.Hour, 4, nil)
		if len(got) != 14 {
			t.Fatalf("expected 14 hourly slots, got %d", len(got))
		}
		for _, s := range got {
			if s.RemainingSeats != 4 || s.ActivePartySize != 0 || !s.Available {
				t.Errorf("slot %v: %#v", s.StartTime.Hour(), s)
			}
		}
	})

	t.Run("partial overlap reduces remaining seats only in matching slots", func(t *testing.T) {
		bk := []domain.Booking{
			{
				ID:         uuid.New(),
				StartTime:  day.Add(10 * time.Hour),
				EndTime:    day.Add(12 * time.Hour),
				PartySize:  2,
				Status:     domain.StatusPendingConfirmation,
			},
		}
		got := service.ComputeSlotCapacities(open, close, time.Hour, 4, bk)
		for _, s := range got {
			h := s.StartTime.Hour()
			switch h {
			case 10, 11:
				if s.RemainingSeats != 2 || s.ActivePartySize != 2 {
					t.Errorf("hour %d: %#v", h, s)
				}
			default:
				if s.RemainingSeats != 4 || s.ActivePartySize != 0 {
					t.Errorf("hour %d should be empty: %#v", h, s)
				}
			}
		}
	})

	t.Run("multiple parties sum and saturate", func(t *testing.T) {
		bk := []domain.Booking{
			{StartTime: day.Add(14 * time.Hour), EndTime: day.Add(15 * time.Hour), PartySize: 3, Status: domain.StatusCheckedIn},
			{StartTime: day.Add(14 * time.Hour), EndTime: day.Add(15 * time.Hour), PartySize: 5, Status: domain.StatusPendingConfirmation},
		}
		got := service.ComputeSlotCapacities(open, close, time.Hour, 4, bk)
		for _, s := range got {
			if s.StartTime.Hour() == 14 {
				if s.ActivePartySize != 8 {
					t.Errorf("expected sum 8, got %d", s.ActivePartySize)
				}
				if s.RemainingSeats != 0 {
					t.Errorf("oversaturation should clamp to 0, got %d", s.RemainingSeats)
				}
				if s.Available {
					t.Errorf("saturated slot should not be Available")
				}
			}
		}
	})

	t.Run("canceled bookings are ignored", func(t *testing.T) {
		bk := []domain.Booking{
			{StartTime: day.Add(9 * time.Hour), EndTime: day.Add(10 * time.Hour), PartySize: 2, Status: domain.StatusCanceled},
		}
		got := service.ComputeSlotCapacities(open, close, time.Hour, 4, bk)
		for _, s := range got {
			if s.ActivePartySize != 0 {
				t.Errorf("canceled booking leaked into slot %v", s)
			}
		}
	})

	t.Run("close before open returns nil", func(t *testing.T) {
		got := service.ComputeSlotCapacities(close, open, time.Hour, 4, nil)
		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("non-positive slot or capacity returns nil", func(t *testing.T) {
		if got := service.ComputeSlotCapacities(open, close, 0, 4, nil); got != nil {
			t.Errorf("expected nil for slot=0, got %#v", got)
		}
		if got := service.ComputeSlotCapacities(open, close, time.Hour, -1, nil); got != nil {
			t.Errorf("expected nil for capacity<0, got %#v", got)
		}
	})
}
