package unit_tests

import (
	"testing"
	"time"

	"github.com/harborworks/booking-hub/internal/service"
)

func TestWebhookBackoffSchedule(t *testing.T) {
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 0},
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
	}
	for _, c := range cases {
		got := service.WebhookBackoff(c.attempt)
		if got != c.want {
			t.Errorf("WebhookBackoff(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestWebhookRetryCap(t *testing.T) {
	// Up to attempt 4 we keep retrying.
	for i := 1; i < service.WebhookMaxAttempts; i++ {
		if got := service.NextWebhookStatus(i); got != "pending" {
			t.Errorf("attempt %d should remain pending, got %s", i, got)
		}
	}
	// At and beyond the cap we mark dead.
	if got := service.NextWebhookStatus(service.WebhookMaxAttempts); got != "dead" {
		t.Errorf("attempt %d should be dead, got %s", service.WebhookMaxAttempts, got)
	}
	if got := service.NextWebhookStatus(service.WebhookMaxAttempts + 5); got != "dead" {
		t.Errorf("any attempt beyond cap should be dead, got %s", got)
	}
}
