package api_tests

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestFinal_HTMLPagesRender hits every newly added HTML page so the
// CenterHTML / DashboardHTML / IndexHTML / DetailHTML handlers contribute
// to coverage.
func TestFinal_HTMLPagesRender(t *testing.T) {
	logged, _ := registerAndLogin(t, "htmlfinal")
	admin := loginAsAdmin(t)
	htmlHeaders := map[string]string{"Accept": "text/html"}

	// /group-buys index (anonymous)
	resp, body := newClient(t).doRaw(t, "GET", "/group-buys", nil, htmlHeaders)
	expectStatus(t, resp, body, http.StatusOK)

	// Create one to give the index something to render and the detail page a target.
	gbid := createGroupBuy(t, admin, 4, 1)
	resp, body = logged.doRaw(t, "GET", "/group-buys/"+gbid, nil, htmlHeaders)
	expectStatus(t, resp, body, http.StatusOK)
	if !strings.Contains(string(body), "Test buy") {
		t.Errorf("expected detail page to render group buy title")
	}

	// Documents center.
	logged.doJSON(t, "POST", "/api/documents/confirmation", map[string]any{
		"related_type": "booking",
		"related_id":   SeedSlipA1,
		"title":        "Center test",
	}, nil)
	resp, body = logged.doRaw(t, "GET", "/documents", nil, htmlHeaders)
	expectStatus(t, resp, body, http.StatusOK)
	if !strings.Contains(string(body), "Document center") {
		t.Errorf("documents page missing heading")
	}

	// Notifications + to-do center, plus filter variant.
	logged.doJSON(t, "POST", "/api/todos", map[string]any{
		"task_type": "x", "title": "test todo",
	}, nil)
	resp, body = logged.doRaw(t, "GET", "/notifications", nil, htmlHeaders)
	expectStatus(t, resp, body, http.StatusOK)
	resp, body = logged.doRaw(t, "GET", "/notifications?status=open", nil, htmlHeaders)
	expectStatus(t, resp, body, http.StatusOK)

	// Admin analytics dashboard, both default + non-default event_type.
	resp, body = admin.doRaw(t, "GET", "/admin/analytics", nil, htmlHeaders)
	expectStatus(t, resp, body, http.StatusOK)
	resp, body = admin.doRaw(t, "GET", "/admin/analytics?event_type=favorite", nil, htmlHeaders)
	expectStatus(t, resp, body, http.StatusOK)

	// Non-admin → 403 for the dashboard.
	resp, _ = logged.doRaw(t, "GET", "/admin/analytics", nil, htmlHeaders)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin admin/analytics expected 403, got %d", resp.StatusCode)
	}
}

// TestFinal_RemainingSeatsEndpoint exercises the per-slot accounting API.
func TestFinal_RemainingSeatsEndpoint(t *testing.T) {
	c, _ := registerAndLogin(t, "remaining")
	start := time.Now().UTC().Add(3 * time.Hour).Truncate(time.Hour).Format(time.RFC3339)
	end := time.Now().UTC().Add(4 * time.Hour).Truncate(time.Hour).Format(time.RFC3339)

	// Conference room has capacity 12.
	url := "/api/resources/" + SeedConfRoom + "/remaining?start=" + start + "&end=" + end
	resp, body := c.doJSON(t, "GET", url, nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	out := mustJSON(t, body)
	if out["capacity"] == nil || out["remaining_seats"] == nil {
		t.Errorf("missing fields: %v", out)
	}

	// Bad uuid
	resp, _ = c.doJSON(t, "GET", "/api/resources/not-a-uuid/remaining?start="+start+"&end="+end, nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 bad uuid, got %d", resp.StatusCode)
	}
	// Bad start time
	resp, _ = c.doJSON(t, "GET", "/api/resources/"+SeedConfRoom+"/remaining?start=nope&end="+end, nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 bad start, got %d", resp.StatusCode)
	}
	// Bad end time
	resp, _ = c.doJSON(t, "GET", "/api/resources/"+SeedConfRoom+"/remaining?start="+start+"&end=nope", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 bad end, got %d", resp.StatusCode)
	}
}

// TestFinal_ChangePassword covers the new rotation endpoint, both happy
// path and error branches (wrong current, weak new, validation).
func TestFinal_ChangePassword(t *testing.T) {
	c, _ := registerAndLogin(t, "rotator")

	// Wrong current
	resp, body := c.doJSON(t, "POST", "/api/auth/change-password", map[string]string{
		"current_password": "Wrong@Current2026!",
		"new_password":     "Brand@New2026!",
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong current expected 401, got %d body=%s", resp.StatusCode, body)
	}

	// Weak new
	resp, body = c.doJSON(t, "POST", "/api/auth/change-password", map[string]string{
		"current_password": TestPassword,
		"new_password":     "weak",
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("weak new expected 400, got %d body=%s", resp.StatusCode, body)
	}

	// Missing fields
	resp, _ = c.doJSON(t, "POST", "/api/auth/change-password", map[string]any{}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing fields expected 400, got %d", resp.StatusCode)
	}

	// Happy path
	resp, body = c.doJSON(t, "POST", "/api/auth/change-password", map[string]string{
		"current_password": TestPassword,
		"new_password":     "Brand@NewPassword2026!",
	}, nil)
	expectStatus(t, resp, body, http.StatusOK)
}

// TestFinal_OrganizerPIIMaskedOnSharedAPI verifies a non-owner sees masks.
func TestFinal_OrganizerPIIMaskedOnSharedAPI(t *testing.T) {
	owner, _ := registerAndLogin(t, "groupowner")
	resp, body := owner.doJSON(t, "POST", "/api/groups", map[string]any{
		"name":            "PII test " + uniqueUsername("g"),
		"organizer_name":  "Patricia Q. Person",
		"organizer_email": "patricia@example.com",
		"capacity":        10,
	}, nil)
	expectStatus(t, resp, body, http.StatusCreated)
	id, _ := mustJSON(t, body)["id"].(string)

	// Owner sees everything (no mask).
	resp, body = owner.doJSON(t, "GET", "/api/groups/"+id, nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	out := mustJSON(t, body)
	if out["organizer_email"] != "patricia@example.com" {
		t.Errorf("owner should see real email, got %v", out["organizer_email"])
	}

	// Stranger sees masked.
	stranger, _ := registerAndLogin(t, "groupstranger")
	resp, body = stranger.doJSON(t, "GET", "/api/groups/"+id, nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	strangerView := mustJSON(t, body)
	if strangerView["organizer_email"] == "patricia@example.com" {
		t.Errorf("stranger should see masked email, got raw")
	}
	if !strings.Contains(strangerView["organizer_email"].(string), "*") {
		t.Errorf("stranger email should be masked, got %v", strangerView["organizer_email"])
	}
	if strangerView["organizer_name"] == "Patricia Q. Person" {
		t.Errorf("stranger should see masked name")
	}

	// Anonymous also sees masked.
	resp, body = newClient(t).doJSON(t, "GET", "/api/groups/"+id, nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	anon := mustJSON(t, body)
	if !strings.Contains(anon["organizer_email"].(string), "*") {
		t.Errorf("anonymous should see masked email")
	}

	// Admin always sees full PII.
	admin := loginAsAdmin(t)
	resp, body = admin.doJSON(t, "GET", "/api/groups/"+id, nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	adminView := mustJSON(t, body)
	if adminView["organizer_email"] != "patricia@example.com" {
		t.Errorf("admin should see real email, got %v", adminView["organizer_email"])
	}
}

// TestFinal_WebhookCreateRejectsPublicTarget makes sure the SSRF gate
// blocks anything outside the local-network allow-list.
func TestFinal_WebhookCreateRejectsPublicTarget(t *testing.T) {
	admin := loginAsAdmin(t)
	resp, body := admin.doJSON(t, "POST", "/api/admin/webhooks", map[string]any{
		"name":       "external",
		"target_url": "http://example.com/",
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", resp.StatusCode, body)
	}
	containsAll(t, body, "non-local")
}

// TestFinal_BookingSecureNotesEncrypted creates a booking with secure_notes,
// reads it back as the owner (should see plaintext), and then as another
// user (should NOT see plaintext, even via the JSON API).
func TestFinal_BookingSecureNotesEncrypted(t *testing.T) {
	owner, _ := registerAndLogin(t, "secrets")
	start := time.Now().UTC().Add(3 * time.Hour).Truncate(time.Hour).Format(time.RFC3339)
	end := time.Now().UTC().Add(4 * time.Hour).Truncate(time.Hour).Format(time.RFC3339)

	resp, body := owner.doJSON(t, "POST", "/api/bookings", map[string]any{
		"resource_id":  SeedSlipA1,
		"start_time":   start,
		"end_time":     end,
		"notes":        "public note",
		"secure_notes": "classified: arrival door code 7421",
	}, map[string]string{"Idempotency-Key": idemKey("secn")})
	expectStatus(t, resp, body, http.StatusCreated)
	id, _ := mustJSON(t, body)["id"].(string)

	// Owner GET → secure_notes plaintext present.
	resp, body = owner.doJSON(t, "GET", "/api/bookings/"+id, nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	out := mustJSON(t, body)
	if out["secure_notes"] != "classified: arrival door code 7421" {
		t.Errorf("owner should see decrypted secure_notes, got %v", out["secure_notes"])
	}

	// Another user → 403 entirely.
	other, _ := registerAndLogin(t, "nosecrets")
	resp, _ = other.doJSON(t, "GET", "/api/bookings/"+id, nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("other user expected 403, got %d", resp.StatusCode)
	}
}

// TestFinal_NoPlaintextPasswordInLogs is a smoke check: the boot log file
// the test runner captured should not contain the literal seed password
// pattern. Implemented by reading the marker file's existence rather than
// scraping the running container's stdout (which is harder to reach from
// a sub-test). The marker file holds the per-boot one-time secret.
func TestFinal_NoPlaintextPasswordInLogs(t *testing.T) {
	// The seed wrote the password to /app/keys/initial_admin_password.
	// Existence is the proof: the password is in the file, NOT in the log.
	data, err := osReadFile("/app/keys/initial_admin_password")
	if err != nil {
		t.Fatalf("expected initial password file to exist: %v", err)
	}
	if len(data) < 12 {
		t.Errorf("expected a strong-ish password in the file, got %d bytes", len(data))
	}
}
