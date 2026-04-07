package domain

import (
	"time"

	"github.com/google/uuid"
)

type AnalyticsEventType string

const (
	EventView     AnalyticsEventType = "view"
	EventFavorite AnalyticsEventType = "favorite"
	EventComment  AnalyticsEventType = "comment"
	EventDownload AnalyticsEventType = "download"
)

// AnalyticsEvent is a single observed action against a target. user_anon is a
// hashed identifier so the row can survive a hard deletion of the user.
type AnalyticsEvent struct {
	ID         int64              `json:"id"`
	EventType  AnalyticsEventType `json:"event_type"`
	TargetType string             `json:"target_type"`
	TargetID   uuid.UUID          `json:"target_id"`
	UserAnon   string             `json:"user_anon,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
}

// AnalyticsHourly is a per-hour pre-aggregation row, kept up-to-date by the
// background analytics job. Querying it is much cheaper than scanning raw
// events for trend windows.
type AnalyticsHourly struct {
	BucketStart time.Time          `json:"bucket_start"`
	EventType   AnalyticsEventType `json:"event_type"`
	TargetType  string             `json:"target_type"`
	TargetID    uuid.UUID          `json:"target_id"`
	Count       int64              `json:"count"`
}

// AnomalyAlert records a detected traffic spike (observed/baseline ratio above
// the threshold).
type AnomalyAlert struct {
	ID         uuid.UUID          `json:"id"`
	DetectedAt time.Time          `json:"detected_at"`
	EventType  AnalyticsEventType `json:"event_type"`
	Observed   int64              `json:"observed"`
	Baseline   float64            `json:"baseline"`
	Ratio      float64            `json:"ratio"`
	Detail     string             `json:"detail"`
}

// TopSession is a row in the "top sessions" report. The TargetID points at a
// resource or a group_buy depending on TargetType.
type TopSession struct {
	TargetType string    `json:"target_type"`
	TargetID   uuid.UUID `json:"target_id"`
	Score      int64     `json:"score"`
}

// TrendBucket is a single bucket inside a trend series.
type TrendBucket struct {
	BucketStart time.Time `json:"bucket_start"`
	Count       int64     `json:"count"`
}
