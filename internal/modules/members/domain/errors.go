package domain

import (
	"errors"

	"github.com/google/uuid"
)

var (
	// ErrMemberNotFound: the member does not exist in the organization.
	ErrMemberNotFound = errors.New("member not found")
	// ErrReceiptNotFound: the receipt does not exist in the organization.
	ErrReceiptNotFound = errors.New("receipt not found")
	// ErrBranchNotFound: the branch does not exist in the organization.
	ErrBranchNotFound = errors.New("branch not found")
	// ErrBranchNotAccessible: the user cannot work in that branch.
	ErrBranchNotAccessible = errors.New("branch not accessible")
	// ErrForbidden: the role cannot do this (the superadmin only consults).
	ErrForbidden = errors.New("forbidden")
)

// DocumentTakenError carries the member that already uses the document.
type DocumentTakenError struct{ MemberID uuid.UUID }

func (e *DocumentTakenError) Error() string { return "member document already registered" }

// Issue is one problem found in a field.
type Issue struct{ Field, Code string }

// ValidationError lists the problems of the submitted fields.
type ValidationError struct{ Issues []Issue }

func (e *ValidationError) Error() string { return "validation failed" }

// Field codes; the web picks its own Spanish text.
const (
	CodeDocumentType   = "DOCUMENT_TYPE_INVALID"
	CodeDocumentNumber = "DOCUMENT_NUMBER_INVALID"
	CodeFullName       = "FULL_NAME_INVALID"
	CodePhone          = "PHONE_INVALID"
	CodeMonths         = "MONTHS_INVALID"
	CodeStartDate      = "START_DATE_INVALID"
	CodeAmount         = "AMOUNT_INVALID"
	CodeMethod         = "METHOD_INVALID"
	CodeAccessScope    = "ACCESS_SCOPE_INVALID"
)

// ErrConflict: the id sent by the client already belongs to something else.
var ErrConflict = errors.New("conflict")
