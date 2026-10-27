// Package receipts issues the numbered receipts and records the payments of every sale
// (memberships and day passes). Both are written inside the caller's transaction, so a
// failed sale leaves no gap in the numbering.
package receipts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
)

// Kind separates the two numbering series.
type Kind string

const (
	Membership Kind = "membership"
	DayPass    Kind = "day_pass"
)

// Methods are the accepted payment methods.
var Methods = map[string]bool{"yape_plin": true, "cash": true, "card": true, "transfer": true}

// Label formats a receipt number: R-000012 or D-000003.
func Label(kind Kind, number int) string {
	prefix := "R"
	if kind == DayPass {
		prefix = "D"
	}
	return fmt.Sprintf("%s-%06d", prefix, number)
}

// Issue allocates the next number of the organization's series and stores the receipt.
// The counter row is locked by the upsert until the transaction ends, so concurrent
// sales get distinct, gapless numbers.
func Issue(ctx context.Context, tx pgx.Tx, organizationID uuid.UUID, kind Kind, branchID uuid.UUID, issuedOn time.Time) (uuid.UUID, int, error) {
	var number int
	err := tx.QueryRow(ctx, `INSERT INTO receipt_counters (organization_id, kind, last_number) VALUES ($1, $2, 1)
		ON CONFLICT (organization_id, kind) DO UPDATE SET last_number = receipt_counters.last_number + 1
		RETURNING last_number`, organizationID, string(kind)).Scan(&number)
	if err != nil {
		return uuid.Nil, 0, err
	}
	id := ids.New()
	_, err = tx.Exec(ctx, `INSERT INTO receipts (id, organization_id, kind, number, branch_id, issued_on) VALUES ($1,$2,$3,$4,$5,$6)`,
		id, organizationID, string(kind), number, branchID, issuedOn)
	return id, number, err
}

// Payment is what a sale collected.
type Payment struct {
	OrganizationID uuid.UUID
	ReceiptID      uuid.UUID
	BranchID       uuid.UUID
	Kind           Kind
	Method         string
	Amount         string
	PaidOn         time.Time
	MemberID       *uuid.UUID
	MembershipID   *uuid.UUID
	DayPassID      *uuid.UUID
	CreatedBy      uuid.UUID
}

// Record stores the payment of a receipt.
func Record(ctx context.Context, tx pgx.Tx, p Payment) error {
	_, err := tx.Exec(ctx, `INSERT INTO payments (id, organization_id, receipt_id, branch_id, kind, method, amount, paid_on, member_id, membership_id, day_pass_id, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7::numeric,$8,$9,$10,$11,$12)`,
		ids.New(), p.OrganizationID, p.ReceiptID, p.BranchID, string(p.Kind), p.Method, p.Amount, p.PaidOn, p.MemberID, p.MembershipID, p.DayPassID, p.CreatedBy)
	return err
}
