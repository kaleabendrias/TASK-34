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

func TestValidateWebhookTargetURL_Allowed(t *testing.T) {
	allowed := []string{
		"http://127.0.0.1:9000/hook",
		"http://localhost:8080/path",
		"http://app:8080/x",
		"http://10.0.0.5/x",
		"https://192.168.1.20/y",
	}
	for _, raw := range allowed {
		if _, err := service.ValidateWebhookTargetURL(raw); err != nil {
			t.Errorf("expected %q to be allowed, got %v", raw, err)
		}
	}
}

func TestValidateWebhookTargetURL_Rejected(t *testing.T) {
	type tc struct{ raw, why string }
	cases := []tc{
		{"", "empty"},
		{"://broken", "parse"},
		{"ftp://localhost/", "scheme"},
		{"http:///nohost", "missing host"},
		{"http://8.8.8.8/", "non-local IP"},
		{"http://example.com/", "public hostname"},
	}
	for _, c := range cases {
		if _, err := service.ValidateWebhookTargetURL(c.raw); err == nil {
			t.Errorf("expected %q to be rejected (%s)", c.raw, c.why)
		}
	}
}

func TestNextWebhookStatus_RetryCap(t *testing.T) {
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
