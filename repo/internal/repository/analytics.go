package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harborworks/booking-hub/internal/domain"
)

type AnalyticsRepository interface {
	RecordEvent(ctx context.Context, e *domain.AnalyticsEvent) error
	UpsertHourly(ctx context.Context, h domain.AnalyticsHourly) error
	AggregateRecent(ctx context.Context, since time.Time) (int64, error)

	TopSessions(ctx context.Context, since time.Time, limit int) ([]domain.TopSession, error)
	Trend(ctx context.Context, eventType domain.AnalyticsEventType, days int) ([]domain.TrendBucket, error)

	HourlyCount(ctx context.Context, eventType domain.AnalyticsEventType, hourStart time.Time) (int64, error)
	BaselineHourlyAverage(ctx context.Context, eventType domain.AnalyticsEventType, hourStart time.Time) (float64, error)

	InsertAnomaly(ctx context.Context, a *domain.AnomalyAlert) error
	ListAnomalies(ctx context.Context, limit int) ([]domain.AnomalyAlert, error)

	AnonymiseUserEvents(ctx context.Context, anon string) error
}

type analyticsRepo struct{ pool *pgxpool.Pool }

func NewAnalyticsRepository(pool *pgxpool.Pool) AnalyticsRepository {
	return &analyticsRepo{pool: pool}
}

func (r *analyticsRepo) RecordEvent(ctx context.Context, e *domain.AnalyticsEvent) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	const q = `
		INSERT INTO analytics_events (event_type, target_type, target_id, user_anon, created_at)
		VALUES ($1,$2,$3,$4,$5) RETURNING id
	`
	return r.pool.QueryRow(ctx, q, string(e.EventType), e.TargetType, e.TargetID, e.UserAnon, e.CreatedAt).Scan(&e.ID)
}

func (r *analyticsRepo) UpsertHourly(ctx context.Context, h domain.AnalyticsHourly) error {
	const q = `
		INSERT INTO analytics_hourly (bucket_start, event_type, target_type, target_id, count)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (bucket_start, event_type, target_type, target_id)
		DO UPDATE SET count = analytics_hourly.count + EXCLUDED.count
	`
	_, err := r.pool.Exec(ctx, q, h.BucketStart, string(h.EventType), h.TargetType, h.TargetID, h.Count)
	return err
}

// AggregateRecent rolls all events newer than `since` into the hourly table
// and returns how many were aggregated. Used by the background job. Idempotent
// because the hourly upsert sums increments and the events table is also
// pruned by the same job after success in real deployments.
func (r *analyticsRepo) AggregateRecent(ctx context.Context, since time.Time) (int64, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT date_trunc('hour', created_at), event_type, target_type, target_id, COUNT(*)
		FROM analytics_events WHERE created_at > $1
		GROUP BY 1,2,3,4
	`, since)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var total int64
	for rows.Next() {
		var h domain.AnalyticsHourly
		var et string
		if err := rows.Scan(&h.BucketStart, &et, &h.TargetType, &h.TargetID, &h.Count); err != nil {
			return 0, err
		}
		h.EventType = domain.AnalyticsEventType(et)
		if err := r.UpsertHourly(ctx, h); err != nil {
			return 0, err
		}
		total += h.Count
	}
	return total, rows.Err()
}

func (r *analyticsRepo) TopSessions(ctx context.Context, since time.Time, limit int) ([]domain.TopSession, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	rows, err := r.pool.Query(ctx, `
		SELECT target_type, target_id, COUNT(*) AS score
		FROM analytics_events
		WHERE created_at >= $1
		GROUP BY target_type, target_id
		ORDER BY score DESC
		LIMIT $2
	`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.TopSession, 0)
	for rows.Next() {
		var t domain.TopSession
		if err := rows.Scan(&t.TargetType, &t.TargetID, &t.Score); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *analyticsRepo) Trend(ctx context.Context, eventType domain.AnalyticsEventType, days int) ([]domain.TrendBucket, error) {
	if days <= 0 {
		days = 7
	}
	since := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	rows, err := r.pool.Query(ctx, `
		SELECT date_trunc('day', created_at) AS bucket, COUNT(*)
		FROM analytics_events
		WHERE created_at >= $1 AND event_type = $2
		GROUP BY bucket ORDER BY bucket
	`, since, string(eventType))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.TrendBucket, 0, days)
	for rows.Next() {
		var b domain.TrendBucket
		if err := rows.Scan(&b.BucketStart, &b.Count); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *analyticsRepo) HourlyCount(ctx context.Context, eventType domain.AnalyticsEventType, hourStart time.Time) (int64, error) {
	end := hourStart.Add(time.Hour)
	var c int64
	if err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM analytics_events
		WHERE event_type = $1 AND created_at >= $2 AND created_at < $3
	`, string(eventType), hourStart, end).Scan(&c); err != nil {
		return 0, err
	}
	return c, nil
}

// BaselineHourlyAverage returns the average count per hour over the trailing
// 7 days excluding the current hour.
func (r *analyticsRepo) BaselineHourlyAverage(ctx context.Context, eventType domain.AnalyticsEventType, hourStart time.Time) (float64, error) {
	from := hourStart.Add(-7 * 24 * time.Hour)
	var avg float64
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(c), 0) FROM (
			SELECT COUNT(*)::float AS c
			FROM analytics_events
			WHERE event_type = $1 AND created_at >= $2 AND created_at < $3
			GROUP BY date_trunc('hour', created_at)
		) AS hourly
	`, string(eventType), from, hourStart).Scan(&avg)
	if err != nil {
		return 0, err
	}
	return avg, nil
}

func (r *analyticsRepo) InsertAnomaly(ctx context.Context, a *domain.AnomalyAlert) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	if a.DetectedAt.IsZero() {
		a.DetectedAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO anomaly_alerts (id, detected_at, event_type, observed, baseline, ratio, detail)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, a.ID, a.DetectedAt, string(a.EventType), a.Observed, a.Baseline, a.Ratio, a.Detail)
	return err
}

func (r *analyticsRepo) ListAnomalies(ctx context.Context, limit int) ([]domain.AnomalyAlert, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, detected_at, event_type, observed, baseline, ratio, detail
		FROM anomaly_alerts ORDER BY detected_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.AnomalyAlert, 0)
	for rows.Next() {
		var a domain.AnomalyAlert
		var et string
		if err := rows.Scan(&a.ID, &a.DetectedAt, &et, &a.Observed, &a.Baseline, &a.Ratio, &a.Detail); err != nil {
			return nil, err
		}
		a.EventType = domain.AnalyticsEventType(et)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *analyticsRepo) AnonymiseUserEvents(ctx context.Context, anon string) error {
	_, err := r.pool.Exec(ctx, `UPDATE analytics_events SET user_anon = NULL WHERE user_anon = $1`, anon)
	return err
}
