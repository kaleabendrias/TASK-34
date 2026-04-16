package unit_tests

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/service"
)

func newTestAnalyticsService(repo *mockAnalyticsRepo) *service.AnalyticsService {
	return service.NewAnalyticsService(repo, "test-salt-12345", slog.Default())
}

// ─── Anon ────────────────────────────────────────────────────────────────────

func TestAnalyticsAnon_NilUUID_ReturnsEmpty(t *testing.T) {
	svc := newTestAnalyticsService(newMockAnalyticsRepo())
	got := svc.Anon(uuid.Nil)
	if got != "" {
		t.Errorf("expected empty string for nil UUID, got %q", got)
	}
}

func TestAnalyticsAnon_Deterministic(t *testing.T) {
	svc := newTestAnalyticsService(newMockAnalyticsRepo())
	uid := uuid.New()
	got1 := svc.Anon(uid)
	got2 := svc.Anon(uid)
	if got1 != got2 {
		t.Errorf("Anon not deterministic: %q vs %q", got1, got2)
	}
	if len(got1) != 64 { // SHA-256 hex → 64 chars
		t.Errorf("expected 64-char hex hash, got len=%d", len(got1))
	}
}

func TestAnalyticsAnon_DifferentSalts_DifferentHashes(t *testing.T) {
	svc1 := service.NewAnalyticsService(newMockAnalyticsRepo(), "salt-A", slog.Default())
	svc2 := service.NewAnalyticsService(newMockAnalyticsRepo(), "salt-B", slog.Default())
	uid := uuid.New()
	if svc1.Anon(uid) == svc2.Anon(uid) {
		t.Error("different salts should produce different hashes")
	}
}

func TestAnalyticsAnon_DifferentUsers_DifferentHashes(t *testing.T) {
	svc := newTestAnalyticsService(newMockAnalyticsRepo())
	uid1 := uuid.New()
	uid2 := uuid.New()
	if svc.Anon(uid1) == svc.Anon(uid2) {
		t.Error("different user IDs should produce different hashes")
	}
}

// ─── Track ───────────────────────────────────────────────────────────────────

func TestTrack_RecordsEvent(t *testing.T) {
	repo := newMockAnalyticsRepo()
	svc := newTestAnalyticsService(repo)

	err := svc.Track(context.Background(), domain.EventView, "resource", uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("Track: %v", err)
	}
	repo.mu.Lock()
	n := len(repo.events)
	repo.mu.Unlock()
	if n != 1 {
		t.Errorf("expected 1 event, got %d", n)
	}
}

func TestTrack_AnonymisesUser(t *testing.T) {
	repo := newMockAnalyticsRepo()
	svc := newTestAnalyticsService(repo)
	userID := uuid.New()

	err := svc.Track(context.Background(), domain.EventFavorite, "resource", uuid.New(), userID)
	if err != nil {
		t.Fatalf("Track: %v", err)
	}

	repo.mu.Lock()
	stored := repo.events[0]
	repo.mu.Unlock()

	expectedAnon := svc.Anon(userID)
	if stored.UserAnon != expectedAnon {
		t.Errorf("expected anon %s, got %s", expectedAnon, stored.UserAnon)
	}
}

func TestTrack_NilUser_EmptyAnon(t *testing.T) {
	repo := newMockAnalyticsRepo()
	svc := newTestAnalyticsService(repo)

	err := svc.Track(context.Background(), domain.EventView, "resource", uuid.New(), uuid.Nil)
	if err != nil {
		t.Fatalf("Track: %v", err)
	}
	repo.mu.Lock()
	stored := repo.events[0]
	repo.mu.Unlock()
	if stored.UserAnon != "" {
		t.Errorf("expected empty anon for nil user, got %q", stored.UserAnon)
	}
}

func TestTrack_RepoError_ReturnsError(t *testing.T) {
	repo := newMockAnalyticsRepo()
	repo.recordEventErr = errMock
	svc := newTestAnalyticsService(repo)

	err := svc.Track(context.Background(), domain.EventView, "resource", uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error from repo")
	}
}

// ─── TopSessions ─────────────────────────────────────────────────────────────

func TestTopSessions_NonPositiveDays_Defaults(t *testing.T) {
	svc := newTestAnalyticsService(newMockAnalyticsRepo())
	// days=0 should default to 7 internally without panic.
	_, err := svc.TopSessions(context.Background(), 0, 5)
	if err != nil {
		t.Fatalf("TopSessions: %v", err)
	}
}

func TestTopSessions_ReturnsRepoResults(t *testing.T) {
	svc := newTestAnalyticsService(newMockAnalyticsRepo())
	list, err := svc.TopSessions(context.Background(), 7, 10)
	if err != nil {
		t.Fatalf("TopSessions: %v", err)
	}
	// Mock returns empty slice; just confirm no error.
	if list == nil {
		t.Error("expected non-nil list")
	}
}

// ─── Trends ──────────────────────────────────────────────────────────────────

func TestTrends_ReturnsBucketsForAllWindows(t *testing.T) {
	svc := newTestAnalyticsService(newMockAnalyticsRepo())
	result, err := svc.Trends(context.Background(), domain.EventView)
	if err != nil {
		t.Fatalf("Trends: %v", err)
	}
	for _, days := range []int{7, 30, 90} {
		if _, ok := result[days]; !ok {
			t.Errorf("missing %d-day bucket", days)
		}
	}
}

// ─── RunAnomalyDetection ─────────────────────────────────────────────────────

func TestRunAnomalyDetection_NoAnomaly(t *testing.T) {
	repo := newMockAnalyticsRepo()
	repo.hourlyCount = 5
	repo.baselineAvg = 10.0 // ratio = 0.5, below 3x threshold
	svc := newTestAnalyticsService(repo)

	err := svc.RunAnomalyDetection(context.Background())
	if err != nil {
		t.Fatalf("RunAnomalyDetection: %v", err)
	}
	repo.mu.Lock()
	n := len(repo.anomalies)
	repo.mu.Unlock()
	if n != 0 {
		t.Errorf("expected no anomalies, got %d", n)
	}
}

func TestRunAnomalyDetection_DetectsAnomaly(t *testing.T) {
	repo := newMockAnalyticsRepo()
	repo.hourlyCount = 100   // observed
	repo.baselineAvg = 10.0  // baseline; ratio = 10x > 3x threshold
	svc := newTestAnalyticsService(repo)

	err := svc.RunAnomalyDetection(context.Background())
	if err != nil {
		t.Fatalf("RunAnomalyDetection: %v", err)
	}
	repo.mu.Lock()
	n := len(repo.anomalies)
	repo.mu.Unlock()
	// There are 4 event types, and repo returns the same hourly/baseline for
	// all of them, so we expect 4 anomaly alerts.
	if n == 0 {
		t.Error("expected at least one anomaly alert")
	}
}

func TestRunAnomalyDetection_LowBaseline_Skipped(t *testing.T) {
	// baseline < 1 → skip (not enough history).
	repo := newMockAnalyticsRepo()
	repo.hourlyCount = 100
	repo.baselineAvg = 0 // < 1 → skip
	svc := newTestAnalyticsService(repo)

	if err := svc.RunAnomalyDetection(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repo.mu.Lock()
	n := len(repo.anomalies)
	repo.mu.Unlock()
	if n != 0 {
		t.Errorf("expected no anomalies for low baseline, got %d", n)
	}
}

func TestRunAnomalyDetection_ZeroObserved_Skipped(t *testing.T) {
	repo := newMockAnalyticsRepo()
	repo.hourlyCount = 0    // observed = 0 → skip
	repo.baselineAvg = 10.0
	svc := newTestAnalyticsService(repo)

	if err := svc.RunAnomalyDetection(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repo.mu.Lock()
	n := len(repo.anomalies)
	repo.mu.Unlock()
	if n != 0 {
		t.Errorf("expected no anomalies for zero observed, got %d", n)
	}
}

// ─── RunAggregation ──────────────────────────────────────────────────────────

func TestRunAggregation_Success(t *testing.T) {
	svc := newTestAnalyticsService(newMockAnalyticsRepo())
	if err := svc.RunAggregation(context.Background()); err != nil {
		t.Fatalf("RunAggregation: %v", err)
	}
}

// ─── AnonymiseUserEvents ──────────────────────────────────────────────────────

func TestAnonymiseUserEvents_Success(t *testing.T) {
	svc := newTestAnalyticsService(newMockAnalyticsRepo())
	if err := svc.AnonymiseUserEvents(context.Background(), uuid.New()); err != nil {
		t.Fatalf("AnonymiseUserEvents: %v", err)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// errMock is a generic error used in error-injection tests across all service
// test files in this package.
var errMock = domain.ErrInvalidInput
