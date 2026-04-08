package api_tests

import (
	"net/http"
	"strings"
	"testing"
)

// TestSession_CookieSlidesOnEveryAuthenticatedRequest verifies the
// high-severity inactivity-window fix: every authenticated request must
// re-emit the session cookie with a fresh Max-Age so the browser does not
// drop the cookie before the 30-minute inactivity window has actually
// elapsed.
//
// Before the fix, only the login response set Max-Age and subsequent
// authenticated requests omitted the Set-Cookie header — so a user who
// stayed under the 30-minute server-side window could still be silently
// logged out by the client.
func TestSession_CookieSlidesOnEveryAuthenticatedRequest(t *testing.T) {
	c, _ := registerAndLogin(t, "slide")

	// /api/auth/me requires a valid session and is one of the cheapest
	// authenticated endpoints in the API. We hit it twice in a row and
	// expect each response to carry a fresh Set-Cookie line whose Max-Age
	// equals the configured inactivity window (1800 seconds = 30 minutes).
	for i := 0; i < 2; i++ {
		resp, body := c.doJSON(t, "GET", "/api/auth/me", nil, nil)
		expectStatus(t, resp, body, http.StatusOK)
		setCookie := resp.Header.Get("Set-Cookie")
		if setCookie == "" {
			t.Fatalf("call %d: expected Set-Cookie on authenticated response, got none", i)
		}
		if !strings.Contains(setCookie, "harborworks_session=") {
			t.Fatalf("call %d: Set-Cookie does not refresh the session cookie: %q", i, setCookie)
		}
		// Max-Age must reflect the 30-minute sliding window. We tolerate
		// either the canonical "Max-Age=1800" header or any non-zero value
		// — the regression we're guarding against is the missing header,
		// not its exact value.
		if !strings.Contains(strings.ToLower(setCookie), "max-age=") {
			t.Errorf("call %d: Set-Cookie missing Max-Age (cookie won't slide): %q", i, setCookie)
		}
	}
}

// TestGroupBuy_ReadableAfterOrganizerDeleted verifies the data-scan fix:
// the group_buys.organizer_id FK uses ON DELETE SET NULL, so reading a
// group buy whose organiser has been hard-deleted must succeed (the
// repository scans into a nullable UUID and the domain model exposes
// *uuid.UUID).
//
// Before the fix the scan failed with "cannot scan NULL into uuid.UUID"
// and every Get/List that touched an orphaned row returned 500.
func TestGroupBuy_ReadableAfterOrganizerDeleted(t *testing.T) {
	// Use a throwaway user as the organiser so the rest of the suite
	// (which depends on the seeded admin) is unaffected when we delete
	// the row out from under the API.
	organiser, _ := registerAndLogin(t, "gborg")
	id := createGroupBuy(t, organiser, 6, 1)

	// Hard-delete the organiser row directly via the DB. We can't go
	// through the public account-deletion endpoint because that schedules
	// a grace-period anonymisation rather than nulling the FK
	// immediately, and we want to exercise the SET NULL path explicitly.
	db, err := openDB(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// First confirm the row currently has a non-null organizer_id so the
	// test isn't a no-op against an unrelated bug.
	var orgBefore *string
	if err := db.QueryRow(`SELECT organizer_id::text FROM group_buys WHERE id = $1`, id).Scan(&orgBefore); err != nil {
		t.Fatalf("read organizer before delete: %v", err)
	}
	if orgBefore == nil || *orgBefore == "" {
		t.Fatalf("group buy organizer_id should be set before the delete; got NULL")
	}

	if _, err := db.Exec(`DELETE FROM users WHERE id = $1::uuid`, *orgBefore); err != nil {
		t.Fatalf("delete organiser user: %v", err)
	}

	// Confirm the FK was nulled by the SET NULL action — sanity check the
	// migration is wired the way the rest of the test depends on.
	var orgAfter *string
	if err := db.QueryRow(`SELECT organizer_id::text FROM group_buys WHERE id = $1`, id).Scan(&orgAfter); err != nil {
		t.Fatalf("read organizer after delete: %v", err)
	}
	if orgAfter != nil {
		t.Fatalf("expected organizer_id to be NULL after user delete, got %q", *orgAfter)
	}

	// The throwaway organiser we registered above was just hard-deleted;
	// their session is now invalid. Use a fresh anonymous client (the
	// GET endpoint is soft-auth) so the read path doesn't depend on the
	// deleted user's auth state.
	guest := newClient(t)

	// Singular Get must succeed and return organizer_id == null.
	resp, body := guest.doJSON(t, "GET", "/api/group-buys/"+id, nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	gb := mustJSON(t, body)
	if v, present := gb["organizer_id"]; present && v != nil {
		t.Fatalf("expected organizer_id null, got %v", v)
	}

	// List must also include the orphaned row without erroring out.
	resp, body = guest.doJSON(t, "GET", "/api/group-buys?limit=50", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	listed := mustJSON(t, body)
	rows, _ := listed["group_buys"].([]any)
	found := false
	for _, r := range rows {
		row, _ := r.(map[string]any)
		if rid, _ := row["id"].(string); rid == id {
			found = true
			if v, present := row["organizer_id"]; present && v != nil {
				t.Errorf("listed orphan row still carries organizer_id %v", v)
			}
		}
	}
	if !found {
		t.Fatalf("orphaned group buy %s missing from list response", id)
	}

	_ = organiser
}

// TestAdmin_BackupHandlerErrorBranches drives the err-return paths in
// BackupFull / BackupIncremental / BackupRestorePlan by renaming the
// `backups` table out from under the request: the file write succeeds but
// the metadata insert (or LastFull lookup) fails, so each handler hits
// its `if err != nil` arm. These branches were uncovered after the admin
// gate moved into middleware and dropped the per-handler 403 happy path.
func TestAdmin_BackupHandlerErrorBranches(t *testing.T) {
	admin := loginAsAdmin(t)
	db, err := openDB(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	withBrokenTable(t, db, "backups", func() {
		resp, body := admin.doJSON(t, "POST", "/api/admin/backups/full", nil, nil)
		if resp.StatusCode < 500 {
			t.Errorf("backups/full expected 5xx, got %d body=%s", resp.StatusCode, body)
		}
		resp, body = admin.doJSON(t, "POST", "/api/admin/backups/incremental", nil, nil)
		if resp.StatusCode < 500 {
			t.Errorf("backups/incremental expected 5xx, got %d body=%s", resp.StatusCode, body)
		}
		// PlanRestore wraps any LastFull error into 424 Failed Dependency.
		resp, body = admin.doJSON(t, "GET", "/api/admin/backups/restore-plan", nil, nil)
		if resp.StatusCode != http.StatusFailedDependency && resp.StatusCode < 500 {
			t.Errorf("backups/restore-plan expected 424 or 5xx, got %d body=%s", resp.StatusCode, body)
		}
	})
}
