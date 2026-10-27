package domain

import (
	"errors"
	"time"
)

var (
	// ErrUserNotFound: no user matches.
	ErrUserNotFound = errors.New("user not found")
	// ErrInvalidCredentials is deliberately the same for an unknown DNI, a wrong
	// password and an inactive user: the caller cannot tell which one it was.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrDocumentTaken: another user already has that DNI.
	ErrDocumentTaken = errors.New("document number already in use")
	// ErrCurrentPasswordIncorrect: changing a password requires the current one.
	ErrCurrentPasswordIncorrect = errors.New("current password incorrect")
)

// LockedError means too many failed attempts: try again after RetryAfter.
type LockedError struct {
	RetryAfter time.Duration
}

func (e *LockedError) Error() string { return "too many failed attempts" }

// Issue is one problem found while validating a field.
type Issue struct {
	Field string
	Code  string
}

// ValidationError lists the problems of the submitted fields.
type ValidationError struct {
	Issues []Issue
}

func (e *ValidationError) Error() string { return "validation failed" }

// Field codes. The web picks its own Spanish text from them.
const (
	CodeDocumentNumberInvalid = "DOCUMENT_NUMBER_INVALID"
	CodePasswordRequired      = "PASSWORD_REQUIRED"
	CodeFullNameInvalid       = "FULL_NAME_INVALID"
	CodePhoneInvalid          = "PHONE_INVALID"
	CodePasswordTooShort      = "PASSWORD_TOO_SHORT"
	CodePasswordTooLong       = "PASSWORD_TOO_LONG"
	CodePasswordUnchanged     = "PASSWORD_UNCHANGED"
)
