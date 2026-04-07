package api_tests

import (
	"bytes"
	"net/http"
	"testing"
)

func TestDocument_PDFGenerationAndSupersession(t *testing.T) {
	c, _ := registerAndLogin(t, "doctor")
	relatedID := SeedSlipA1

	// First confirmation: revision 1.
	resp, body := c.doJSON(t, "POST", "/api/documents/confirmation", map[string]any{
		"related_type": "booking",
		"related_id":   relatedID,
		"title":        "First",
		"fields":       map[string]string{"resource": "Slip A1"},
	}, map[string]string{"Idempotency-Key": idemKey("doc1")})
	expectStatus(t, resp, body, http.StatusCreated)
	first := mustJSON(t, body)
	if r, _ := first["revision"].(float64); r != 1 {
		t.Errorf("first revision = %v, want 1", first["revision"])
	}
	doc, _ := first["document"].(map[string]any)
	docID, _ := doc["id"].(string)

	// Re-generate: revision 2 + first revision now superseded.
	resp, body = c.doJSON(t, "POST", "/api/documents/confirmation", map[string]any{
		"related_type": "booking",
		"related_id":   relatedID,
		"title":        "Second",
		"fields":       map[string]string{"resource": "Slip A1"},
	}, map[string]string{"Idempotency-Key": idemKey("doc2")})
	expectStatus(t, resp, body, http.StatusCreated)
	second := mustJSON(t, body)
	if r, _ := second["revision"].(float64); r != 2 {
		t.Errorf("second revision = %v, want 2", second["revision"])
	}

	// Get with revision history.
	resp, body = c.doJSON(t, "GET", "/api/documents/"+docID, nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	d := mustJSON(t, body)
	revs, _ := d["revisions"].([]any)
	if len(revs) != 2 {
		t.Fatalf("expected 2 revisions, got %d", len(revs))
	}
	// Find revision 1 and confirm superseded=true.
	supersededFound := false
	for _, r := range revs {
		rm := r.(map[string]any)
		if num, _ := rm["revision"].(float64); num == 1 {
			if sup, _ := rm["superseded"].(bool); sup {
				supersededFound = true
			}
		}
	}
	if !supersededFound {
		t.Errorf("revision 1 should be marked superseded")
	}

	// Download current PDF and verify magic bytes.
	resp, body = c.doRaw(t, "GET", "/api/documents/"+docID+"/content", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if !bytes.HasPrefix(body, []byte("%PDF-1.4")) {
		t.Fatalf("not a PDF: %v", body[:8])
	}
	if resp.Header.Get("X-Revision") != "2" {
		t.Errorf("X-Revision = %s", resp.Header.Get("X-Revision"))
	}

	// Download rev 1 explicitly: must be marked superseded.
	resp, body = c.doRaw(t, "GET", "/api/documents/"+docID+"/content?revision=1", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	if resp.Header.Get("X-Superseded") != "true" {
		t.Errorf("rev1 X-Superseded = %s", resp.Header.Get("X-Superseded"))
	}

	// List my documents.
	resp, body = c.doJSON(t, "GET", "/api/documents", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
}

func TestDocument_PNGCheckinPass(t *testing.T) {
	c, _ := registerAndLogin(t, "passer")
	resp, body := c.doJSON(t, "POST", "/api/documents/checkin-pass", map[string]any{
		"related_type": "booking",
		"related_id":   SeedMooringM7,
		"title":        "Pass",
		"fields":       map[string]string{"name": "Test"},
	}, map[string]string{"Idempotency-Key": idemKey("png")})
	expectStatus(t, resp, body, http.StatusCreated)
	doc, _ := mustJSON(t, body)["document"].(map[string]any)
	id, _ := doc["id"].(string)

	resp, body = c.doRaw(t, "GET", "/api/documents/"+id+"/content", nil, nil)
	expectStatus(t, resp, body, http.StatusOK)
	pngMagic := []byte{0x89, 0x50, 0x4E, 0x47}
	if !bytes.HasPrefix(body, pngMagic) {
		t.Fatalf("not a PNG: %v", body[:4])
	}
}

func TestDocument_OwnerOnly(t *testing.T) {
	a, _ := registerAndLogin(t, "owner")
	b, _ := registerAndLogin(t, "stranger")

	resp, body := a.doJSON(t, "POST", "/api/documents/confirmation", map[string]any{
		"related_type": "booking",
		"related_id":   SeedSlipA2,
		"title":        "OwnerOnly",
	}, nil)
	expectStatus(t, resp, body, http.StatusCreated)
	docID, _ := mustJSON(t, body)["document"].(map[string]any)
	id, _ := docID["id"].(string)

	resp, _ = b.doJSON(t, "GET", "/api/documents/"+id, nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	resp, _ = b.doRaw(t, "GET", "/api/documents/"+id+"/content", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("content expected 403, got %d", resp.StatusCode)
	}
}
