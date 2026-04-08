package unit_tests

import (
	"errors"
	"os"
	"testing"

	"github.com/harborworks/booking-hub/internal/infrastructure/config"
)

// TestConfig_RejectsMissingAnalyticsSalt is the "negative configuration"
// audit test: if ANALYTICS_ANON_SALT is absent from the environment the
// server must refuse to boot. There is no development fallback literal;
// every deployment supplies the salt through secure configuration.
func TestConfig_RejectsMissingAnalyticsSalt(t *testing.T) {
	// Snapshot and clear the salt for this test only.
	prev, had := os.LookupEnv("ANALYTICS_ANON_SALT")
	if err := os.Unsetenv("ANALYTICS_ANON_SALT"); err != nil {
		t.Fatalf("unset: %v", err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("ANALYTICS_ANON_SALT", prev)
		} else {
			_ = os.Unsetenv("ANALYTICS_ANON_SALT")
		}
	})

	cfg, err := config.Load()
	if err == nil {
		t.Fatalf("Load() succeeded without ANALYTICS_ANON_SALT; cfg=%+v", cfg)
	}
	if !errors.Is(err, config.ErrMissingAnalyticsSalt) {
		t.Errorf("expected ErrMissingAnalyticsSalt, got %v", err)
	}
}

// TestConfig_WhitespaceSaltRejected guards against an operator supplying
// ANALYTICS_ANON_SALT="   " and thinking they've satisfied the contract:
// trimmed-empty must behave exactly like unset.
func TestConfig_WhitespaceSaltRejected(t *testing.T) {
	prev, had := os.LookupEnv("ANALYTICS_ANON_SALT")
	if err := os.Setenv("ANALYTICS_ANON_SALT", "   \t\n "); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("ANALYTICS_ANON_SALT", prev)
		} else {
			_ = os.Unsetenv("ANALYTICS_ANON_SALT")
		}
	})

	if _, err := config.Load(); !errors.Is(err, config.ErrMissingAnalyticsSalt) {
		t.Errorf("expected ErrMissingAnalyticsSalt for whitespace-only salt, got %v", err)
	}
}

// TestConfig_AcceptsSaltFromEnv is the positive counterpart: a real value
// loads cleanly. Keeps the above two tests from being trivially wrong.
func TestConfig_AcceptsSaltFromEnv(t *testing.T) {
	prev, had := os.LookupEnv("ANALYTICS_ANON_SALT")
	if err := os.Setenv("ANALYTICS_ANON_SALT", "unit-test-salt"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("ANALYTICS_ANON_SALT", prev)
		} else {
			_ = os.Unsetenv("ANALYTICS_ANON_SALT")
		}
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if cfg.AnalyticsAnonSalt != "unit-test-salt" {
		t.Errorf("expected salt to round-trip, got %q", cfg.AnalyticsAnonSalt)
	}
}
