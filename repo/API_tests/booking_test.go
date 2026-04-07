package api_tests

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBooking_CreateAndStateMachine(t *testing.T) {
	c, _ := registerAndLogin(t, "booker")

	// Lead-time violation: < 2h.
	soon := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339)
	soonEnd := time.Now().UTC().Add(90 * time.Minute).Format(time.RFC3339)
	resp, body := c.doJSON(t, "POST", "/api/bookings", map[string]any{
		"resource_id": SeedSlipA1,
		"start_time":  soon,
		"end_time":    soonEnd,
	}, map[string]string{"Idempotency-Key": idemKey("lead")})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 ErrLeadTime, got %d body=%s", resp.StatusCode, body)
	}
	containsAll(t, body, "lead time")

	// Happy path.
	resp, body = c.doJSON(t, "POST", "/api/bookings", map[string]any{
		"resource_id": SeedSlipA1,
		"start_time":  futureRFC3339(3),
		"end_time":    futureRFC3339(4),
		"notes":       "test booking",
	}, map[string]string{"Idempotency-Key": idemKey("create")})
	expectStatus(t, resp, body, http.StatusCreated)
	bk := mustJSON(t, body)
	id, _ := bk["id"].(string)
	if id == "" {
		t.Fatal("missing id")
	}
	if status, _ := bk["status"].(string); status != "pending_confirmation" {
		t.Errorf("status = %v", bk["status"])
	}

	// Same window again → overlap rejection.
	resp, body = c.doJSON(t, "POST", "/api/bookings", map[string]any{
		"resource_id": SeedSlipA1,
		"start_time":  futureRFC3339(3),
		"end_time":    futureRFC3339(4),
	}, map[string]string{"Idempotency-Key": idemKey("overlap")})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("overlap expected 409, got %d body=%s", resp.StatusCode, body)
	}

	// Get + List.
	resp, body = c.doJSON(t, "GET", "/api/bookings/"+id, nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	resp, body = c.doJSON(t, "GET", "/api/bookings", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)

	// Transition pending → checked_in (allowed).
	resp, body = c.doJSON(t, "POST", "/api/bookings/"+id+"/transition", map[string]string{
		"target_state": "checked_in",
	}, map[string]string{"Idempotency-Key": idemKey("ci")})
	expectStatus(t, resp, body, http.StatusOK)

	// Now invalid: checked_in → pending_confirmation.
	resp, body = c.doJSON(t, "POST", "/api/bookings/"+id+"/transition", map[string]string{
		"target_state": "pending_confirmation",
	}, map[string]string{"Idempotency-Key": idemKey("bad")})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("invalid transition expected 409, got %d", resp.StatusCode)
	}

	// Transition to completed (allowed terminal).
	resp, body = c.doJSON(t, "POST", "/api/bookings/"+id+"/transition", map[string]string{
		"target_state": "completed",
	}, map[string]string{"Idempotency-Key": idemKey("done")})
	expectStatus(t, resp, body, http.StatusOK)

	// Other user cannot read it.
	other, _ := registerAndLogin(t, "otheruser")
	resp, _ = other.doJSON(t, "GET", "/api/bookings/"+id, nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestBooking_CreateBadInputs(t *testing.T) {
	c, _ := registerAndLogin(t, "badin")
	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing resource", map[string]any{"start_time": futureRFC3339(3), "end_time": futureRFC3339(4)}},
		{"bad uuid", map[string]any{"resource_id": "not-a-uuid", "start_time": futureRFC3339(3), "end_time": futureRFC3339(4)}},
		{"bad time", map[string]any{"resource_id": SeedSlipA2, "start_time": "yesterday", "end_time": futureRFC3339(4)}},
		{"bad end", map[string]any{"resource_id": SeedSlipA2, "start_time": futureRFC3339(3), "end_time": "tomorrow"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, _ := c.doJSON(t, "POST", "/api/bookings", tc.body, nil)
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

func TestBooking_HTMLNewPageRequiresAuthAndRenders(t *testing.T) {
	// Anonymous → 302/401-style redirect or 401 JSON. We accept either.
	c := newClient(t)
	c.HTTP.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, _ := c.doRaw(t, "GET", "/bookings/new", nil, map[string]string{"Accept": "text/html"})
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous /bookings/new expected redirect or 401, got %d", resp.StatusCode)
	}

	// Logged in → 200 + html
	logged, _ := registerAndLogin(t, "htmlnew")
	resp, body := logged.doRaw(t, "GET", "/bookings/new", nil, map[string]string{"Accept": "text/html"})
	expectStatus(t, resp, body, http.StatusOK)
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Errorf("content-type = %q", resp.Header.Get("Content-Type"))
	}
}
