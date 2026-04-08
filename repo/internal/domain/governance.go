package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// DataDictionaryEntry describes a single field in the local data dictionary.
type DataDictionaryEntry struct {
	ID          uuid.UUID `json:"id"`
	Entity      string    `json:"entity"`
	Field       string    `json:"field"`
	DataType    string    `json:"data_type"`
	Description string    `json:"description"`
	Sensitive   bool      `json:"sensitive"`
	Tags        []string  `json:"tags"`
}

type Tag struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

type Tagging struct {
	ID         uuid.UUID `json:"id"`
	TagID      uuid.UUID `json:"tag_id"`
	TargetType string    `json:"target_type"`
	TargetID   uuid.UUID `json:"target_id"`
}

// ConsentRecord captures whether a user granted or withdrew consent for a
// specific scope (e.g. "marketing", "analytics", "share_profile").
type ConsentRecord struct {
	ID          uuid.UUID  `json:"id"`
	UserID      uuid.UUID  `json:"user_id"`
	Scope       string     `json:"scope"`
	Granted     bool       `json:"granted"`
	Version     string     `json:"version"`
	GrantedAt   *time.Time `json:"granted_at,omitempty"`
	WithdrawnAt *time.Time `json:"withdrawn_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// DeletionRequest is the user-initiated self-service erasure request. It is
// intentionally delayed (process_after) so the user has time to cancel.
type DeletionRequest struct {
	ID           uuid.UUID  `json:"id"`
	UserID       uuid.UUID  `json:"user_id"`
	RequestedAt  time.Time  `json:"requested_at"`
	ProcessAfter time.Time  `json:"process_after"`
	Status       string     `json:"status"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

// Webhook is an outbound integration target.
type Webhook struct {
	ID           uuid.UUID         `json:"id"`
	Name         string            `json:"name"`
	TargetURL    string            `json:"target_url"`
	EventFilter  []string          `json:"event_filter"`
	FieldMapping map[string]string `json:"field_mapping"`
	Secret       string            `json:"secret"`
	Enabled      bool              `json:"enabled"`
	CreatedAt    time.Time         `json:"created_at"`
}

type WebhookDelivery struct {
	ID            uuid.UUID       `json:"id"`
	WebhookID     uuid.UUID       `json:"webhook_id"`
	EventType     string          `json:"event_type"`
	Payload       json.RawMessage `json:"payload"`
	Attempts      int             `json:"attempts"`
	NextAttemptAt time.Time       `json:"next_attempt_at"`
	Status        string          `json:"status"`
	LastResponse  string          `json:"last_response"`
	CreatedAt     time.Time       `json:"created_at"`
}

// Backup is a row in the local backups index.
type Backup struct {
	ID        uuid.UUID `json:"id"`
	Kind      string    `json:"kind"`
	Path      string    `json:"path"`
	SizeBytes int64     `json:"size_bytes"`
	TakenAt   time.Time `json:"taken_at"`
	Detail    string    `json:"detail"`
}

// IdempotencyRecord persists the outcome of a side-effecting request so retries
// resolve to the same response.
type IdempotencyRecord struct {
	Key          string     `json:"key"`
	UserID       *uuid.UUID `json:"user_id,omitempty"`
	RequestHash  string     `json:"request_hash"`
	StatusCode   int        `json:"status_code"`
	ResponseBody []byte     `json:"-"`
	ContentType  string     `json:"content_type"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
}

// Idempotency record statuses.
const (
	IdempotencyStatusPending   = "pending"
	IdempotencyStatusCompleted = "completed"
)
