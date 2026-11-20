// Package domain has the rules of vouchers (proofs of payment): how many, how big, how long they are kept,
// and the typed errors. Plain Go: no HTTP, no SQL.
package domain

import (
	"errors"
	"time"
)

// MaxPerPayment is how many vouchers one payment may have: exactly one, to keep the object store small.
// The same limit applies to a product sale paid on the spot and to a collection of pending sales.
// Rows stored when the limit was five stay as they were.
const MaxPerPayment = 1

// OwnerKind is what a voucher proves: the payment of a receipt (memberships), a product sale paid on
// the spot, or a collection of pending product sales.
type OwnerKind string

// The owners a voucher can have.
const (
	OwnerPayment    OwnerKind = "payment"
	OwnerSale       OwnerKind = "sale"
	OwnerCollection OwnerKind = "collection"
)

// MethodCash is the one payment method that never takes a voucher.
const MethodCash = "cash"

// MaxBytes is the largest voucher accepted. The web and the app shrink photos before sending them.
const MaxBytes = 2 << 20

// Retention is how long the file of a voucher is kept; zero or less keeps it forever.
type Retention time.Duration

// RetentionDays builds a retention of whole days (zero or less: forever).
func RetentionDays(days int) Retention {
	if days <= 0 {
		return 0
	}
	return Retention(time.Duration(days) * 24 * time.Hour)
}

// Enabled says whether files are ever deleted.
func (r Retention) Enabled() bool { return r > 0 }

// ExpiresAt is when the file of a voucher created at createdAt is deleted; nil when it is kept forever.
func (r Retention) ExpiresAt(createdAt time.Time) *time.Time {
	if !r.Enabled() {
		return nil
	}
	at := createdAt.Add(time.Duration(r))
	return &at
}

// Expired says whether the file of a voucher is gone or due: already purged, or past its retention at now.
func (r Retention) Expired(createdAt time.Time, purged bool, now time.Time) bool {
	if purged {
		return true
	}
	at := r.ExpiresAt(createdAt)
	return at != nil && !now.Before(*at)
}

var (
	// ErrReceiptNotFound: no payment for that receipt in the gym (or out of reach).
	ErrReceiptNotFound = errors.New("receipt not found")
	// ErrSaleNotFound: no such product sale in the gym (or out of reach).
	ErrSaleNotFound = errors.New("sale not found")
	// ErrCollectionNotFound: no such collection of pending sales in the gym (or out of reach).
	ErrCollectionNotFound = errors.New("collection not found")
	// ErrSaleNotPaid: the sale is still pending; there is no money to prove yet.
	ErrSaleNotPaid = errors.New("sale not paid")
	// ErrSaleCollected: the sale was paid later by a collection; the proof belongs to that collection.
	ErrSaleCollected = errors.New("sale paid by a collection")
	// ErrFeatureDisabled: the voucher belongs to a module the gym does not have (sales).
	ErrFeatureDisabled = errors.New("feature disabled")
	// ErrVoucherNotFound: the voucher is not in the gym (or out of reach).
	ErrVoucherNotFound = errors.New("voucher not found")
	// ErrForbidden: the role cannot do this.
	ErrForbidden = errors.New("forbidden")
	// ErrLimitReached: the payment already has MaxPerPayment vouchers.
	ErrLimitReached = errors.New("voucher limit reached")
	// ErrExpired: the voucher's file was deleted (or is due) after the retention period; its row remains.
	ErrExpired = errors.New("voucher expired")
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
