package api_tests

import (
	"net/http"
	"testing"
)

// TestErrorBranches_NotFoundOnGetById walks every "GET /:id" endpoint with a
// well-formed UUID that doesn't exist, exercising the ErrNotFound branch in
// writeServiceError for each handler.
func TestErrorBranches_NotFoundOnGetById(t *testing.T) {
	const ghost = "00000000-0000-0000-0000-000000000000"
	logged, _ := registerAndLogin(t, "ghost")
	admin := loginAsAdmin(t)

	cases := []struct {
		client *Client
		method string
		path   string
		want   int
	}{
		{logged, "GET", "/api/bookings/" + ghost, http.StatusNotFound},
		{newClient(t), "GET", "/api/groups/" + ghost, http.StatusNotFound},
		{logged, "GET", "/api/group-buys/" + ghost, http.StatusNotFound},
		{logged, "GET", "/api/group-buys/" + ghost + "/progress", http.StatusNotFound},
		{logged, "GET", "/api/group-buys/" + ghost + "/participants", http.StatusOK}, // empty list
		{logged, "GET", "/api/documents/" + ghost, http.StatusNotFound},
		{logged, "GET", "/api/documents/" + ghost + "/content", http.StatusNotFound},
		{admin, "POST", "/api/bookings/" + ghost + "/transition", http.StatusNotFound},
	}
	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			var resp *http.Response
			if c.method == "POST" {
				resp, _ = c.client.doJSON(t, c.method, c.path, map[string]string{"target_state": "checked_in"}, nil)
			} else {
				resp, _ = c.client.doJSON(t, c.method, c.path, nil, nil)
			}
			if resp.StatusCode != c.want {
				t.Errorf("expected %d, got %d", c.want, resp.StatusCode)
			}
		})
	}
}

// TestErrorBranches_BadJSONOnEveryPost hits the ShouldBindJSON / ShouldBind
// failure branch on every POST endpoint that decodes a body. Some endpoints
// also enforce required-field validation and reject empty objects with 400.
func TestErrorBranches_BadJSONOnEveryPost(t *testing.T) {
	logged, _ := registerAndLogin(t, "badjson")
	admin := loginAsAdmin(t)

	posts := []struct {
		client *Client
		path   string
	}{
		{newClient(t), "/api/auth/register"},
		{newClient(t), "/api/auth/login"},
		{logged, "/api/bookings"},
		{newClient(t), "/api/groups"},
		{logged, "/api/group-buys"},
		{logged, "/api/documents/confirmation"},
		{logged, "/api/documents/checkin-pass"},
		{logged, "/api/todos"},
		{logged, "/api/consent/grant"},
		{logged, "/api/consent/withdraw"},
		{logged, "/api/analytics/track"},
		{admin, "/api/admin/webhooks"},
	}
	// Sending an empty object hits the required-field validator branch on
	// every endpoint and is therefore a uniform "400 expected" probe.
	for _, p := range posts {
		t.Run(p.path, func(t *testing.T) {
			resp, body := p.client.doJSON(t, "POST", p.path, map[string]any{}, nil)
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("%s expected 400, got %d body=%s", p.path, resp.StatusCode, body)
			}
		})
	}
}

// TestErrorBranches_CaptchaInvalid drives the wrong-captcha path in
// writeAuthError → captcha incorrect (needs a fresh captcha minted, then we
// answer wrong).
func TestErrorBranches_CaptchaInvalid(t *testing.T) {
	c, username := registerAndLogin(t, "wrongcap")

	// Drive 2 failures so the next attempt requires captcha.
	for i := 0; i < 2; i++ {
		c.doJSON(t, "POST", "/api/auth/login", map[string]string{
			"username": username, "password": "wrong-1A!",
		}, nil)
	}

	// Mint a captcha then submit a wrong answer.
	resp, body := c.doJSON(t, "GET", "/api/auth/captcha", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	ch := mustJSON(t, body)
	token, _ := ch["token"].(string)

	resp, body = c.doJSON(t, "POST", "/api/auth/login", map[string]any{
		"username":       username,
		"password":       "wrong-1A!",
		"captcha_token":  token,
		"captcha_answer": "999999", // wrong
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong captcha expected 401, got %d", resp.StatusCode)
	}
	containsAll(t, body, "captcha")
}

// TestErrorBranches_DocumentRevisionNotFound hits the GetRevision-not-found
// branch in document Content handler.
func TestErrorBranches_DocumentRevisionNotFound(t *testing.T) {
	c, _ := registerAndLogin(t, "revfind")
	// Create a document
	resp, body := c.doJSON(t, "POST", "/api/documents/confirmation", map[string]any{
		"related_type": "booking",
		"related_id":   SeedSlipA1,
		"title":        "Rev test",
	}, nil)
	expectStatus(t, resp, body, http.StatusCreated)
	docMeta := mustJSON(t, body)
	doc, _ := docMeta["document"].(map[string]any)
	id, _ := doc["id"].(string)

	resp, _ = c.doJSON(t, "GET", "/api/documents/"+id+"/content?revision=99", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// TestErrorBranches_InvalidConsentScope sends an empty scope to hit the
// validation branch.
func TestErrorBranches_InvalidConsentScope(t *testing.T) {
	c, _ := registerAndLogin(t, "consentbad")
	resp, _ := c.doJSON(t, "POST", "/api/consent/grant", map[string]string{}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
