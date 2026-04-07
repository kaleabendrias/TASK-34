package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/repository"
)

// AnalyticsService is the entry point for emitting and querying analytics
// data. Pre-aggregation and anomaly detection are exposed as Run* methods so
// the jobs Runner can schedule them.
type AnalyticsService struct {
	repo      repository.AnalyticsRepository
	anonSalt  string
	anomalyX  float64 // 3.0 by spec
	log       *slog.Logger
}

func NewAnalyticsService(repo repository.AnalyticsRepository, anonSalt string, log *slog.Logger) *AnalyticsService {
	return &AnalyticsService{repo: repo, anonSalt: anonSalt, anomalyX: 3.0, log: log}
}

// Anon returns the hashed identifier used in events. The hash survives a
// hard-deletion of the user but cannot be reversed without the salt.
func (s *AnalyticsService) Anon(userID uuid.UUID) string {
	if userID == uuid.Nil {
		return ""
	}
	h := sha256.Sum256([]byte(s.anonSalt + ":" + userID.String()))
	return hex.EncodeToString(h[:])
}

func (s *AnalyticsService) Track(ctx context.Context, eventType domain.AnalyticsEventType, targetType string, targetID uuid.UUID, userID uuid.UUID) error {
	return s.repo.RecordEvent(ctx, &domain.AnalyticsEvent{
		EventType:  eventType,
		TargetType: targetType,
		TargetID:   targetID,
		UserAnon:   s.Anon(userID),
	})
}

func (s *AnalyticsService) TopSessions(ctx context.Context, days int, limit int) ([]domain.TopSession, error) {
	if days <= 0 {
		days = 7
	}
	since := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	return s.repo.TopSessions(ctx, since, limit)
}

// Trends returns 7 / 30 / 90 day daily trend buckets for an event type.
func (s *AnalyticsService) Trends(ctx context.Context, eventType domain.AnalyticsEventType) (map[int][]domain.TrendBucket, error) {
	out := map[int][]domain.TrendBucket{}
	for _, days := range []int{7, 30, 90} {
		buckets, err := s.repo.Trend(ctx, eventType, days)
		if err != nil {
			return nil, err
		}
		out[days] = buckets
	}
	return out, nil
}

func (s *AnalyticsService) Anomalies(ctx context.Context, limit int) ([]domain.AnomalyAlert, error) {
	return s.repo.ListAnomalies(ctx, limit)
}

// RunAggregation rolls raw events into the hourly table. Idempotent.
func (s *AnalyticsService) RunAggregation(ctx context.Context) error {
	since := time.Now().UTC().Add(-2 * time.Hour) // catch the previous full hour
	n, err := s.repo.AggregateRecent(ctx, since)
	if err != nil {
		return err
	}
	s.log.Debug("analytics aggregation", "events", n)
	return nil
}

// RunAnomalyDetection compares the previous hour against the trailing 7-day
// hourly average for each event type. If observed > 3x baseline (and baseline
// is non-trivial) an anomaly_alert row is inserted.
func (s *AnalyticsService) RunAnomalyDetection(ctx context.Context) error {
	hour := time.Now().UTC().Truncate(time.Hour).Add(-time.Hour)
	for _, et := range []domain.AnalyticsEventType{domain.EventView, domain.EventFavorite, domain.EventComment, domain.EventDownload} {
		observed, err := s.repo.HourlyCount(ctx, et, hour)
		if err != nil {
			return err
		}
		baseline, err := s.repo.BaselineHourlyAverage(ctx, et, hour)
		if err != nil {
			return err
		}
		if baseline < 1 || observed == 0 {
			continue
		}
		ratio := float64(observed) / baseline
		if ratio > s.anomalyX {
			alert := &domain.AnomalyAlert{
				EventType: et,
				Observed:  observed,
				Baseline:  baseline,
				Ratio:     ratio,
				Detail:    fmt.Sprintf("hour=%s observed=%d baseline=%.2f", hour.Format(time.RFC3339), observed, baseline),
			}
			if err := s.repo.InsertAnomaly(ctx, alert); err != nil {
				s.log.Warn("anomaly insert failed", "error", err)
				continue
			}
			s.log.Warn("traffic anomaly detected", "event", et, "ratio", ratio, "observed", observed, "baseline", baseline)
		}
	}
	return nil
}

// AnonymiseUserEvents is invoked by the deletion executor to scrub any
// reference to a user from the events table. The hashed user_anon column is
// nulled out so historic counts remain intact.
func (s *AnalyticsService) AnonymiseUserEvents(ctx context.Context, userID uuid.UUID) error {
	return s.repo.AnonymiseUserEvents(ctx, s.Anon(userID))
}
