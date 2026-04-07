package api_tests

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestBackup_FullIncrementalListAndRestorePlan(t *testing.T) {
	admin := loginAsAdmin(t)

	resp, body := admin.doJSON(t, "POST", "/api/admin/backups/full", nil, nil)
	expectStatus(t, resp, body, http.StatusCreated)
	full := mustJSON(t, body)
	if k, _ := full["kind"].(string); k != "full" {
		t.Errorf("expected kind=full")
	}
	fullPath, _ := full["path"].(string)

	resp, body = admin.doJSON(t, "POST", "/api/admin/backups/incremental", nil, nil)
	expectStatus(t, resp, body, http.StatusCreated)

	resp, body = admin.doJSON(t, "GET", "/api/admin/backups", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if cnt, _ := mustJSON(t, body)["count"].(float64); cnt < 2 {
		t.Errorf("expected ≥2 backups, got %v", cnt)
	}

	resp, body = admin.doJSON(t, "GET", "/api/admin/backups/restore-plan", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	plan := mustJSON(t, body)
	steps, _ := plan["steps"].([]any)
	if len(steps) == 0 {
		t.Fatal("plan has no steps")
	}
	first, _ := steps[0].(map[string]any)
	if k, _ := first["kind"].(string); k != "full" {
		t.Errorf("plan must start with full, got %v", first["kind"])
	}
	if sla, _ := plan["sla_hours"].(float64); sla != 4 {
		t.Errorf("expected sla_hours=4, got %v", sla)
	}

	// Verify the file actually exists on the mounted /backups volume.
	if !strings.HasPrefix(fullPath, "/backups/") {
		t.Errorf("path should be in /backups/, got %s", fullPath)
	}
	if _, err := os.Stat(fullPath); err != nil {
		t.Errorf("backup file missing on disk: %v", err)
	}
}

func TestBackup_NonAdminBlocked(t *testing.T) {
	user, _ := registerAndLogin(t, "nobk")
	gets := []string{
		"/api/admin/backups",
		"/api/admin/backups/restore-plan",
		"/api/admin/cache/stats",
	}
	for _, p := range gets {
		resp, _ := user.doJSON(t, "GET", p, nil, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s expected 403, got %d", p, resp.StatusCode)
		}
	}
	posts := []string{
		"/api/admin/backups/full",
		"/api/admin/backups/incremental",
		"/api/admin/cache/purge",
		"/api/admin/webhooks/00000000-0000-0000-0000-000000000000/disable",
	}
	for _, p := range posts {
		resp, _ := user.doJSON(t, "POST", p, nil, nil)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("POST %s expected 403, got %d", p, resp.StatusCode)
		}
	}
}
