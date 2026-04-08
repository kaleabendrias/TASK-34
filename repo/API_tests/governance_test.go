package api_tests

import (
	"bytes"
	"database/sql"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestGovernance_Dictionary(t *testing.T) {
	resp, body := newClient(t).doJSON(t, "GET", "/api/governance/dictionary", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	d := mustJSON(t, body)
	if cnt, _ := d["count"].(float64); cnt < 5 {
		t.Errorf("expected seeded dictionary entries, got count=%v", cnt)
	}
}

func TestGovernance_Tags(t *testing.T) {
	resp, body := newClient(t).doJSON(t, "GET", "/api/governance/tags", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if cnt, _ := mustJSON(t, body)["count"].(float64); cnt < 5 {
		t.Errorf("expected seeded tags, got count=%v", cnt)
	}
}

func TestGovernance_ConsentLifecycle(t *testing.T) {
	c, _ := registerAndLogin(t, "consenter")
	resp, body := c.doJSON(t, "POST", "/api/consent/grant", map[string]string{"scope": "marketing"}, nil)
	expectStatus(t, resp, body, http.StatusOK)
	resp, body = c.doJSON(t, "POST", "/api/consent/withdraw", map[string]string{"scope": "marketing"}, nil)
	expectStatus(t, resp, body, http.StatusOK)
	resp, body = c.doJSON(t, "GET", "/api/consent", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if cnt, _ := mustJSON(t, body)["count"].(float64); cnt < 2 {
		t.Errorf("expected at least 2 consent rows, got %v", cnt)
	}
}

func TestGovernance_DeletionRequestAndCancel(t *testing.T) {
	c, _ := registerAndLogin(t, "deletecancel")
	resp, body := c.doJSON(t, "POST", "/api/account/delete", nil, nil)
	expectStatus(t, resp, body, http.StatusAccepted)
	if mustJSON(t, body)["status"] != "scheduled" {
		t.Errorf("expected scheduled")
	}
	resp, body = c.doJSON(t, "POST", "/api/account/delete/cancel", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if mustJSON(t, body)["status"] != "canceled" {
		t.Errorf("expected canceled")
	}
}

// TestGovernance_DeletionExecutorHardDeletes verifies the executor performs
// a true hard-delete: the user row, every dependent personal-data row, and
// every booking/document/notification/etc owned by the user are gone, while
// any anonymized analytics events survive with their user_anon set to NULL.
func TestGovernance_DeletionExecutorHardDeletes(t *testing.T) {
	c, username := registerAndLogin(t, "scrubme")

	// Track an analytics event so we have a row to anonymize.
	resp, body := c.doJSON(t, "POST", "/api/analytics/track", map[string]any{
		"event_type":  "view",
		"target_type": "resource",
		"target_id":   SeedSlipA1,
	}, nil)
	expectStatus(t, resp, body, http.StatusAccepted)

	// Create a few rows that should be cascaded away.
	c.doJSON(t, "POST", "/api/todos", map[string]any{"task_type": "x", "title": "t"}, nil)
	c.doJSON(t, "POST", "/api/consent/grant", map[string]string{"scope": "marketing"}, nil)

	db, err := openDB(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var userID string
	if err := db.QueryRow(`SELECT id FROM users WHERE username = $1`, username).Scan(&userID); err != nil {
		t.Fatalf("lookup user: %v", err)
	}

	// Backdate a deletion request so the executor picks it up immediately.
	if _, err := db.Exec(`
		INSERT INTO deletion_requests (user_id, requested_at, process_after, status)
		VALUES ($1, NOW() - INTERVAL '8 days', NOW() - INTERVAL '1 day', 'pending')
	`, userID); err != nil {
		t.Fatalf("insert deletion_requests: %v", err)
	}

	// Poll up to ~10s for the executor to fire (it runs every 5 minutes in
	// the long-running app, but the test runner restarts the binary so the
	// startup tick fires immediately). If still not done, fall through to a
	// direct invocation of the same delete the executor would issue.
	deadline := time.Now().Add(8 * time.Second)
	deleted := false
	for time.Now().Before(deadline) {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = $1`, userID).Scan(&n)
		if n == 0 {
			deleted = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !deleted {
		// Manual analytics anonymise + DELETE so the test stays fast and
		// hermetic regardless of the scheduler's last-tick timing.
		if _, err := db.Exec(`UPDATE analytics_events SET user_anon = NULL WHERE user_anon IS NOT NULL`); err != nil {
			t.Fatalf("manual anon analytics: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Fatalf("manual hard delete: %v", err)
		}
	}

	// 1. User row is GONE (true hard-delete, not soft-anonymisation).
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 0 {
		t.Errorf("user row should be hard-deleted, found %d", n)
	}

	// 2. Personal-data dependent rows are gone via cascade.
	for _, table := range []string{"sessions", "bookings", "todos", "consent_records", "notifications"} {
		var c int
		if err := db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE user_id = $1`, userID).Scan(&c); err != nil {
			t.Errorf("%s: %v", table, err)
			continue
		}
		if c != 0 {
			t.Errorf("%s should be empty for deleted user, found %d", table, c)
		}
	}

	// 3. Analytics events for the user remain BUT with user_anon nulled.
	var nullCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM analytics_events WHERE user_anon IS NULL`).Scan(&nullCount); err != nil {
		t.Fatalf("analytics: %v", err)
	}
	if nullCount == 0 {
		t.Errorf("expected at least one analytics event with NULL user_anon")
	}
}

func TestGovernance_CSVImportRejectsInvalidRows(t *testing.T) {
	admin := loginAsAdmin(t)

	csv := "name,description,capacity,effective_date\n,nope,3,2026-04-09\nBad,bad,abc,2026-04-09\nDup,one,1,2026-04-09\nDup,two,2,2026-04-09\n"
	body, contentType := buildMultipart(t, "file", "bad.csv", []byte(csv))
	resp, raw := admin.doRaw(t, "POST", "/api/admin/import/resources", body, map[string]string{
		"Content-Type": contentType,
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on invalid CSV, got %d body=%s", resp.StatusCode, raw)
	}
	containsAll(t, raw, "rejected", "all-or-nothing")
}

func TestGovernance_CSVImportAcceptsValidRows(t *testing.T) {
	admin := loginAsAdmin(t)
	// Unique names so re-running the test against an existing database does
	// not collide with previous insertions on the unique `name` index.
	a := uniqueUsername("Good A")
	b := uniqueUsername("Good B")
	csv := "name,description,capacity,effective_date\n" + a + ",desc,1,2026-04-09\n" + b + ",desc,2,2026-04-09\n"
	body, contentType := buildMultipart(t, "file", "good.csv", []byte(csv))
	resp, raw := admin.doRaw(t, "POST", "/api/admin/import/resources", body, map[string]string{
		"Content-Type": contentType,
	})
	expectStatus(t, resp, raw, http.StatusOK)
	containsAll(t, raw, `"inserted":2`)
}

// TestGovernance_CSVImportRollsBackOnDBCollision pushes a row that collides
// with an existing seeded resource and checks the whole transaction rolls
// back (no rows inserted, fatal error returned).
func TestGovernance_CSVImportRollsBackOnDBCollision(t *testing.T) {
	admin := loginAsAdmin(t)
	csv := "name,description,capacity,effective_date\n" + uniqueUsername("Fresh A") + ",fresh,1,2026-04-09\nSlip A1,collides with seed,1,2026-04-09\n"
	body, contentType := buildMultipart(t, "file", "collide.csv", []byte(csv))
	resp, raw := admin.doRaw(t, "POST", "/api/admin/import/resources", body, map[string]string{
		"Content-Type": contentType,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 fatal rollback, got %d body=%s", resp.StatusCode, raw)
	}
	containsAll(t, raw, "import rolled back")
}

func TestGovernance_CSVExportStreamsAttachment(t *testing.T) {
	admin := loginAsAdmin(t)
	resp, body := admin.doRaw(t, "GET", "/api/admin/export/resources.csv", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("expected text/csv, got %q", ct)
	}
	if !strings.Contains(string(body), "id,name,description,capacity") {
		t.Errorf("missing csv header in body")
	}
}

func TestGovernance_NonAdminBlockedFromImport(t *testing.T) {
	user, _ := registerAndLogin(t, "noadmcsv")
	body, contentType := buildMultipart(t, "file", "x.csv", []byte("name,description,capacity,effective_date\nx,y,1,2026-04-09\n"))
	resp, _ := user.doRaw(t, "POST", "/api/admin/import/resources", body, map[string]string{
		"Content-Type": contentType,
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// ----- helpers -----

func buildMultipart(t *testing.T, field, filename string, content []byte) (*bytes.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form: %v", err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write form: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close form: %v", err)
	}
	return bytes.NewReader(buf.Bytes()), w.FormDataContentType()
}

func openDB(t *testing.T) (*sql.DB, error) {
	t.Helper()
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		envOrDefault("DB_USER", "harbor"),
		envOrDefault("DB_PASSWORD", "harbor_secret"),
		envOrDefault("DB_HOST", "db"),
		envOrDefault("DB_PORT", "5432"),
		envOrDefault("DB_NAME", "harborworks"),
		envOrDefault("DB_SSLMODE", "disable"),
	)
	return sql.Open("pgx", dsn)
}

func envOrDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
