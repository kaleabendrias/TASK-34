package api_tests

import (
	"net/http"
	"testing"
)

func TestCache_MissThenHit(t *testing.T) {
	admin := loginAsAdmin(t)
	// Purge so other tests' state doesn't interfere.
	admin.doJSON(t, "POST", "/api/admin/cache/purge", nil, nil)

	// First call: MISS.
	resp, body := admin.doJSON(t, "GET", "/api/resources", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if got := resp.Header.Get("X-Cache"); got != "MISS" {
		t.Errorf("first call X-Cache = %q, want MISS", got)
	}

	// Second call: HIT.
	resp, body = admin.doJSON(t, "GET", "/api/resources", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if got := resp.Header.Get("X-Cache"); got != "HIT" {
		t.Errorf("second call X-Cache = %q, want HIT", got)
	}
}

func TestCache_AdminBypassHeader(t *testing.T) {
	admin := loginAsAdmin(t)
	// Warm the cache.
	admin.doJSON(t, "GET", "/api/resources", nil, nil)
	// Bypass with the header → MISS or BYPASS, never HIT.
	resp, body := admin.doJSON(t, "GET", "/api/resources", nil, map[string]string{
		"X-Cache-Bypass": "true",
	})
	expectStatus(t, resp, body, http.StatusOK)
	if got := resp.Header.Get("X-Cache"); got != "BYPASS" {
		t.Errorf("admin bypass: X-Cache = %q, want BYPASS", got)
	}
}

func TestCache_NonAdminCannotBypass(t *testing.T) {
	user, _ := registerAndLogin(t, "nobypass")
	// Warm.
	user.doJSON(t, "GET", "/api/resources", nil, nil)
	// Non-admin sets the header. It must be ignored — i.e. cache returns HIT.
	resp, body := user.doJSON(t, "GET", "/api/resources", nil, map[string]string{
		"X-Cache-Bypass": "true",
	})
	expectStatus(t, resp, body, http.StatusOK)
	if got := resp.Header.Get("X-Cache"); got == "BYPASS" {
		t.Errorf("non-admin should not be able to bypass cache")
	}
}

func TestCache_AdminStatsAndPurge(t *testing.T) {
	admin := loginAsAdmin(t)
	// Touch a few cached endpoints.
	admin.doJSON(t, "GET", "/api/resources", nil, nil)
	admin.doJSON(t, "GET", "/api/governance/dictionary", nil, nil)

	resp, body := admin.doJSON(t, "GET", "/api/admin/cache/stats", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	stats := mustJSON(t, body)
	if stats["ttl"].(string) == "" {
		t.Errorf("stats missing ttl")
	}

	resp, body = admin.doJSON(t, "POST", "/api/admin/cache/purge", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if mustJSON(t, body)["status"] != "purged" {
		t.Errorf("expected purged status")
	}
}

func TestCache_NonAdminCannotAccessAdminEndpoints(t *testing.T) {
	user, _ := registerAndLogin(t, "noadmin")
	resp, _ := user.doJSON(t, "GET", "/api/admin/cache/stats", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}
