package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Notification is a passive in-app message.
type Notification struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// TodoStatus filter values for the todo list.
type TodoStatus string

const (
	TodoOpen       TodoStatus = "open"
	TodoInProgress TodoStatus = "in_progress"
	TodoDone       TodoStatus = "done"
	TodoDismissed  TodoStatus = "dismissed"
)

// Todo is an actionable task surfaced in the To-Do Center. The TaskType is a
// free-form string identifier (e.g. "confirm_booking", "approve_group_buy")
// that the UI maps to a concrete CTA. Payload carries arbitrary JSON.
type Todo struct {
	ID        uuid.UUID       `json:"id"`
	UserID    uuid.UUID       `json:"user_id"`
	TaskType  string          `json:"task_type"`
	Title     string          `json:"title"`
	Payload   json.RawMessage `json:"payload"`
	Status    TodoStatus      `json:"status"`
	DueAt     *time.Time      `json:"due_at,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// NotificationDelivery is an admin-side delivery log entry. Each row records
// the channel used (in_app, email, webhook) and the outcome.
type NotificationDelivery struct {
	ID             uuid.UUID  `json:"id"`
	NotificationID *uuid.UUID `json:"notification_id,omitempty"`
	UserID         *uuid.UUID `json:"user_id,omitempty"`
	Channel        string     `json:"channel"`
	Status         string     `json:"status"`
	Detail         string     `json:"detail"`
	DeliveredAt    time.Time  `json:"delivered_at"`
}
