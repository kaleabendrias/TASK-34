package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harborworks/booking-hub/internal/domain"
)

type WebhookRepository interface {
	Create(ctx context.Context, w *domain.Webhook) error
	List(ctx context.Context) ([]domain.Webhook, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Webhook, error)
	Disable(ctx context.Context, id uuid.UUID) error

	EnqueueDelivery(ctx context.Context, d *domain.WebhookDelivery) error
	DequeuePending(ctx context.Context, before time.Time, limit int) ([]domain.WebhookDelivery, error)
	UpdateDeliveryAttempt(ctx context.Context, id uuid.UUID, attempts int, status, lastResponse string, nextAttempt time.Time) error
	ListDeliveries(ctx context.Context, limit int) ([]domain.WebhookDelivery, error)
}

type webhookRepo struct{ pool *pgxpool.Pool }

func NewWebhookRepository(pool *pgxpool.Pool) WebhookRepository {
	return &webhookRepo{pool: pool}
}

func (r *webhookRepo) Create(ctx context.Context, w *domain.Webhook) error {
	if w.ID == uuid.Nil {
		w.ID = uuid.New()
	}
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}
	if w.FieldMapping == nil {
		w.FieldMapping = map[string]string{}
	}
	mapping, err := json.Marshal(w.FieldMapping)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO webhooks (id, name, target_url, event_filter, field_mapping, secret, enabled, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, w.ID, w.Name, w.TargetURL, w.EventFilter, mapping, w.Secret, w.Enabled, w.CreatedAt)
	return err
}

func (r *webhookRepo) List(ctx context.Context) ([]domain.Webhook, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, target_url, event_filter, field_mapping, secret, enabled, created_at
		FROM webhooks ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Webhook, 0)
	for rows.Next() {
		w, err := scanWebhookRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

func (r *webhookRepo) Get(ctx context.Context, id uuid.UUID) (*domain.Webhook, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, target_url, event_filter, field_mapping, secret, enabled, created_at
		FROM webhooks WHERE id = $1
	`, id)
	w, err := scanWebhookRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return w, nil
}

func scanWebhookRow(s rowScanner) (*domain.Webhook, error) {
	var (
		w       domain.Webhook
		mapping []byte
	)
	if err := s.Scan(&w.ID, &w.Name, &w.TargetURL, &w.EventFilter, &mapping, &w.Secret, &w.Enabled, &w.CreatedAt); err != nil {
		return nil, err
	}
	if len(mapping) > 0 {
		_ = json.Unmarshal(mapping, &w.FieldMapping)
	}
	if w.FieldMapping == nil {
		w.FieldMapping = map[string]string{}
	}
	return &w, nil
}

func (r *webhookRepo) Disable(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE webhooks SET enabled = FALSE WHERE id = $1`, id)
	return err
}

func (r *webhookRepo) EnqueueDelivery(ctx context.Context, d *domain.WebhookDelivery) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	if d.NextAttemptAt.IsZero() {
		d.NextAttemptAt = d.CreatedAt
	}
	if d.Status == "" {
		d.Status = "pending"
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO webhook_deliveries (id, webhook_id, event_type, payload, attempts, next_attempt_at, status, last_response, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, d.ID, d.WebhookID, d.EventType, []byte(d.Payload), d.Attempts, d.NextAttemptAt, d.Status, d.LastResponse, d.CreatedAt)
	return err
}

func (r *webhookRepo) DequeuePending(ctx context.Context, before time.Time, limit int) ([]domain.WebhookDelivery, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, webhook_id, event_type, payload, attempts, next_attempt_at, status, last_response, created_at
		FROM webhook_deliveries
		WHERE status = 'pending' AND next_attempt_at <= $1
		ORDER BY next_attempt_at LIMIT $2
	`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.WebhookDelivery, 0)
	for rows.Next() {
		var (
			d       domain.WebhookDelivery
			payload []byte
		)
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.EventType, &payload, &d.Attempts, &d.NextAttemptAt, &d.Status, &d.LastResponse, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Payload = payload
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *webhookRepo) UpdateDeliveryAttempt(ctx context.Context, id uuid.UUID, attempts int, status, lastResponse string, nextAttempt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET attempts = $2, status = $3, last_response = $4, next_attempt_at = $5
		WHERE id = $1
	`, id, attempts, status, lastResponse, nextAttempt)
	return err
}

func (r *webhookRepo) ListDeliveries(ctx context.Context, limit int) ([]domain.WebhookDelivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, webhook_id, event_type, payload, attempts, next_attempt_at, status, last_response, created_at
		FROM webhook_deliveries ORDER BY created_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.WebhookDelivery, 0)
	for rows.Next() {
		var (
			d       domain.WebhookDelivery
			payload []byte
		)
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.EventType, &payload, &d.Attempts, &d.NextAttemptAt, &d.Status, &d.LastResponse, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Payload = payload
		out = append(out, d)
	}
	return out, rows.Err()
}
