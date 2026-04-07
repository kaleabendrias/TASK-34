package unit_tests

import (
	"errors"
	"strings"
	"testing"

	"github.com/harborworks/booking-hub/internal/domain"
)

func TestValidatePassword_Success(t *testing.T) {
	if err := domain.ValidatePassword("Harbor@Works2026!"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidatePassword_Failures(t *testing.T) {
	cases := []struct {
		name  string
		pw    string
		needs string
	}{
		{"too short", "Aa1!aa", "at least 12"},
		{"no lower", "HARBOR@1234567", "lowercase"},
		{"no upper", "harbor@1234567", "uppercase"},
		{"no digit", "Harbor@Workers!", "digit"},
		{"no symbol", "Harbor1Workers9", "symbol"},
		{"empty string", "", "at least 12"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := domain.ValidatePassword(c.pw)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, domain.ErrPasswordPolicy) {
				t.Errorf("expected ErrPasswordPolicy, got %v", err)
			}
			if !strings.Contains(err.Error(), c.needs) {
				t.Errorf("error %q should mention %q", err.Error(), c.needs)
			}
		})
	}
}

func TestValidatePassword_SpaceAndAccents(t *testing.T) {
	// Spaces alone are not symbols.
	if err := domain.ValidatePassword("Harbor Works12"); err == nil {
		t.Fatal("expected symbol requirement to fail when only spaces are added")
	}
	// Unicode letters: lowercase é counts as lower; should still need symbol.
	if err := domain.ValidatePassword("Harborwérks2026"); err == nil {
		t.Fatal("expected symbol requirement to fail")
	}
	if err := domain.ValidatePassword("Harborwérks2026!"); err != nil {
		t.Fatalf("unicode + symbol should pass: %v", err)
	}
}
