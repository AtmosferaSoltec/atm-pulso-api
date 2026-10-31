// Package domain holds the pure rules of the products module: the catalog, quick sales of units and pending
// sales (pending sales). Money is handled in integer cents. There is no inventory, no receipt and no tax here. No I/O.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Limits, repeated as CHECK constraints in the migration.
const (
	MaxNameLength   = 60
	MaxPriceCents   = 10_000_00    // a unit costs at most 10000.00
	MaxQuantity     = 99           // units of one product in one sale
	MaxItems        = 30           // different products in one sale
	MaxSaleCents    = 100_000_00   // a sale totals at most 100000.00
	MaxCollectCents = 1_000_000_00 // one collection collects at most 1000000.00
)

// Sale statuses.
const (
	StatusPaid    = "paid"
	StatusPending = "pending"
)

// Methods are the accepted payment methods (the same as memberships).
var Methods = map[string]bool{"yape_plin": true, "cash": true, "card": true, "transfer": true}

var moneyPattern = regexp.MustCompile(`^[0-9]{1,7}(\.[0-9]{1,2})?$`)

// ParseMoney reads "10", "10.5" or "10.50" into cents.
func ParseMoney(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if !moneyPattern.MatchString(raw) {
		return 0, false
	}
	whole, frac, _ := strings.Cut(raw, ".")
	frac = (frac + "00")[:2]
	var cents int64
	for _, digit := range whole + frac {
		cents = cents*10 + int64(digit-'0')
	}
	return cents, true
}

// FormatMoney writes cents as the text money of the API, "10.00".
func FormatMoney(cents int64) string { return fmt.Sprintf("%d.%02d", cents/100, cents%100) }

// ProductName trims and collapses spaces; a valid name has 1 to 60 characters.
func ProductName(raw string) (string, bool) {
	name := strings.Join(strings.Fields(raw), " ")
	n := utf8.RuneCountInString(name)
	return name, n >= 1 && n <= MaxNameLength
}

// ProductPrice validates a unit price: 0.01 to 10000.00.
func ProductPrice(raw string) (int64, bool) {
	cents, ok := ParseMoney(raw)
	return cents, ok && cents > 0 && cents <= MaxPriceCents
}

// Line is one product of a sale as the client sent it.
type Line struct {
	ProductID uuid.UUID
	Quantity  int
}

// MergeLines adds up repeated products and keeps the order of first appearance. It reports false when there are
// no lines, too many, a non-positive quantity or a product over MaxQuantity units.
func MergeLines(lines []Line) ([]Line, bool) {
	if len(lines) == 0 || len(lines) > 4*MaxItems {
		return nil, false
	}
	index := map[uuid.UUID]int{}
	var out []Line
	for _, line := range lines {
		if line.ProductID == uuid.Nil || line.Quantity < 1 || line.Quantity > MaxQuantity {
			return nil, false
		}
		if at, seen := index[line.ProductID]; seen {
			out[at].Quantity += line.Quantity
			if out[at].Quantity > MaxQuantity {
				return nil, false
			}
			continue
		}
		index[line.ProductID] = len(out)
		out = append(out, line)
	}
	return out, len(out) <= MaxItems
}

// Errors of the module; the HTTP adapter translates them.
var (
	ErrProductNotFound     = errors.New("product not found")
	ErrProductNameTaken    = errors.New("product name taken")
	ErrSaleNotFound        = errors.New("sale not found")
	ErrMemberNotFound      = errors.New("member not found")
	ErrBranchNotAccessible = errors.New("branch not accessible")
	ErrForbidden           = errors.New("forbidden")
	ErrConflict            = errors.New("conflict")
	// ErrSaleNotPending: a sale to collect is not an open pending sale of that member (already collected by someone else).
	ErrSaleNotPending = errors.New("sale not pending")
	// ErrNothingToCollect: the member owes nothing.
	ErrNothingToCollect = errors.New("nothing to collect")
)

// Issue is one problem found in a field.
type Issue struct{ Field, Code string }

// ValidationError lists the problems of the submitted fields.
type ValidationError struct{ Issues []Issue }

func (e *ValidationError) Error() string { return "validation failed" }

// Field codes; the web picks its own Spanish text.
const (
	CodeName           = "PRODUCT_NAME_INVALID"
	CodePrice          = "PRODUCT_PRICE_INVALID"
	CodePosition       = "PRODUCT_POSITION_INVALID"
	CodeItems          = "ITEMS_INVALID"
	CodeProduct        = "PRODUCT_INVALID"
	CodeStatus         = "STATUS_INVALID"
	CodeMethod         = "METHOD_INVALID"
	CodeMemberRequired = "MEMBER_REQUIRED"
	CodeMember         = "MEMBER_INVALID"
	CodeTotal          = "TOTAL_TOO_LARGE"
	CodeDate           = "DATE_INVALID"
	CodeRange          = "DATE_RANGE_INVALID"
	CodeGroupBy        = "GROUP_BY_INVALID"
	CodeSaleIDs        = "SALE_IDS_INVALID"
)
