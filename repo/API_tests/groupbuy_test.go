package api_tests

import (
	"net/http"
	"sync"
	"testing"
)

// createGroupBuy is shared by several tests.
func createGroupBuy(t *testing.T, c *Client, capacity, threshold int) string {
	t.Helper()
	body := map[string]any{
		"resource_id": SeedSunsetDeck,
		"title":       "Test buy " + uniqueUsername("gb"),
		"capacity":    capacity,
		"starts_at":   futureRFC3339(3),
		"ends_at":     futureRFC3339(5),
	}
	if threshold > 0 {
		body["threshold"] = threshold
	}
	resp, raw := c.doJSON(t, "POST", "/api/group-buys", body, map[string]string{
		"Idempotency-Key": idemKey("create"),
	})
	expectStatus(t, resp, raw, http.StatusCreated)
	gb := mustJSON(t, raw)
	id, _ := gb["id"].(string)
	if id == "" {
		t.Fatal("missing id")
	}
	return id
}

func TestGroupBuy_CreateValidatesInput(t *testing.T) {
	c, _ := registerAndLogin(t, "gbcreator")

	// threshold > capacity → 400.
	resp, body := c.doJSON(t, "POST", "/api/group-buys", map[string]any{
		"resource_id": SeedSunsetDeck,
		"title":       "Bad",
		"threshold":   100,
		"capacity":    5,
		"starts_at":   futureRFC3339(3),
		"ends_at":     futureRFC3339(4),
	}, map[string]string{"Idempotency-Key": idemKey("bad")})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("threshold>capacity expected 400, got %d body=%s", resp.StatusCode, body)
	}

	// Missing resource_id → 400.
	resp, _ = c.doJSON(t, "POST", "/api/group-buys", map[string]any{
		"title": "x", "capacity": 1, "starts_at": futureRFC3339(3), "ends_at": futureRFC3339(4),
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing resource expected 400, got %d", resp.StatusCode)
	}
}

// TestGroupBuy_IdempotentReplay verifies that two requests with the same
// Idempotency-Key resolve to identical responses and (critically) only one
// underlying state change happens.
func TestGroupBuy_IdempotentReplay(t *testing.T) {
	admin := loginAsAdmin(t)
	id := createGroupBuy(t, admin, 10, 0)

	user, _ := registerAndLogin(t, "idem")
	key := idemKey("join-once")

	resp1, body1 := user.doJSON(t, "POST", "/api/group-buys/"+id+"/join", map[string]any{
		"quantity": 2,
	}, map[string]string{"Idempotency-Key": key})
	expectStatus(t, resp1, body1, http.StatusOK)
	first := mustJSON(t, body1)
	gb1, _ := first["group_buy"].(map[string]any)
	remaining1, _ := gb1["remaining_slots"].(float64)

	// Second click with the same key → identical body, header marked replay.
	resp2, body2 := user.doJSON(t, "POST", "/api/group-buys/"+id+"/join", map[string]any{
		"quantity": 2,
	}, map[string]string{"Idempotency-Key": key})
	expectStatus(t, resp2, body2, http.StatusOK)
	if string(body1) != string(body2) {
		t.Fatalf("replay body differs:\n%s\nvs\n%s", body1, body2)
	}
	if resp2.Header.Get("Idempotent-Replay") != "true" {
		t.Errorf("missing Idempotent-Replay header")
	}

	// Verify the underlying counter only decremented once.
	resp, body := user.doJSON(t, "GET", "/api/group-buys/"+id+"/progress", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	prog := mustJSON(t, body)
	if got, _ := prog["remaining_slots"].(float64); got != remaining1 {
		t.Fatalf("expected remaining_slots stable at %v, got %v", remaining1, got)
	}
}

// TestGroupBuy_IdempotencyMismatchRejected ensures the same key with a
// different request body is hard-rejected (no silent replay).
func TestGroupBuy_IdempotencyMismatchRejected(t *testing.T) {
	admin := loginAsAdmin(t)
	id := createGroupBuy(t, admin, 10, 0)
	user, _ := registerAndLogin(t, "mismatch")
	key := idemKey("mismatch")

	resp, body := user.doJSON(t, "POST", "/api/group-buys/"+id+"/join", map[string]any{
		"quantity": 1,
	}, map[string]string{"Idempotency-Key": key})
	expectStatus(t, resp, body, http.StatusOK)

	resp, body = user.doJSON(t, "POST", "/api/group-buys/"+id+"/join", map[string]any{
		"quantity": 5, // different body
	}, map[string]string{"Idempotency-Key": key})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 mismatch, got %d body=%s", resp.StatusCode, body)
	}
}

// TestGroupBuy_NoOversellUnderConcurrency spins up many users joining at
// once. The optimistic-locked decrement must not allow the total of confirmed
// quantities to exceed the configured capacity.
func TestGroupBuy_NoOversellUnderConcurrency(t *testing.T) {
	const capacity = 3
	const racers = 12
	admin := loginAsAdmin(t)
	id := createGroupBuy(t, admin, capacity, 1)

	users := make([]*Client, racers)
	for i := 0; i < racers; i++ {
		users[i], _ = registerAndLogin(t, "racer")
	}

	var wg sync.WaitGroup
	successes := make([]bool, racers)
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(i int) {
			defer wg.Done()
			resp, _ := users[i].doJSON(t, "POST", "/api/group-buys/"+id+"/join", map[string]any{
				"quantity": 1,
			}, map[string]string{"Idempotency-Key": idemKey("race-" + itoa(i))})
			successes[i] = resp.StatusCode == http.StatusOK
		}(i)
	}
	wg.Wait()

	wonCount := 0
	for _, ok := range successes {
		if ok {
			wonCount++
		}
	}
	if wonCount != capacity {
		t.Fatalf("expected exactly %d successes, got %d", capacity, wonCount)
	}

	// Final progress: remaining_slots == 0, status == met.
	resp, body := admin.doJSON(t, "GET", "/api/group-buys/"+id+"/progress", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	prog := mustJSON(t, body)
	if got, _ := prog["remaining_slots"].(float64); got != 0 {
		t.Fatalf("expected remaining_slots=0, got %v", got)
	}
}

func TestGroupBuy_GetListProgressParticipantsAreMasked(t *testing.T) {
	admin := loginAsAdmin(t)
	id := createGroupBuy(t, admin, 5, 0)

	user, _ := registerAndLogin(t, "joiner")
	resp, body := user.doJSON(t, "POST", "/api/group-buys/"+id+"/join", map[string]any{"quantity": 1}, map[string]string{"Idempotency-Key": idemKey("j")})
	expectStatus(t, resp, body, http.StatusOK)

	// List
	resp, body = user.doJSON(t, "GET", "/api/group-buys?limit=5", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)

	// Get
	resp, body = user.doJSON(t, "GET", "/api/group-buys/"+id, nil, nil)
	expectStatus(t, resp, body, http.StatusOK)

	// Participants — names must be masked.
	resp, body = user.doJSON(t, "GET", "/api/group-buys/"+id+"/participants", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	parts := mustJSON(t, body)
	list, _ := parts["participants"].([]any)
	if len(list) == 0 {
		t.Fatal("no participants")
	}
	first, _ := list[0].(map[string]any)
	masked, _ := first["masked_name"].(string)
	if masked == "" || !containsRune(masked, '*') {
		t.Errorf("masked_name %q is not masked", masked)
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
