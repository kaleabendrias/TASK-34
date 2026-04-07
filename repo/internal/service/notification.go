package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/repository"
)

type NotificationService struct {
	repo repository.NotificationRepository
	log  *slog.Logger
}

func NewNotificationService(repo repository.NotificationRepository, log *slog.Logger) *NotificationService {
	return &NotificationService{repo: repo, log: log}
}

// Notify creates an in-app notification and logs the delivery for the admin
// audit log. Channels other than 'in_app' are simulated; the row records the
// attempted delivery.
func (s *NotificationService) Notify(ctx context.Context, userID uuid.UUID, kind, title, body string) (*domain.Notification, error) {
	n := &domain.Notification{UserID: userID, Kind: kind, Title: title, Body: body}
	if err := s.repo.CreateNotification(ctx, n); err != nil {
		return nil, err
	}
	_ = s.repo.LogDelivery(ctx, &domain.NotificationDelivery{
		NotificationID: &n.ID,
		UserID:         &userID,
		Channel:        "in_app",
		Status:         "delivered",
		Detail:         "stored in notifications table",
	})
	return n, nil
}

func (s *NotificationService) List(ctx context.Context, userID uuid.UUID, unreadOnly bool, limit int) ([]domain.Notification, error) {
	return s.repo.ListNotifications(ctx, userID, unreadOnly, limit)
}

func (s *NotificationService) UnreadCount(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.repo.CountUnread(ctx, userID)
}

func (s *NotificationService) MarkRead(ctx context.Context, userID, id uuid.UUID) error {
	return s.repo.MarkRead(ctx, userID, id)
}

// CreateTodo schedules an actionable task in the user's To-Do Center.
func (s *NotificationService) CreateTodo(ctx context.Context, userID uuid.UUID, taskType, title string, payload any, due *time.Time) (*domain.Todo, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	t := &domain.Todo{
		UserID:   userID,
		TaskType: taskType,
		Title:    title,
		Payload:  raw,
		Status:   domain.TodoOpen,
		DueAt:    due,
	}
	if err := s.repo.CreateTodo(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *NotificationService) ListTodos(ctx context.Context, userID uuid.UUID, status string, limit int) ([]domain.Todo, error) {
	return s.repo.ListTodos(ctx, userID, status, limit)
}

func (s *NotificationService) UpdateTodoStatus(ctx context.Context, userID, id uuid.UUID, status domain.TodoStatus) error {
	return s.repo.UpdateTodoStatus(ctx, userID, id, status)
}

func (s *NotificationService) AdminDeliveries(ctx context.Context, limit int) ([]domain.NotificationDelivery, error) {
	return s.repo.ListDeliveries(ctx, limit)
}
