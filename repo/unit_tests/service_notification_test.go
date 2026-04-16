package unit_tests

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/harborworks/booking-hub/internal/domain"
	"github.com/harborworks/booking-hub/internal/service"
)

func newTestNotificationService(repo *mockNotifRepo) *service.NotificationService {
	return service.NewNotificationService(repo, slog.Default())
}

// ─── Notify ──────────────────────────────────────────────────────────────────

func TestNotify_CreatesNotificationAndDelivery(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	userID := uuid.New()

	n, err := svc.Notify(context.Background(), userID, "booking_confirmed", "Confirmed", "Your booking is confirmed.")
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if n.ID == uuid.Nil {
		t.Error("notification should have a non-nil ID")
	}
	if n.Kind != "booking_confirmed" {
		t.Errorf("kind: want booking_confirmed, got %s", n.Kind)
	}
	if n.Title != "Confirmed" {
		t.Errorf("title: want Confirmed, got %s", n.Title)
	}

	// Delivery log should have one entry.
	repo.mu.Lock()
	ndel := len(repo.deliveries)
	repo.mu.Unlock()
	if ndel != 1 {
		t.Errorf("expected 1 delivery log entry, got %d", ndel)
	}
}

func TestNotify_RepoError_Propagated(t *testing.T) {
	repo := newMockNotifRepo()
	repo.createNotifErr = domain.ErrInvalidInput
	svc := newTestNotificationService(repo)

	_, err := svc.Notify(context.Background(), uuid.New(), "kind", "title", "body")
	if err == nil {
		t.Fatal("expected error from repo")
	}
}

// ─── List ────────────────────────────────────────────────────────────────────

func TestList_AllNotifications(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()

	svc.Notify(context.Background(), uid, "k1", "T1", "B1")
	svc.Notify(context.Background(), uid, "k2", "T2", "B2")

	list, err := svc.List(context.Background(), uid, false, 50)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 notifications, got %d", len(list))
	}
}

func TestList_UnreadOnly(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()

	svc.Notify(context.Background(), uid, "k1", "T1", "B1")
	svc.Notify(context.Background(), uid, "k2", "T2", "B2")

	// Mark one as read.
	list, _ := svc.List(context.Background(), uid, false, 50)
	svc.MarkRead(context.Background(), uid, list[0].ID)

	unread, err := svc.List(context.Background(), uid, true, 50)
	if err != nil {
		t.Fatalf("List unread: %v", err)
	}
	if len(unread) != 1 {
		t.Errorf("expected 1 unread, got %d", len(unread))
	}
}

// ─── UnreadCount ─────────────────────────────────────────────────────────────

func TestUnreadCount_CountsOnlyUnread(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()

	svc.Notify(context.Background(), uid, "k1", "T1", "B1")
	svc.Notify(context.Background(), uid, "k2", "T2", "B2")
	svc.Notify(context.Background(), uid, "k3", "T3", "B3")

	// Mark first as read.
	list, _ := svc.List(context.Background(), uid, false, 50)
	svc.MarkRead(context.Background(), uid, list[0].ID)

	count, err := svc.UnreadCount(context.Background(), uid)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 unread, got %d", count)
	}
}

func TestUnreadCount_ZeroForOtherUser(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()
	otherUID := uuid.New()

	svc.Notify(context.Background(), uid, "k1", "T1", "B1")

	count, err := svc.UnreadCount(context.Background(), otherUID)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 for other user, got %d", count)
	}
}

// ─── MarkRead ────────────────────────────────────────────────────────────────

func TestMarkRead_MarksCorrectNotification(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()

	n, _ := svc.Notify(context.Background(), uid, "k1", "T1", "B1")

	if err := svc.MarkRead(context.Background(), uid, n.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	count, _ := svc.UnreadCount(context.Background(), uid)
	if count != 0 {
		t.Errorf("expected 0 unread after mark, got %d", count)
	}
}

// ─── CreateTodo ──────────────────────────────────────────────────────────────

func TestCreateTodo_Success(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()

	todo, err := svc.CreateTodo(context.Background(), uid, "confirm_booking", "Confirm booking", map[string]string{"resource": "Slip A1"}, nil)
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}
	if todo.ID == uuid.Nil {
		t.Error("expected non-nil todo ID")
	}
	if todo.Status != domain.TodoOpen {
		t.Errorf("expected open status, got %s", todo.Status)
	}
	if todo.TaskType != "confirm_booking" {
		t.Errorf("task_type: want confirm_booking, got %s", todo.TaskType)
	}
}

func TestCreateTodo_WithDueDate(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()
	due := time.Now().UTC().Add(24 * time.Hour)

	todo, err := svc.CreateTodo(context.Background(), uid, "review", "Review docs", nil, &due)
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}
	if todo.DueAt == nil {
		t.Error("expected DueAt to be set")
	}
}

func TestCreateTodo_NilPayload(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()

	todo, err := svc.CreateTodo(context.Background(), uid, "task", "My task", nil, nil)
	if err != nil {
		t.Fatalf("CreateTodo with nil payload: %v", err)
	}
	if todo.ID == uuid.Nil {
		t.Error("expected non-nil ID")
	}
}

func TestCreateTodo_PayloadIsMarshalled(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()
	payload := map[string]any{"key": "value", "num": 42}

	todo, err := svc.CreateTodo(context.Background(), uid, "task", "My task", payload, nil)
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(todo.Payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("payload key: want value, got %v", got["key"])
	}
}

// ─── ListTodos ────────────────────────────────────────────────────────────────

func TestListTodos_AllStatuses(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()

	svc.CreateTodo(context.Background(), uid, "t1", "Task 1", nil, nil)
	svc.CreateTodo(context.Background(), uid, "t2", "Task 2", nil, nil)

	list, err := svc.ListTodos(context.Background(), uid, "", 50)
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 todos, got %d", len(list))
	}
}

func TestListTodos_FilterByStatus(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()

	todo, _ := svc.CreateTodo(context.Background(), uid, "t1", "Task 1", nil, nil)
	svc.CreateTodo(context.Background(), uid, "t2", "Task 2", nil, nil)

	// Mark one as done.
	svc.UpdateTodoStatus(context.Background(), uid, todo.ID, domain.TodoDone)

	openList, _ := svc.ListTodos(context.Background(), uid, string(domain.TodoOpen), 50)
	if len(openList) != 1 {
		t.Errorf("expected 1 open todo, got %d", len(openList))
	}
	doneList, _ := svc.ListTodos(context.Background(), uid, string(domain.TodoDone), 50)
	if len(doneList) != 1 {
		t.Errorf("expected 1 done todo, got %d", len(doneList))
	}
}

// ─── UpdateTodoStatus ─────────────────────────────────────────────────────────

func TestUpdateTodoStatus_Success(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()

	todo, _ := svc.CreateTodo(context.Background(), uid, "t1", "Task 1", nil, nil)

	if err := svc.UpdateTodoStatus(context.Background(), uid, todo.ID, domain.TodoDone); err != nil {
		t.Fatalf("UpdateTodoStatus: %v", err)
	}
	list, _ := svc.ListTodos(context.Background(), uid, string(domain.TodoDone), 50)
	if len(list) != 1 {
		t.Errorf("expected 1 done todo after update, got %d", len(list))
	}
}

func TestUpdateTodoStatus_WrongUser_NotFound(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()
	otherUID := uuid.New()

	todo, _ := svc.CreateTodo(context.Background(), uid, "t1", "Task 1", nil, nil)

	err := svc.UpdateTodoStatus(context.Background(), otherUID, todo.ID, domain.TodoDone)
	if err == nil {
		t.Fatal("expected error when updating todo owned by different user")
	}
}

// ─── AdminDeliveries ──────────────────────────────────────────────────────────

func TestAdminDeliveries_ReturnsDeliveryLog(t *testing.T) {
	repo := newMockNotifRepo()
	svc := newTestNotificationService(repo)
	uid := uuid.New()

	svc.Notify(context.Background(), uid, "k1", "T1", "B1")
	svc.Notify(context.Background(), uid, "k2", "T2", "B2")

	deliveries, err := svc.AdminDeliveries(context.Background(), 50)
	if err != nil {
		t.Fatalf("AdminDeliveries: %v", err)
	}
	if len(deliveries) != 2 {
		t.Errorf("expected 2 deliveries, got %d", len(deliveries))
	}
}
