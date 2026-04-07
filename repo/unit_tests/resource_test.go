package unit_tests

import (
	"testing"
	"time"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/service"
)

func TestComputeFreeWindows(t *testing.T) {
	day := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
	open := day.Add(8 * time.Hour)  // 08:00
	close := day.Add(22 * time.Hour) // 22:00

	t.Run("no bookings → one big window", func(t *testing.T) {
		got := service.ComputeFreeWindows(open, close, nil)
		if len(got) != 1 || !got[0].StartTime.Equal(open) || !got[0].EndTime.Equal(close) {
			t.Fatalf("got %#v", got)
		}
	})

	t.Run("middle booking carves a gap", func(t *testing.T) {
		bk := []domain.Booking{
			{StartTime: day.Add(10 * time.Hour), EndTime: day.Add(12 * time.Hour)},
		}
		got := service.ComputeFreeWindows(open, close, bk)
		if len(got) != 2 {
			t.Fatalf("expected 2 free windows, got %d: %#v", len(got), got)
		}
		if !got[0].EndTime.Equal(day.Add(10 * time.Hour)) {
			t.Errorf("first ends at 10:00, got %v", got[0].EndTime)
		}
		if !got[1].StartTime.Equal(day.Add(12 * time.Hour)) {
			t.Errorf("second starts at 12:00, got %v", got[1].StartTime)
		}
	})

	t.Run("bookings touching the edges", func(t *testing.T) {
		bk := []domain.Booking{
			{StartTime: day.Add(7 * time.Hour), EndTime: day.Add(9 * time.Hour)},   // overlaps open
			{StartTime: day.Add(20 * time.Hour), EndTime: day.Add(23 * time.Hour)}, // overlaps close
		}
		got := service.ComputeFreeWindows(open, close, bk)
		if len(got) != 1 {
			t.Fatalf("expected 1 free window, got %d", len(got))
		}
		if !got[0].StartTime.Equal(day.Add(9 * time.Hour)) || !got[0].EndTime.Equal(day.Add(20 * time.Hour)) {
			t.Errorf("free window mismatch: %#v", got[0])
		}
	})

	t.Run("close before open returns nil", func(t *testing.T) {
		got := service.ComputeFreeWindows(close, open, nil)
		if got != nil {
			t.Fatalf("expected nil, got %#v", got)
		}
	})

	t.Run("zero-duration booking is skipped", func(t *testing.T) {
		bk := []domain.Booking{
			{StartTime: day.Add(10 * time.Hour), EndTime: day.Add(10 * time.Hour)},
		}
		got := service.ComputeFreeWindows(open, close, bk)
		if len(got) != 1 {
			t.Fatalf("expected single uninterrupted window, got %d", len(got))
		}
	})
}
