package domain

import "errors"

var (
	ErrCannotModifySelf  = errors.New("cannot modify self")
	ErrRoleNotAssignable = errors.New("role not assignable")
	ErrScopeNotAllowed   = errors.New("scope not allowed")
	ErrForeignScope      = errors.New("foreign scope")
	ErrForbidden         = errors.New("forbidden")
	ErrConflict          = errors.New("conflict")
	ErrResourceNotFound  = errors.New("resource not found")
	// ErrUserNotFound: no user matches.
	ErrUserNotFound = errors.New("user not found")
	// ErrDocumentTaken: another person of the gym already has that DNI.
	ErrDocumentTaken = errors.New("document number already in use")
	// ErrUserInOtherCompany: the person already works for another company; only Atmósfera can add them to a second one.
	ErrUserInOtherCompany = errors.New("user belongs to another company")
	// ErrAuthUnavailable: atm-auth-api is not configured or did not answer.
	ErrAuthUnavailable = errors.New("atm-auth-api unavailable")
	// ErrBranchInUse: the branch has history or people and cannot be removed.
	ErrBranchInUse = errors.New("branch in use")
)

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
	CodeFullNameInvalid       = "FULL_NAME_INVALID"
	CodePhoneInvalid          = "PHONE_INVALID"
)
