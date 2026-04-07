package api_tests

import (
	"database/sql"
	"net/http"
	"testing"
)

// withBrokenTable temporarily renames the given table away so any handler
// reading from it returns an error. The original name is restored when the
// test finishes (success or failure).
func withBrokenTable(t *testing.T, db *sql.DB, table string, fn func()) {
	t.Helper()
	if _, err := db.Exec(`ALTER TABLE ` + table + ` RENAME TO ` + table + `_brk`); err != nil {
		t.Fatalf("rename %s: %v", table, err)
	}
	defer func() {
		if _, err := db.Exec(`ALTER TABLE ` + table + `_brk RENAME TO ` + table); err != nil {
			t.Errorf("restore %s: %v", table, err)
		}
	}()
	fn()
}

// purgeCache wipes the in-memory cache so the next request actually hits
// the (now-broken) database.
func purgeCache(t *testing.T, admin *Client) {
	t.Helper()
	admin.doJSON(t, "POST", "/api/admin/cache/purge", nil, nil)
}

// TestDBFailure_ReadHandlersReturnInternalError walks the read endpoints
// whose dead `if err != nil { writeServiceError; return }` branches need to
// fire to push API coverage past 90%. Each sub-test breaks one table,
// purges cache so the request reaches the broken DB, asserts the 500
// response, then restores the table.
func TestDBFailure_ReadHandlersReturnInternalError(t *testing.T) {
	admin := loginAsAdmin(t)
	db, err := openDB(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	cases := []struct {
		name  string
		table string
		path  string
	}{
		{"dictionary", "data_dictionary", "/api/governance/dictionary"},
		{"tags", "tags", "/api/governance/tags"},
		{"resources_list", "resources", "/api/resources"},
		{"groups_list", "group_reservations", "/api/groups"},
		{"top_sessions", "analytics_events", "/api/analytics/top?days=7"},
		{"trends", "analytics_events", "/api/analytics/trends?event_type=view"},
		{"backups_list", "backups", "/api/admin/backups"},
		{"webhooks_list", "webhooks", "/api/admin/webhooks"},
		{"webhook_deliveries", "webhook_deliveries", "/api/admin/webhooks/deliveries"},
		{"notification_deliveries", "notification_deliveries", "/api/admin/notification-deliveries"},
		{"anomalies", "anomaly_alerts", "/api/admin/anomalies"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			purgeCache(t, admin)
			withBrokenTable(t, db, c.table, func() {
				resp, body := admin.doJSON(t, "GET", c.path, nil, nil)
				if resp.StatusCode != http.StatusInternalServerError && resp.StatusCode != http.StatusServiceUnavailable {
					t.Errorf("%s expected 5xx, got %d body=%s", c.path, resp.StatusCode, body)
				}
			})
			purgeCache(t, admin)
		})
	}
}

// TestDBFailure_LoggedInHandlersAndConsent covers the same dead-branch
// pattern on user-scoped handlers (notifications, todos, consent, my
// bookings).
func TestDBFailure_LoggedInHandlersAndConsent(t *testing.T) {
	admin := loginAsAdmin(t)
	user, _ := registerAndLogin(t, "dbfail")
	db, err := openDB(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	cases := []struct {
		name  string
		table string
		path  string
	}{
		{"notifications", "notifications", "/api/notifications"},
		{"unread_count", "notifications", "/api/notifications/unread-count"},
		{"todos_list", "todos", "/api/todos"},
		{"consent_list", "consent_records", "/api/consent"},
		{"my_bookings", "bookings", "/api/bookings"},
		{"my_documents", "documents", "/api/documents"},
		{"my_groupbuys", "group_buys", "/api/group-buys"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			purgeCache(t, admin)
			withBrokenTable(t, db, c.table, func() {
				resp, body := user.doJSON(t, "GET", c.path, nil, nil)
				if resp.StatusCode < 500 {
					t.Errorf("%s expected 5xx, got %d body=%s", c.path, resp.StatusCode, body)
				}
			})
			purgeCache(t, admin)
		})
	}
}

// TestDBFailure_HTMLPagesShowErrors covers the err branches in HTML pages
// (NewPage, AvailabilityPage, IndexHTML, DetailHTML, MyBookingsPage).
func TestDBFailure_HTMLPagesShowErrors(t *testing.T) {
	admin := loginAsAdmin(t)
	user, _ := registerAndLogin(t, "htmlfail")
	db, err := openDB(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	cases := []struct {
		name   string
		client *Client
		table  string
		path   string
	}{
		{"new_booking_page", user, "resources", "/bookings/new"},
		{"availability_html", user, "resources", "/availability?resource_id=" + SeedSlipA1 + "&date=2026-04-08"},
		{"groups_html", user, "group_reservations", "/groups"},
		{"my_bookings_dash", user, "bookings", "/"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			purgeCache(t, admin)
			withBrokenTable(t, db, c.table, func() {
				resp, _ := c.client.doRaw(t, "GET", c.path, nil, map[string]string{"Accept": "text/html"})
				if resp.StatusCode < 500 {
					t.Errorf("%s expected 5xx, got %d", c.path, resp.StatusCode)
				}
			})
			purgeCache(t, admin)
		})
	}
}

// TestDBFailure_GroupBuyAlreadyJoinedAndDeadline covers the error branches
// in writeGroupBuyError that need a specific server state to fire.
func TestDBFailure_GroupBuyAlreadyJoinedAndDeadline(t *testing.T) {
	admin := loginAsAdmin(t)
	user, _ := registerAndLogin(t, "joinerr")
	db, err := openDB(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// 1) ErrAlreadyJoined: same user, same group, two distinct idempotency keys.
	id := createGroupBuy(t, admin, 5, 1)
	resp, body := user.doJSON(t, "POST", "/api/group-buys/"+id+"/join", map[string]any{"quantity": 1}, map[string]string{"Idempotency-Key": idemKey("aj1")})
	expectStatus(t, resp, body, http.StatusOK)
	resp, body = user.doJSON(t, "POST", "/api/group-buys/"+id+"/join", map[string]any{"quantity": 1}, map[string]string{"Idempotency-Key": idemKey("aj2")})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("ErrAlreadyJoined: expected 409, got %d body=%s", resp.StatusCode, body)
	}
	containsAll(t, body, "already joined")

	// 2) ErrDeadlinePassed: backdate a group buy's deadline via SQL, then
	// have a different user try to join.
	id2 := createGroupBuy(t, admin, 5, 1)
	if _, err := db.Exec(`UPDATE group_buys SET deadline = NOW() - INTERVAL '1 minute' WHERE id = $1`, id2); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	user2, _ := registerAndLogin(t, "deadlinejoin")
	resp, body = user2.doJSON(t, "POST", "/api/group-buys/"+id2+"/join", map[string]any{"quantity": 1}, map[string]string{"Idempotency-Key": idemKey("dl")})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("ErrDeadlinePassed: expected 409, got %d body=%s", resp.StatusCode, body)
	}
	containsAll(t, body, "deadline")

	// 3) ErrConflict (group buy in canceled status): mark canceled, try to join.
	id3 := createGroupBuy(t, admin, 5, 1)
	if _, err := db.Exec(`UPDATE group_buys SET status = 'canceled' WHERE id = $1`, id3); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	user3, _ := registerAndLogin(t, "canceljoin")
	resp, body = user3.doJSON(t, "POST", "/api/group-buys/"+id3+"/join", map[string]any{"quantity": 1}, map[string]string{"Idempotency-Key": idemKey("cl")})
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("canceled status: expected 409, got %d body=%s", resp.StatusCode, body)
	}
}

// TestDBFailure_CaptchaAndConsentEndpoints covers the err branches in
// auth Captcha and governance Grant/Withdraw/Cancel handlers.
func TestDBFailure_CaptchaAndConsentEndpoints(t *testing.T) {
	admin := loginAsAdmin(t)
	user, _ := registerAndLogin(t, "captchafail")
	db, err := openDB(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Captcha endpoint
	purgeCache(t, admin)
	withBrokenTable(t, db, "captcha_challenges", func() {
		resp, _ := newClient(t).doJSON(t, "GET", "/api/auth/captcha", nil, nil)
		if resp.StatusCode < 500 {
			t.Errorf("captcha expected 5xx, got %d", resp.StatusCode)
		}
	})
	purgeCache(t, admin)

	// Consent grant + withdraw + cancel deletion fail
	purgeCache(t, admin)
	withBrokenTable(t, db, "consent_records", func() {
		resp, _ := user.doJSON(t, "POST", "/api/consent/grant", map[string]string{"scope": "x"}, nil)
		if resp.StatusCode < 500 {
			t.Errorf("grant expected 5xx, got %d", resp.StatusCode)
		}
		resp, _ = user.doJSON(t, "POST", "/api/consent/withdraw", map[string]string{"scope": "x"}, nil)
		if resp.StatusCode < 500 {
			t.Errorf("withdraw expected 5xx, got %d", resp.StatusCode)
		}
	})
	purgeCache(t, admin)

	purgeCache(t, admin)
	withBrokenTable(t, db, "deletion_requests", func() {
		resp, _ := user.doJSON(t, "POST", "/api/account/delete", nil, nil)
		if resp.StatusCode < 500 {
			t.Errorf("delete expected 5xx, got %d", resp.StatusCode)
		}
		resp, _ = user.doJSON(t, "POST", "/api/account/delete/cancel", nil, nil)
		if resp.StatusCode < 500 {
			t.Errorf("cancel expected 5xx, got %d", resp.StatusCode)
		}
	})
	purgeCache(t, admin)
}

// TestDBFailure_BackupAndCSVExport covers the error branches in backup +
// CSV export handlers.
func TestDBFailure_BackupAndCSVExport(t *testing.T) {
	admin := loginAsAdmin(t)
	db, err := openDB(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	purgeCache(t, admin)
	withBrokenTable(t, db, "resources", func() {
		resp, _ := admin.doRaw(t, "GET", "/api/admin/export/resources.csv", nil, nil)
		if resp.StatusCode < 500 {
			t.Errorf("export csv expected 5xx, got %d", resp.StatusCode)
		}
	})
	purgeCache(t, admin)
}
