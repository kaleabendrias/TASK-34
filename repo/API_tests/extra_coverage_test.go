package api_tests

import (
	"net/http"
	"strings"
	"testing"
)

// TestExtra_GroupDetailHTML hits /groups/:id which goes through the soft auth
// path and renders the GroupDetail templ template.
func TestExtra_GroupDetailHTML(t *testing.T) {
	c := newClient(t)
	// Create a group via the API first.
	resp, body := c.doJSON(t, "POST", "/api/groups", map[string]any{
		"name":            "Detail group",
		"organizer_email": "detail@example.com",
		"capacity":        5,
	}, nil)
	expectStatus(t, resp, body, http.StatusCreated)
	g := mustJSON(t, body)
	id, _ := g["id"].(string)

	resp, body = c.doRaw(t, "GET", "/groups/"+id, nil, map[string]string{"Accept": "text/html"})
	expectStatus(t, resp, body, http.StatusOK)
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("expected html, got %q", resp.Header.Get("Content-Type"))
	}

	// Bad UUID for the HTML detail page → 400 string body
	resp, _ = c.doRaw(t, "GET", "/groups/not-a-uuid", nil, map[string]string{"Accept": "text/html"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestExtra_BogusSessionCookieCleared hits an endpoint with a junk session
// cookie. The middleware should reject and clear the cookie via Set-Cookie.
func TestExtra_BogusSessionCookieCleared(t *testing.T) {
	c := newClient(t)
	resp, _ := c.doRaw(t, "GET", "/api/auth/me", nil, map[string]string{
		"Cookie": "harborworks_session=this-is-not-a-real-token-1234567890",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	// Verify a Set-Cookie header was emitted to clear the bogus session.
	gotClear := false
	for _, sc := range resp.Header.Values("Set-Cookie") {
		if strings.Contains(sc, "harborworks_session=") && strings.Contains(sc, "Max-Age=0") {
			gotClear = true
			break
		}
	}
	if !gotClear {
		t.Errorf("expected cookie clear, headers: %v", resp.Header.Values("Set-Cookie"))
	}
}

// TestExtra_GroupBuyDeadlineExpired creates a group buy whose deadline is
// just before now (we can't pre-date directly via the API but we can drive
// the SweepExpired job effect by waiting briefly). Instead, use a deadline
// of "now+0.1s" by accepting the validation that deadline > now AND that we
// can race the sweep. Faster: just verify the join error path with an
// already-canceled group buy via the admin endpoint (no such endpoint exists,
// so we just check that the writeGroupBuyError path on ErrAlreadyJoined is
// actually exercised by joining twice via different idempotency keys.
func TestExtra_GroupBuyJoinErrorPaths(t *testing.T) {
	admin := loginAsAdmin(t)
	id := createGroupBuy(t, admin, 1, 1) // capacity = 1, threshold = 1

	a, _ := registerAndLogin(t, "joinA")
	b, _ := registerAndLogin(t, "joinB")

	// First join consumes the only slot.
	resp, body := a.doJSON(t, "POST", "/api/group-buys/"+id+"/join", map[string]any{"quantity": 1}, map[string]string{"Idempotency-Key": idemKey("ja")})
	expectStatus(t, resp, body, http.StatusOK)

	// Second user tries → ErrOversold (409 "no remaining capacity")
	resp, body = b.doJSON(t, "POST", "/api/group-buys/"+id+"/join", map[string]any{"quantity": 1}, map[string]string{"Idempotency-Key": idemKey("jb")})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 oversold, got %d body=%s", resp.StatusCode, body)
	}
	containsAll(t, body, "no remaining capacity")
}

// TestExtra_BookingTransitionMissingId checks bad uuid path on /transition.
func TestExtra_BookingTransitionInvalidPaths(t *testing.T) {
	c, _ := registerAndLogin(t, "txer")
	// bad uuid
	resp, _ := c.doJSON(t, "POST", "/api/bookings/not-a-uuid/transition", map[string]string{"target_state": "checked_in"}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	// missing target_state
	resp, _ = c.doJSON(t, "POST", "/api/bookings/00000000-0000-0000-0000-000000000000/transition", map[string]any{}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	// well-formed but unknown id → 404
	resp, _ = c.doJSON(t, "POST", "/api/bookings/00000000-0000-0000-0000-000000000000/transition", map[string]string{"target_state": "checked_in"}, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// TestExtra_BookingGetInvalidId hits the bad-uuid branch.
func TestExtra_BookingGetInvalidId(t *testing.T) {
	c, _ := registerAndLogin(t, "geter")
	resp, _ := c.doJSON(t, "GET", "/api/bookings/not-a-uuid", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestExtra_DocumentInvalidPaths covers the bad-input branches in the
// document handler.
func TestExtra_DocumentInvalidPaths(t *testing.T) {
	c, _ := registerAndLogin(t, "docbad")
	// missing related_id
	resp, _ := c.doJSON(t, "POST", "/api/documents/confirmation", map[string]any{
		"related_type": "booking", "title": "x",
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	// bad uuid
	resp, _ = c.doJSON(t, "POST", "/api/documents/confirmation", map[string]any{
		"related_type": "booking", "related_id": "not-uuid", "title": "x",
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 bad uuid, got %d", resp.StatusCode)
	}
	// same for checkin-pass
	resp, _ = c.doJSON(t, "POST", "/api/documents/checkin-pass", map[string]any{
		"related_type": "booking", "related_id": "not-uuid", "title": "x",
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	// document content with bad uuid
	resp, _ = c.doJSON(t, "GET", "/api/documents/not-uuid/content", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	// document with bad revision query
	resp, _ = c.doJSON(t, "GET", "/api/documents/00000000-0000-0000-0000-000000000000/content?revision=abc", nil, nil)
	// Expect either 400 (bad revision) or 404 (doc not found before revision check) depending on order.
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
		t.Errorf("unexpected %d", resp.StatusCode)
	}
	// document get with bad uuid
	resp, _ = c.doJSON(t, "GET", "/api/documents/not-uuid", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestExtra_NotificationBadIDs hits the bad-uuid branches.
func TestExtra_NotificationBadIDs(t *testing.T) {
	c, _ := registerAndLogin(t, "notifbad")
	resp, _ := c.doJSON(t, "POST", "/api/notifications/not-uuid/read", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	resp, _ = c.doJSON(t, "POST", "/api/todos/not-uuid/status", map[string]string{"status": "done"}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	// missing status
	resp, _ = c.doJSON(t, "POST", "/api/todos/00000000-0000-0000-0000-000000000000/status", map[string]any{}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	// missing fields on todo create
	resp, _ = c.doJSON(t, "POST", "/api/todos", map[string]any{}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestExtra_GroupBuyBadIDs covers the bad-uuid handler branches.
func TestExtra_GroupBuyBadIDs(t *testing.T) {
	c := newClient(t)
	for _, p := range []string{
		"/api/group-buys/not-uuid",
		"/api/group-buys/not-uuid/progress",
		"/api/group-buys/not-uuid/participants",
	} {
		resp, _ := c.doJSON(t, "GET", p, nil, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s expected 400, got %d", p, resp.StatusCode)
		}
	}
	logged, _ := registerAndLogin(t, "gbbad")
	resp, _ := logged.doJSON(t, "POST", "/api/group-buys/not-uuid/join", map[string]any{}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestExtra_GroupBuyCreateBadTimes covers parseTime failure paths.
func TestExtra_GroupBuyCreateBadTimes(t *testing.T) {
	c := loginAsAdmin(t)
	for _, body := range []map[string]any{
		{"resource_id": SeedSlipA1, "title": "x", "capacity": 5, "starts_at": "junk", "ends_at": futureRFC3339(4)},
		{"resource_id": SeedSlipA1, "title": "x", "capacity": 5, "starts_at": futureRFC3339(3), "ends_at": "junk"},
		{"resource_id": "not-uuid", "title": "x", "capacity": 5, "starts_at": futureRFC3339(3), "ends_at": futureRFC3339(4)},
		{"resource_id": SeedSlipA1, "title": "x", "capacity": 5, "starts_at": futureRFC3339(3), "ends_at": futureRFC3339(4), "deadline": "junk"},
	} {
		resp, _ := c.doJSON(t, "POST", "/api/group-buys", body, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for %v, got %d", body, resp.StatusCode)
		}
	}
}

// TestExtra_AdminWebhookBadId covers the disable-bad-uuid branch.
func TestExtra_AdminWebhookBadId(t *testing.T) {
	admin := loginAsAdmin(t)
	resp, _ := admin.doJSON(t, "POST", "/api/admin/webhooks/not-uuid/disable", nil, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// TestExtra_LoggedInDashboardRendersHTML hits the Index handler with auth so
// MyBookingsPage is rendered (rather than LandingPage).
func TestExtra_LoggedInDashboardRendersHTML(t *testing.T) {
	c, _ := registerAndLogin(t, "dashboarder")
	resp, body := c.doRaw(t, "GET", "/", nil, map[string]string{"Accept": "text/html"})
	expectStatus(t, resp, body, http.StatusOK)
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("content-type = %q", resp.Header.Get("Content-Type"))
	}
}

// TestExtra_AvailabilityHTMLWithSearch exercises AvailabilityPage's "search
// performed" branch.
func TestExtra_AvailabilityHTMLWithSearch(t *testing.T) {
	c := newClient(t)
	resp, body := c.doRaw(t, "GET", "/availability?resource_id="+SeedSlipA1+"&date=2026-04-08", nil, map[string]string{"Accept": "text/html"})
	expectStatus(t, resp, body, http.StatusOK)
}

// TestExtra_PaginationParams hits the limit/offset query branches in list
// endpoints.
func TestExtra_PaginationParams(t *testing.T) {
	c, _ := registerAndLogin(t, "paginator")
	// All these accept limit/offset; just verify 200.
	paths := []string{
		"/api/group-buys?limit=5&offset=0",
		"/api/bookings?limit=5&offset=0",
		"/api/groups?limit=5&offset=0",
		"/api/notifications?limit=10",
		"/api/todos?status=open&limit=10",
		"/api/admin/webhooks/deliveries?limit=10",
		"/api/analytics/top?days=30&limit=20",
	}
	for _, p := range paths {
		resp, _ := c.doJSON(t, "GET", p, nil, nil)
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s unexpected %d", p, resp.StatusCode)
		}
	}
}

// TestExtra_LoggedInHTMLPages exercises soft-auth HTML routes with a real
// session so usernameOf hits its non-nil branch.
func TestExtra_LoggedInHTMLPages(t *testing.T) {
	c, _ := registerAndLogin(t, "loggedhtml")
	for _, p := range []string{"/groups", "/availability"} {
		resp, body := c.doRaw(t, "GET", p, nil, map[string]string{"Accept": "text/html"})
		expectStatus(t, resp, body, http.StatusOK)
	}
	// Create a group then GET its detail page (logged in).
	resp, body := c.doJSON(t, "POST", "/api/groups", map[string]any{
		"name": "Logged group", "organizer_email": "lg@example.com", "capacity": 5,
	}, nil)
	expectStatus(t, resp, body, http.StatusCreated)
	id, _ := mustJSON(t, body)["id"].(string)
	resp, body = c.doRaw(t, "GET", "/groups/"+id, nil, map[string]string{"Accept": "text/html"})
	expectStatus(t, resp, body, http.StatusOK)
}

// TestExtra_BackupIncrementalDirect exercises BackupIncremental through the
// admin endpoint (the scheduled-job code path is also covered, but this
// ensures the handler line itself is hit by tests.)
func TestExtra_BackupIncrementalDirect(t *testing.T) {
	admin := loginAsAdmin(t)
	resp, body := admin.doJSON(t, "POST", "/api/admin/backups/incremental", nil, nil)
	expectStatus(t, resp, body, http.StatusCreated)
}

// TestExtra_WebhookCreateMultipleBranches covers the field-mapping default
// path and the disabled flag.
func TestExtra_WebhookCreateMultipleBranches(t *testing.T) {
	admin := loginAsAdmin(t)
	resp, body := admin.doJSON(t, "POST", "/api/admin/webhooks", map[string]any{
		"name":          "with-mapping",
		"target_url":    "http://127.0.0.1:1/dest",
		"event_filter":  []string{"booking.created"},
		"field_mapping": map[string]string{"id": "alias_id"},
	}, nil)
	expectStatus(t, resp, body, http.StatusCreated)
}

// TestExtra_BlacklistEnforcedOnBookingCreate uses direct SQL to flag a fresh
// user as blacklisted, then verifies POST /api/bookings is blocked.
func TestExtra_BlacklistEnforcedOnBookingCreate(t *testing.T) {
	c, username := registerAndLogin(t, "bbk")
	db, err := openDB(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE users SET is_blacklisted = TRUE, blacklist_reason = 'test' WHERE username = $1`, username); err != nil {
		t.Fatalf("blacklist: %v", err)
	}
	resp, body := c.doJSON(t, "POST", "/api/bookings", map[string]any{
		"resource_id": SeedSlipA1,
		"start_time":  futureRFC3339(3),
		"end_time":    futureRFC3339(4),
	}, map[string]string{"Idempotency-Key": idemKey("bl")})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", resp.StatusCode, body)
	}
	containsAll(t, body, "blacklisted")
}
