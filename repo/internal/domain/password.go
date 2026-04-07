package domain

import (
	"errors"
	"fmt"
	"unicode"
)

// Password policy constants. Centralised so the UI and the validator agree.
const (
	PasswordMinLength = 12
)

// ValidatePassword enforces the HarborWorks password policy:
//   - at least 12 characters long
//   - contains at least one lowercase letter
//   - contains at least one uppercase letter
//   - contains at least one digit
//   - contains at least one symbol (anything not letter/digit/space)
//
// All violations are reported in a single joined error so a UI can list them.
func ValidatePassword(pw string) error {
	var (
		hasLower, hasUpper, hasDigit, hasSymbol bool
		errs                                    []error
	)

	if len(pw) < PasswordMinLength {
		errs = append(errs, fmt.Errorf("must be at least %d characters", PasswordMinLength))
	}

	for _, r := range pw {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsSpace(r):
			// spaces don't count toward symbol requirement
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSymbol = true
		}
	}

	if !hasLower {
		errs = append(errs, errors.New("must include a lowercase letter"))
	}
	if !hasUpper {
		errs = append(errs, errors.New("must include an uppercase letter"))
	}
	if !hasDigit {
		errs = append(errs, errors.New("must include a digit"))
	}
	if !hasSymbol {
		errs = append(errs, errors.New("must include a symbol"))
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(append([]error{ErrPasswordPolicy}, errs...)...)
}
