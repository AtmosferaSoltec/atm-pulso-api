// Package domain has the rules of payment vouchers: how many, how big, and the typed errors.
// Plain Go: no HTTP, no SQL.
package domain

import "errors"

// MaxPerPayment is how many vouchers one payment may have (a payment split in several transfers).
const MaxPerPayment = 5

// MaxBytes is the largest voucher accepted. The web shrinks phone photos before sending them.
const MaxBytes = 10 << 20

var (
	// ErrReceiptNotFound: no payment for that receipt in the organization (or out of reach).
	ErrReceiptNotFound = errors.New("receipt not found")
	// ErrVoucherNotFound: the voucher is not in the organization (or out of reach).
	ErrVoucherNotFound = errors.New("voucher not found")
	// ErrForbidden: the role cannot do this.
	ErrForbidden = errors.New("forbidden")
	// ErrLimitReached: the payment already has MaxPerPayment vouchers.
	ErrLimitReached = errors.New("voucher limit reached")
	// ErrStorageUnavailable: no object store is configured (development without R2).
	ErrStorageUnavailable = errors.New("object storage unavailable")
)

// Issue is one problem found in a field.
type Issue struct{ Field, Code string }

// ValidationError lists the problems of the submitted data.
type ValidationError struct{ Issues []Issue }

func (e *ValidationError) Error() string { return "validation failed" }

// Field codes; the web picks its own Spanish text.
const (
	CodeNotForCash = "VOUCHER_NOT_FOR_CASH"
	CodeType       = "VOUCHER_TYPE_INVALID"
	CodeSize       = "VOUCHER_SIZE_INVALID"
)
