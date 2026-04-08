package api_tests

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

// TestIdempotency_ConcurrentSameKey verifies that when many goroutines
// fire the same idempotency key at the same /join endpoint, the at-most-once
// reservation pattern guarantees:
//
//   - exactly ONE underlying state change happens (one slot decremented),
//   - one request is the canonical "first mover" that returns 200,
//   - every other concurrent caller either replays that 200 (Idempotent-Replay
//     header) or is rejected with 409 because a sibling holds the pending
//     reservation (clients are expected to retry on the 409 — those retries
//     would then see the completed row and replay).
//
// The critical invariant under test is that the *underlying* counter only
// moves once even though many requests collide.
func TestIdempotency_ConcurrentSameKey(t *testing.T) {
	const racers = 16

	admin := loginAsAdmin(t)
	id := createGroupBuy(t, admin, 10, 0)

	user, _ := registerAndLogin(t, "concurrent-idem")
	key := idemKey("concurrent-same-key")

	var (
		wg          sync.WaitGroup
		successes   int32
		replayed    int32
		pendingHits int32
		other       int32
	)
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func() {
			defer wg.Done()
			resp, _ := user.doJSON(t, "POST", "/api/group-buys/"+id+"/join", map[string]any{
				"quantity": 1,
			}, map[string]string{"Idempotency-Key": key})
			switch {
			case resp.StatusCode == http.StatusOK && resp.Header.Get("Idempotent-Replay") == "true":
				atomic.AddInt32(&replayed, 1)
			case resp.StatusCode == http.StatusOK:
				atomic.AddInt32(&successes, 1)
			case resp.StatusCode == http.StatusConflict:
				atomic.AddInt32(&pendingHits, 1)
			default:
				atomic.AddInt32(&other, 1)
			}
		}()
	}
	wg.Wait()

	if other != 0 {
		t.Fatalf("unexpected non-200/409 responses: %d", other)
	}
	if successes != 1 {
		t.Fatalf("expected exactly 1 first-mover success, got %d (replayed=%d pending=%d)",
			successes, replayed, pendingHits)
	}
	if replayed+pendingHits != int32(racers-1) {
		t.Fatalf("expected %d replays+pending-rejections, got replayed=%d pending=%d",
			racers-1, replayed, pendingHits)
	}

	// Crucially: confirm only one slot was actually consumed.
	resp, body := user.doJSON(t, "GET", "/api/group-buys/"+id+"/progress", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	prog := mustJSON(t, body)
	if got, _ := prog["remaining_slots"].(float64); got != 9 {
		t.Fatalf("expected remaining_slots=9 (one decrement), got %v", got)
	}

	// Now any retry of the 409'd callers should see the completed row and
	// replay the response.
	resp, body = user.doJSON(t, "POST", "/api/group-buys/"+id+"/join", map[string]any{
		"quantity": 1,
	}, map[string]string{"Idempotency-Key": key})
	expectStatus(t, resp, body, http.StatusOK)
	if resp.Header.Get("Idempotent-Replay") != "true" {
		t.Fatalf("post-settle retry should be a replay, header missing")
	}
}

// TestIdempotency_CrossUserKeyIsolation verifies that two different users
// submitting the SAME client-generated Idempotency-Key value never see each
// other's response. Before the (user_id, key) scoping fix, the second user
// would replay the first user's POST body — leaking data and silently
// dropping their own request.
//
// The test sequence:
//
//  1. Alice POSTs /join on group buy A with key K → succeeds, decrement A.
//  2. Bob   POSTs /join on group buy B with key K → MUST succeed independently
//     and decrement B (not get a stale replay of Alice's response).
//  3. Bob's response body must reference group_buy_id == B, not A.
//  4. Bob's second call with key K must replay Bob's own response, not Alice's.
func TestIdempotency_CrossUserKeyIsolation(t *testing.T) {
	admin := loginAsAdmin(t)
	gbA := createGroupBuy(t, admin, 5, 0)
	gbB := createGroupBuy(t, admin, 5, 0)

	alice, _ := registerAndLogin(t, "alice-collide")
	bob, _ := registerAndLogin(t, "bob-collide")

	const sharedKey = "user-supplied-shared-key-DO-NOT-COLLIDE"

	// 1. Alice joins gbA with the shared key.
	resp, body := alice.doJSON(t, "POST", "/api/group-buys/"+gbA+"/join", map[string]any{
		"quantity": 1,
	}, map[string]string{"Idempotency-Key": sharedKey})
	expectStatus(t, resp, body, http.StatusOK)
	aliceResp := mustJSON(t, body)
	gbARet, _ := aliceResp["group_buy"].(map[string]any)
	if gotID, _ := gbARet["id"].(string); gotID != gbA {
		t.Fatalf("alice expected gb id %s, got %s", gbA, gotID)
	}

	// 2. Bob joins gbB with the same client-supplied key. This must NOT
	//    replay Alice's response (different scope), and must actually
	//    decrement gbB.
	resp, body = bob.doJSON(t, "POST", "/api/group-buys/"+gbB+"/join", map[string]any{
		"quantity": 1,
	}, map[string]string{"Idempotency-Key": sharedKey})
	expectStatus(t, resp, body, http.StatusOK)
	if resp.Header.Get("Idempotent-Replay") == "true" {
		t.Fatalf("bob should NOT have seen alice's replay; cross-user collision")
	}
	bobResp := mustJSON(t, body)
	gbBRet, _ := bobResp["group_buy"].(map[string]any)
	if gotID, _ := gbBRet["id"].(string); gotID != gbB {
		t.Fatalf("bob expected gb id %s, got %s — cross-user data leak", gbB, gotID)
	}

	// 3. Confirm both group buys decremented exactly once.
	for _, gb := range []string{gbA, gbB} {
		resp, body := admin.doJSON(t, "GET", "/api/group-buys/"+gb+"/progress", nil, nil)
		expectStatus(t, resp, body, http.StatusOK)
		prog := mustJSON(t, body)
		if got, _ := prog["remaining_slots"].(float64); got != 4 {
			t.Fatalf("group buy %s: expected remaining_slots=4, got %v", gb, got)
		}
	}

	// 4. Bob's retry of the same key replays HIS response (not Alice's).
	resp, body = bob.doJSON(t, "POST", "/api/group-buys/"+gbB+"/join", map[string]any{
		"quantity": 1,
	}, map[string]string{"Idempotency-Key": sharedKey})
	expectStatus(t, resp, body, http.StatusOK)
	if resp.Header.Get("Idempotent-Replay") != "true" {
		t.Fatalf("bob's retry should be a replay")
	}
	replay := mustJSON(t, body)
	gbReplay, _ := replay["group_buy"].(map[string]any)
	if gotID, _ := gbReplay["id"].(string); gotID != gbB {
		t.Fatalf("bob's replay returned wrong gb id %s (want %s)", gotID, gbB)
	}
}
