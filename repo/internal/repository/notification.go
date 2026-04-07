package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harborworks/booking-hub/internal/domain"
)

type NotificationRepository interface {
	CreateNotification(ctx context.Context, n *domain.Notification) error
	ListNotifications(ctx context.Context, userID uuid.UUID, unreadOnly bool, limit int) ([]domain.Notification, error)
	MarkRead(ctx context.Context, userID, id uuid.UUID) error
	CountUnread(ctx context.Context, userID uuid.UUID) (int, error)

	CreateTodo(ctx context.Context, t *domain.Todo) error
	ListTodos(ctx context.Context, userID uuid.UUID, status string, limit int) ([]domain.Todo, error)
	UpdateTodoStatus(ctx context.Context, userID, id uuid.UUID, status domain.TodoStatus) error

	LogDelivery(ctx context.Context, d *domain.NotificationDelivery) error
	ListDeliveries(ctx context.Context, limit int) ([]domain.NotificationDelivery, error)
}

type notificationRepo struct{ pool *pgxpool.Pool }

func NewNotificationRepository(pool *pgxpool.Pool) NotificationRepository {
	return &notificationRepo{pool: pool}
}

func (r *notificationRepo) CreateNotification(ctx context.Context, n *domain.Notification) error {
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO notifications (id, user_id, kind, title, body, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, n.ID, n.UserID, n.Kind, n.Title, n.Body, n.CreatedAt)
	return err
}

func (r *notificationRepo) ListNotifications(ctx context.Context, userID uuid.UUID, unreadOnly bool, limit int) ([]domain.Notification, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, user_id, kind, title, body, read_at, created_at FROM notifications WHERE user_id = $1`
	if unreadOnly {
		q += ` AND read_at IS NULL`
	}
	q += ` ORDER BY created_at DESC LIMIT $2`
	rows, err := r.pool.Query(ctx, q, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Notification, 0)
	for rows.Next() {
		var n domain.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, &n.Title, &n.Body, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *notificationRepo) MarkRead(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE notifications SET read_at = NOW() WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *notificationRepo) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	var c int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&c); err != nil {
		return 0, err
	}
	return c, nil
}

func (r *notificationRepo) CreateTodo(ctx context.Context, t *domain.Todo) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.Status == "" {
		t.Status = domain.TodoOpen
	}
	if len(t.Payload) == 0 {
		t.Payload = json.RawMessage(`{}`)
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO todos (id, user_id, task_type, title, payload, status, due_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, t.ID, t.UserID, t.TaskType, t.Title, []byte(t.Payload), string(t.Status), t.DueAt, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert todo: %w", err)
	}
	return nil
}

func (r *notificationRepo) ListTodos(ctx context.Context, userID uuid.UUID, status string, limit int) ([]domain.Todo, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT id, user_id, task_type, title, payload, status, due_at, created_at, updated_at FROM todos WHERE user_id = $1`
	args := []any{userID}
	if status != "" {
		q += ` AND status = $2`
		args = append(args, status)
		q += ` ORDER BY created_at DESC LIMIT $3`
		args = append(args, limit)
	} else {
		q += ` ORDER BY created_at DESC LIMIT $2`
		args = append(args, limit)
	}
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Todo, 0)
	for rows.Next() {
		var (
			t       domain.Todo
			payload []byte
			st      string
		)
		if err := rows.Scan(&t.ID, &t.UserID, &t.TaskType, &t.Title, &payload, &st, &t.DueAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.Payload = json.RawMessage(payload)
		t.Status = domain.TodoStatus(st)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *notificationRepo) UpdateTodoStatus(ctx context.Context, userID, id uuid.UUID, status domain.TodoStatus) error {
	tag, err := r.pool.Exec(ctx, `UPDATE todos SET status = $3, updated_at = NOW() WHERE id = $1 AND user_id = $2`, id, userID, string(status))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *notificationRepo) LogDelivery(ctx context.Context, d *domain.NotificationDelivery) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	if d.DeliveredAt.IsZero() {
		d.DeliveredAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO notification_deliveries (id, notification_id, user_id, channel, status, detail, delivered_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, d.ID, d.NotificationID, d.UserID, d.Channel, d.Status, d.Detail, d.DeliveredAt)
	return err
}

func (r *notificationRepo) ListDeliveries(ctx context.Context, limit int) ([]domain.NotificationDelivery, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, notification_id, user_id, channel, status, detail, delivered_at
		FROM notification_deliveries ORDER BY delivered_at DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.NotificationDelivery, 0)
	for rows.Next() {
		var d domain.NotificationDelivery
		if err := rows.Scan(&d.ID, &d.NotificationID, &d.UserID, &d.Channel, &d.Status, &d.Detail, &d.DeliveredAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
