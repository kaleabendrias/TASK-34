package api_tests

import (
	"net/http"
	"testing"
)

func TestAnalytics_TrackTopAndTrends(t *testing.T) {
	c, _ := registerAndLogin(t, "analytician")
	for _, et := range []string{"view", "favorite", "comment", "download"} {
		resp, body := c.doJSON(t, "POST", "/api/analytics/track", map[string]any{
			"event_type":  et,
			"target_type": "resource",
			"target_id":   SeedSlipA1,
		}, nil)
		expectStatus(t, resp, body, http.StatusAccepted)
	}

	resp, body := c.doJSON(t, "GET", "/api/analytics/top?days=7&limit=5", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if _, ok := mustJSON(t, body)["top"]; !ok {
		t.Errorf("missing top field")
	}

	resp, body = c.doJSON(t, "GET", "/api/analytics/trends?event_type=view", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	out := mustJSON(t, body)
	for _, k := range []string{"day_7", "day_30", "day_90"} {
		if _, ok := out[k]; !ok {
			t.Errorf("missing %s in trends", k)
		}
	}
}

func TestAnalytics_TrackInvalidInputs(t *testing.T) {
	c := newClient(t)
	// Missing fields
	resp, _ := c.doJSON(t, "POST", "/api/analytics/track", map[string]any{}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	// Bad UUID
	resp, _ = c.doJSON(t, "POST", "/api/analytics/track", map[string]any{
		"event_type": "view", "target_type": "resource", "target_id": "nope",
	}, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on bad uuid, got %d", resp.StatusCode)
	}
}

func TestAnalytics_AnomaliesAdminOnly(t *testing.T) {
	admin := loginAsAdmin(t)
	resp, body := admin.doJSON(t, "GET", "/api/admin/anomalies", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)

	user, _ := registerAndLogin(t, "noanon")
	resp, _ = user.doJSON(t, "GET", "/api/admin/anomalies", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}
