package unit_tests

import (
	"strings"
	"testing"

	"github.com/harborworks/booking-hub/internal/service"
)

func TestValidateResourcesCSV_Success(t *testing.T) {
	csv := `name,description,capacity
Slip A1,North dock,1
Slip A2,South dock,2
`
	parsed, errs, fatal := service.ValidateResourcesCSV(strings.NewReader(csv))
	if fatal != nil {
		t.Fatalf("fatal: %v", fatal)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no row errors, got %v", errs)
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(parsed))
	}
	if parsed[0].Name != "Slip A1" || parsed[0].Capacity != 1 {
		t.Errorf("row 0: %#v", parsed[0])
	}
}

func TestValidateResourcesCSV_AllRowErrors(t *testing.T) {
	csv := `name,description,capacity
,no name,3
Bad Capacity,desc,abc
Negative,desc,-1
Dup,one,2
Dup,two,3
`
	parsed, errs, fatal := service.ValidateResourcesCSV(strings.NewReader(csv))
	if fatal != nil {
		t.Fatalf("unexpected fatal: %v", fatal)
	}
	if len(parsed) != 0 {
		t.Fatalf("all-or-nothing: parsed should be empty, got %d", len(parsed))
	}
	want := map[string]bool{
		"name":     false, // missing name row
		"capacity": false, // either bad capacity row should set this
	}
	for _, e := range errs {
		want[e.Field] = true
	}
	if !want["name"] || !want["capacity"] {
		t.Fatalf("expected at least one name + capacity error, got %v", errs)
	}
	// Duplicate row should be reported.
	dupSeen := false
	for _, e := range errs {
		if e.Reason == "duplicate within file" {
			dupSeen = true
		}
	}
	if !dupSeen {
		t.Errorf("duplicate row should be reported, errs=%v", errs)
	}
}

func TestValidateResourcesCSV_Errors(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		_, _, fatal := service.ValidateResourcesCSV(strings.NewReader(""))
		if fatal == nil {
			t.Fatal("expected fatal on empty csv")
		}
	})
	t.Run("missing column", func(t *testing.T) {
		_, _, fatal := service.ValidateResourcesCSV(strings.NewReader("name,description\nfoo,bar\n"))
		if fatal == nil {
			t.Fatal("expected missing-column fatal")
		}
	})
	t.Run("malformed CSV", func(t *testing.T) {
		_, _, fatal := service.ValidateResourcesCSV(strings.NewReader("name,description,capacity\n\"unterminated"))
		if fatal == nil {
			t.Fatal("expected parse fatal")
		}
	})
	t.Run("short row", func(t *testing.T) {
		csv := "name,description,capacity\nshorty\n"
		_, errs, fatal := service.ValidateResourcesCSV(strings.NewReader(csv))
		// CSV parser may return a fatal error for short rows; either fatal
		// or a row-level error is acceptable.
		if fatal == nil && len(errs) == 0 {
			t.Fatal("expected short-row error")
		}
	})
}
