// Package app holds the use cases of members: enrolling, renewing, listing and the
// printable receipts. Persistence and HTTP are ports/adapters.
package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
)

// BranchInfo is a branch of the gym with its gym.
type BranchInfo = branchdir.Info

// Directory reads the gym's branches (owned by the accounts module; read-only).
type Directory interface {
	// Branch returns branchdir.ErrNotFound when it is not in the gym.
	Branch(ctx context.Context, gymID, branchID uuid.UUID) (BranchInfo, error)
	Branches(ctx context.Context, gymID uuid.UUID) ([]BranchInfo, error)
}

// MemberView is a member as listed.
type MemberView struct {
	ID             uuid.UUID  `json:"id"`
	DocumentType   string     `json:"document_type"`
	DocumentNumber string     `json:"document_number"`
	FullName       string     `json:"full_name"`
	Phone          string     `json:"phone"`
	HomeBranch     BranchName `json:"home_branch"`
	Status         string     `json:"status"`
	DaysRemaining  int        `json:"days_remaining"`
	PlanLabel      string     `json:"plan_label"`
	// PlanNames are the plans the member holds today (one per current period); for a member with
	// none current, the last plan they held; empty when they never had one.
	PlanNames   []string `json:"plan_names"`
	StartsOn    string   `json:"starts_on"`
	EndsOn      string   `json:"ends_on"`
	LastReceipt *string  `json:"last_receipt"`
}

// BranchName is a branch with the name of its gym.
type BranchName struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	GymName string    `json:"gym_name"`
}

// Counts are the members per status, ignoring the status filter.
type Counts struct {
	All       int `json:"all"`
	Active    int `json:"active"`
	Expiring  int `json:"expiring"`
	Expired   int `json:"expired"`
	Scheduled int `json:"scheduled"`
}

// Filter narrows the member list.
type Filter struct {
	Query        string
	Status       string
	HomeBranchID *uuid.UUID
	Page         int
	PageSize     int
}

// ListResult is one page plus the totals.
type ListResult struct {
	Items  []MemberView
	Total  int
	Counts Counts
}

// AccessBranch is a branch a period opens.
type AccessBranch struct {
	ID    uuid.UUID `json:"id"`
	Label string    `json:"label"`
}

// ReceiptRef points to a receipt.
type ReceiptRef struct {
	ID     uuid.UUID `json:"id"`
	Number string    `json:"number"`
}

// MembershipView is one sold period.
type MembershipView struct {
	ID uuid.UUID `json:"id"`
	// PlanID is the plan sold; PlanName is its name when it was sold (a photo, for old receipts).
	PlanID         uuid.UUID      `json:"plan_id"`
	PlanName       string         `json:"plan_name"`
	Months         int            `json:"months"`
	PlanLabel      string         `json:"plan_label"`
	StartsOn       string         `json:"starts_on"`
	EndsOn         string         `json:"ends_on"`
	Amount         string         `json:"amount"`
	AccessScope    string         `json:"access_scope"`
	AccessBranches []AccessBranch `json:"access_branches"`
	Receipt        *ReceiptRef    `json:"receipt"`
	// AmendedAt is when the period was last corrected (RFC 3339), null when never. See Amendments.
	AmendedAt *string `json:"amended_at"`
}

// MemberDetail is a member with every period.
type MemberDetail struct {
	MemberView
	Memberships []MembershipView `json:"memberships"`
}

// ReceiptSummary is a row of a member's receipt list.
type ReceiptSummary struct {
	ID        uuid.UUID `json:"id"`
	Number    string    `json:"number"`
	IssuedOn  string    `json:"issued_on"`
	PlanLabel string    `json:"plan_label"`
	// PlanName is the plan sold (null for a day pass).
	PlanName *string `json:"plan_name"`
	Amount   string  `json:"amount"`
	Method   string  `json:"method"`
}

// ReceiptDetail is everything the printable receipt shows.
type ReceiptDetail struct {
	ID       uuid.UUID `json:"id"`
	Number   string    `json:"number"`
	Kind     string    `json:"kind"`
	IssuedOn string    `json:"issued_on"`
	Gym      struct {
		ID         uuid.UUID `json:"-"`
		Name       string    `json:"name"`
		TaxID      *string   `json:"tax_id"`
		BrandColor string    `json:"brand_color"`
		// LogoURL is a temporary signed address of the logo in the company profile; null when none.
		LogoURL *string `json:"logo_url"`
	} `json:"gym"`
	Branch struct {
		Name    string  `json:"name"`
		Address *string `json:"address"`
	} `json:"branch"`
	Member     *ReceiptMember     `json:"member"`
	Membership *ReceiptMembership `json:"membership"`
	Payment    struct {
		Amount string `json:"amount"`
		Method string `json:"method"`
	} `json:"payment"`
}

// ReceiptMember is the customer on a receipt.
type ReceiptMember struct {
	FullName       string `json:"full_name"`
	DocumentType   string `json:"document_type"`
	DocumentNumber string `json:"document_number"`
	Phone          string `json:"phone"`
}

// ReceiptMembership is the sold period on a receipt.
type ReceiptMembership struct {
	PlanName       string         `json:"plan_name"`
	Months         int            `json:"months"`
	PlanLabel      string         `json:"plan_label"`
	StartsOn       string         `json:"starts_on"`
	EndsOn         string         `json:"ends_on"`
	AccessBranches []AccessBranch `json:"access_branches"`
}

// Sale is the result of an enrollment or a renewal.
type Sale struct {
	Member     MemberView     `json:"member"`
	Membership MembershipView `json:"membership"`
	Receipt    ReceiptRef     `json:"receipt"`
}

// Period is a membership about to be stored.
type Period struct {
	ID uuid.UUID
	// PlanID and PlanName are filled by the repository once it resolves the plan inside the transaction.
	PlanID    uuid.UUID
	PlanName  string
	Months    int
	StartsOn  time.Time
	EndsOn    time.Time
	Amount    string
	Scope     domain.Scope
	BranchIDs []uuid.UUID
}

// Enrollment is a new member with the first period, payment and receipt.
type Enrollment struct {
	GymID          uuid.UUID
	ActorID        uuid.UUID
	MemberID       uuid.UUID
	HomeBranchID   uuid.UUID
	DocumentType   string
	DocumentNumber string
	FullName       string
	Phone          string
	Period         Period
	// PlanID is the plan asked for; nil lets the repository pick the gym's only active plan.
	PlanID *uuid.UUID
	Method string
	Today  time.Time
}

// Renewal is a new period for an existing member, collected at CollectedBranchID.
type Renewal struct {
	GymID             uuid.UUID
	ActorID           uuid.UUID
	MemberID          uuid.UUID
	PeriodID          uuid.UUID
	CollectedBranchID uuid.UUID
	// PlanID is the plan asked for; nil lets the repository pick the gym's only active plan.
	PlanID *uuid.UUID
	Method string
	Today  time.Time
}

// PlanFunc decides the period of a renewal once the repository has locked the member.
type PlanFunc func(homeBranchID uuid.UUID, latestEnd *time.Time) (Period, error)

// SaleIDs are the rows an enrollment or renewal created (or found, when repeated).
type SaleIDs struct {
	MemberID     uuid.UUID
	MembershipID uuid.UUID
}

// AmendInput is the body of PATCH /members/{id}/memberships/{membership_id}: every field is optional but at
// least one is required. The end date is never sent: the server recalculates it.
type AmendInput struct {
	Months   *int    `json:"months"`
	StartsOn *string `json:"starts_on"`
	Amount   *string `json:"amount"`
	Method   *string `json:"method"`
}

// CurrentPeriod is a stored period with the payment that sold it, as the repository found it (locked).
type CurrentPeriod struct {
	PlanID   uuid.UUID
	Months   int
	StartsOn time.Time
	EndsOn   time.Time
	Amount   string
	// PaymentID, Method and PaymentBranchID are nil/empty for a period without a payment.
	PaymentID       *uuid.UUID
	Method          string
	PaymentBranchID *uuid.UUID
}

// PeriodChange is the new state of a corrected period.
type PeriodChange struct {
	Months   int
	StartsOn time.Time
	EndsOn   time.Time
	Amount   string
	Method   string
}

// AmendFunc decides the correction once the repository has locked the member and read the period. It
// returns domain errors (ended, forbidden branch, validation).
type AmendFunc func(cur CurrentPeriod) (PeriodChange, error)

// AmendmentView is one row of the history of a period: who corrected it, when, and the values before and after.
type AmendmentView struct {
	ID        uuid.UUID `json:"id"`
	ChangedAt string    `json:"changed_at"`
	ChangedBy struct {
		ID       uuid.UUID `json:"id"`
		FullName string    `json:"full_name"`
	} `json:"changed_by"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

// Repository is the persistence port.
type Repository interface {
	List(ctx context.Context, gymID uuid.UUID, filter Filter, today time.Time) (ListResult, error)
	Member(ctx context.Context, gymID, memberID uuid.UUID, today time.Time) (MemberDetail, error)
	UpdateMember(ctx context.Context, gymID, memberID uuid.UUID, name, phone string) error
	// Enroll stores everything in one transaction. existed is true when the same id was
	// already enrolled (idempotent repeat). domain.DocumentTakenError when the document is used.
	Enroll(ctx context.Context, e Enrollment) (ids SaleIDs, existed bool, err error)
	Renew(ctx context.Context, r Renewal, plan PlanFunc) (ids SaleIDs, existed bool, err error)
	// AmendMembership locks the member, reads the period and applies what decide returns, rechecking that it does not overlap
	// another period of the same plan; it updates the payment of the receipt and records an amendment, all in one transaction.
	// changed is false when decide returned the same values (nothing written). domain.ErrMembershipNotFound, *domain.OverlapError.
	AmendMembership(ctx context.Context, gymID, actorID, memberID, membershipID uuid.UUID, decide AmendFunc) (changed bool, err error)
	// Amendments lists the corrections of a period, oldest first.
	Amendments(ctx context.Context, gymID, memberID, membershipID uuid.UUID) ([]AmendmentView, error)
	MemberReceipts(ctx context.Context, gymID, memberID uuid.UUID) ([]ReceiptSummary, error)
	Receipt(ctx context.Context, gymID, receiptID uuid.UUID) (ReceiptDetail, error)

	// Plans lists the gym's membership plans with their prices (only the active ones when activeOnly).
	Plans(ctx context.Context, gymID uuid.UUID, activeOnly bool) ([]Plan, error)
	// CreatePlan returns domain.ErrPlanNameTaken or domain.ErrConflict (the id belongs to something else).
	CreatePlan(ctx context.Context, gymID uuid.UUID, plan Plan) (Plan, error)
	// UpdatePlan returns domain.ErrPlanNotFound or domain.ErrPlanNameTaken.
	UpdatePlan(ctx context.Context, gymID, planID uuid.UUID, changes PlanChanges) (Plan, error)
}
