// Package domain has the rules of vouchers (proofs of payment): how many, how big, and the typed errors.
// Plain Go: no HTTP, no SQL.
package domain

import "errors"

// MaxPerPayment is how many vouchers one payment may have (a payment split in several transfers).
// The same limit applies to a product sale paid on the spot and to a collection of pending sales.
const MaxPerPayment = 5

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

// MaxBytes is the largest voucher accepted. The web shrinks phone photos before sending them.
const MaxBytes = 10 << 20

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
