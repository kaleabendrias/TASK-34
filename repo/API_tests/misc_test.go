package api_tests

import (
	"net/http"
	"strings"
	"testing"
)

func TestHealth_LivenessAndReadiness(t *testing.T) {
	c := newClient(t)
	resp, body := c.doJSON(t, "GET", "/healthz", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	resp, body = c.doJSON(t, "GET", "/readyz", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
}

func TestResources_ListAndAvailability(t *testing.T) {
	c := newClient(t)
	resp, body := c.doJSON(t, "GET", "/api/resources", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if cnt, _ := mustJSON(t, body)["count"].(float64); cnt < 1 {
		t.Errorf("expected resources, got %v", cnt)
	}

	// availability with date in seed range
	resp, body = c.doJSON(t, "GET", "/api/availability?resource_id="+SeedSlipA1+"&date=2026-04-08", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)

	// missing resource_id
	resp, _ = c.doJSON(t, "GET", "/api/availability", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	// bad uuid
	resp, _ = c.doJSON(t, "GET", "/api/availability?resource_id=not-a-uuid", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	// bad date
	resp, _ = c.doJSON(t, "GET", "/api/availability?resource_id="+SeedSlipA1+"&date=nope", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGroups_CRUD(t *testing.T) {
	// Group create now requires authentication so the organiser-of-record
	// is recorded for PII masking gates.
	c, _ := registerAndLogin(t, "groupcrud")

	// Create
	resp, body := c.doJSON(t, "POST", "/api/groups", map[string]any{
		"name":            "Test group " + uniqueUsername("g"),
		"organizer_name":  "Tester",
		"organizer_email": "tester@example.com",
		"capacity":        20,
		"notes":           "auto-generated",
	}, nil)
	expectStatus(t, resp, body, http.StatusCreated)
	id, _ := mustJSON(t, body)["id"].(string)

	// Bad input
	resp, _ = c.doJSON(t, "POST", "/api/groups", map[string]any{}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad input expected 400, got %d", resp.StatusCode)
	}

	// List
	resp, body = c.doJSON(t, "GET", "/api/groups", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)

	// Get
	resp, body = c.doJSON(t, "GET", "/api/groups/"+id, nil, nil)
	expectStatus(t, resp, body, http.StatusOK)

	// Get bad id
	resp, _ = c.doJSON(t, "GET", "/api/groups/not-a-uuid", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGroups_HTML(t *testing.T) {
	c := newClient(t)
	for _, p := range []string{"/groups", "/availability"} {
		resp, body := c.doRaw(t, "GET", p, nil, map[string]string{"Accept": "text/html"})
		expectStatus(t, resp, body, http.StatusOK)
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
			t.Errorf("%s content-type wrong", p)
		}
	}
}

func TestAuth_MeRequiresLogin(t *testing.T) {
	c := newClient(t)
	resp, _ := c.doJSON(t, "GET", "/api/auth/me", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAuth_CaptchaEndpoint(t *testing.T) {
	c := newClient(t)
	resp, body := c.doJSON(t, "GET", "/api/auth/captcha", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	out := mustJSON(t, body)
	if _, ok := out["token"]; !ok {
		t.Errorf("missing token")
	}
	if _, ok := out["question"]; !ok {
		t.Errorf("missing question")
	}
}

func TestNotFound(t *testing.T) {
	c := newClient(t)
	resp, _ := c.doJSON(t, "GET", "/api/group-buys/00000000-0000-0000-0000-000000000000", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
