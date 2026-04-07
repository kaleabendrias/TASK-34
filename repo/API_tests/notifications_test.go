package api_tests

import (
	"net/http"
	"testing"
)

func TestNotifications_TodosLifecycle(t *testing.T) {
	c, _ := registerAndLogin(t, "todoer")

	resp, body := c.doJSON(t, "POST", "/api/todos", map[string]any{
		"task_type": "confirm_booking",
		"title":     "Confirm cruise",
		"payload":   map[string]string{"resource": "Slip A1"},
	}, nil)
	expectStatus(t, resp, body, http.StatusCreated)
	created := mustJSON(t, body)
	todoID, _ := created["id"].(string)

	// List open
	resp, body = c.doJSON(t, "GET", "/api/todos?status=open", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)

	// List all
	resp, body = c.doJSON(t, "GET", "/api/todos", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)

	// Move to in_progress
	resp, body = c.doJSON(t, "POST", "/api/todos/"+todoID+"/status", map[string]string{
		"status": "in_progress",
	}, nil)
	expectStatus(t, resp, body, http.StatusOK)

	// Move to done
	resp, body = c.doJSON(t, "POST", "/api/todos/"+todoID+"/status", map[string]string{
		"status": "done",
	}, nil)
	expectStatus(t, resp, body, http.StatusOK)
}

func TestNotifications_UnreadCountAndList(t *testing.T) {
	c, _ := registerAndLogin(t, "notifuser")

	resp, body := c.doJSON(t, "GET", "/api/notifications", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	resp, body = c.doJSON(t, "GET", "/api/notifications?unread=1", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	resp, body = c.doJSON(t, "GET", "/api/notifications/unread-count", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if _, ok := mustJSON(t, body)["unread"]; !ok {
		t.Errorf("missing unread field")
	}
}

func TestNotifications_AdminDeliveryLog(t *testing.T) {
	admin := loginAsAdmin(t)
	resp, body := admin.doJSON(t, "GET", "/api/admin/notification-deliveries", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
}

func TestNotifications_NonAdminBlockedFromDeliveryLog(t *testing.T) {
	user, _ := registerAndLogin(t, "notlog")
	resp, _ := user.doJSON(t, "GET", "/api/admin/notification-deliveries", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestNotifications_MarkReadNotFound(t *testing.T) {
	c, _ := registerAndLogin(t, "marker")
	resp, _ := c.doJSON(t, "POST", "/api/notifications/00000000-0000-0000-0000-000000000000/read", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
