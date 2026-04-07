package api_tests

import (
	"net/http"
	"testing"
)

func TestWebhook_AdminCRUDAndDeliveriesEndpoint(t *testing.T) {
	admin := loginAsAdmin(t)

	// Create
	resp, body := admin.doJSON(t, "POST", "/api/admin/webhooks", map[string]any{
		"name":          "sink",
		"target_url":    "http://127.0.0.1:1/missing",
		"event_filter":  []string{"*"},
		"field_mapping": map[string]string{"id": "alias"},
		"secret":        "shhh",
	}, nil)
	expectStatus(t, resp, body, http.StatusCreated)
	w := mustJSON(t, body)
	id, _ := w["id"].(string)
	if id == "" {
		t.Fatal("missing id")
	}

	// List
	resp, body = admin.doJSON(t, "GET", "/api/admin/webhooks", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if cnt, _ := mustJSON(t, body)["count"].(float64); cnt < 1 {
		t.Errorf("expected at least 1 webhook, got %v", cnt)
	}

	// Disable
	resp, body = admin.doJSON(t, "POST", "/api/admin/webhooks/"+id+"/disable", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)

	// Deliveries listing (likely empty in tests, but endpoint must work)
	resp, body = admin.doJSON(t, "GET", "/api/admin/webhooks/deliveries?limit=5", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
}

func TestWebhook_NonAdminBlocked(t *testing.T) {
	user, _ := registerAndLogin(t, "nohook")
	resp, _ := user.doJSON(t, "POST", "/api/admin/webhooks", map[string]any{
		"name": "x", "target_url": "http://x",
	}, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	resp, _ = user.doJSON(t, "GET", "/api/admin/webhooks", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}
