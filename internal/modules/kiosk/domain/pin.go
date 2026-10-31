// Package domain has the rules of the kiosk exit PIN. Plain Go: no HTTP, no SQL.
package domain

import (
	"errors"
	"time"
)

// PIN length bounds: digits only, so the tablet can show a numeric keypad.
const (
	MinPINLength = 4
	MaxPINLength = 8
)

// Wrong-PIN throttle: after MaxFailures wrong tries in a row the person is locked out for LockFor.
const (
	MaxFailures = 5
	LockFor     = 5 * time.Minute
)

var (
	// ErrForbidden: the role cannot do this.
	ErrForbidden = errors.New("forbidden")
	// ErrPINNotSet: the gym has no exit PIN yet.
	ErrPINNotSet = errors.New("kiosk pin not set")
	// ErrPINWrong: the PIN does not match.
	ErrPINWrong = errors.New("kiosk pin wrong")
	// ErrLocked: too many wrong PINs; try later.
	ErrLocked = errors.New("kiosk pin locked")
)

// Issue is one problem found in a field.
type Issue struct{ Field, Code string }

// ValidationError lists the problems of the submitted data.
type ValidationError struct{ Issues []Issue }

func (e *ValidationError) Error() string { return "validation failed" }

// CodePINInvalid is the field code of a PIN that is not 4 to 8 digits.
const CodePINInvalid = "PIN_INVALID"

// ValidPIN says whether pin is MinPINLength..MaxPINLength ASCII digits.
func ValidPIN(pin string) bool {
	if len(pin) < MinPINLength || len(pin) > MaxPINLength {
		return false
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
