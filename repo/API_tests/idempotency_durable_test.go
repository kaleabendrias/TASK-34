package api_tests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
)

// TestIdempotency_PendingReservationSurvivesCompleteFailure is the
// "durable failure" audit test. It simulates a database disconnection
// that happens *after* Reserve has inserted a pending row but *before*
// Complete has persisted the finalized response. In that window the
// side effect may have committed or not — the middleware cannot tell —
// so the contract is strict: the pending reservation stays in place and
// every subsequent retry with the same key sees 409 Retry-After until
// the 24h TTL expires. Side effects must not run twice.
//
// The test synthesises the aftermath of the interrupted Complete by
// inserting a pending idempotency_keys row directly, with the request
// hash the middleware will compute for the matching POST below, and
// then retries the join. It asserts:
//
//  1. The retry is rejected with 409 + Retry-After (no silent replay).
//  2. The underlying group buy's remaining_slots did NOT change
//     — i.e. the side effect was not re-executed.
func TestIdempotency_PendingReservationSurvivesCompleteFailure(t *testing.T) {
	admin := loginAsAdmin(t)
	id := createGroupBuy(t, admin, 10, 0)

	user, username := registerAndLogin(t, "durable")

	db, err := openDB(t)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var userID string
	if err := db.QueryRow(`SELECT id::text FROM users WHERE username = $1`, username).Scan(&userID); err != nil {
		t.Fatalf("lookup user: %v", err)
	}

	// Snapshot the current remaining_slots so we can assert the blocked
	// retry did not decrement them.
	resp, body := user.doJSON(t, "GET", "/api/group-buys/"+id+"/progress", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	before, _ := mustJSON(t, body)["remaining_slots"].(float64)

	// Build the exact request hash the middleware will compute for the
	// POST we are about to issue. The middleware hashes:
	//   METHOD + " " + URL.Path + "?" + <raw body bytes>
	reqBody := map[string]any{"quantity": 2}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := "/api/group-buys/" + id + "/join"
	sum := sha256.Sum256(append([]byte("POST "+path+"?"), raw...))
	hash := hex.EncodeToString(sum[:])

	key := idemKey("durable-pending")

	// Plant the pending row: this is the state we would be left in if
	// the handler's side effect had committed but the Complete() upsert
	// never landed because the connection dropped.
	if _, err := db.Exec(`
		INSERT INTO idempotency_keys
		    (key, user_id, request_hash, status_code, response_body, content_type, status, created_at, expires_at)
		VALUES ($1, $2::uuid, $3, NULL, NULL, '', 'pending', NOW(), NOW() + INTERVAL '1 hour')
	`, key, userID, hash); err != nil {
		t.Fatalf("plant pending reservation: %v", err)
	}

	// The retry MUST be rejected — not silently replayed and not
	// re-executed.
	resp, body = user.doJSON(t, "POST", path, reqBody, map[string]string{"Idempotency-Key": key})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 on stuck-pending retry, got %d body=%s", resp.StatusCode, body)
	}
	if ra := resp.Header.Get("Retry-After"); ra == "" {
		t.Errorf("expected Retry-After header on pending-conflict response")
	}

	// Side-effect check: remaining_slots must be unchanged. If the
	// middleware had released the pending row on a Complete() failure
	// and re-run the handler, this value would have dropped by 2.
	resp, body = user.doJSON(t, "GET", "/api/group-buys/"+id+"/progress", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	after, _ := mustJSON(t, body)["remaining_slots"].(float64)
	if before != after {
		t.Errorf("at-most-once violated: remaining_slots changed from %v to %v", before, after)
	}

	// Clean the planted row so it doesn't linger past the test (the 24h
	// TTL would otherwise keep it around for the rest of the suite).
	if _, err := db.Exec(`DELETE FROM idempotency_keys WHERE key = $1`, key); err != nil {
		t.Errorf("cleanup planted row: %v", err)
	}
}
