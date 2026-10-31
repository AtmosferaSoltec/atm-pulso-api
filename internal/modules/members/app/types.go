// Package app holds the use cases of members: enrolling, renewing, listing and the
// printable receipts. Persistence and HTTP are ports/adapters.
package app

import (
	"context"
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
	StartsOn       string     `json:"starts_on"`
	EndsOn         string     `json:"ends_on"`
	LastReceipt    *string    `json:"last_receipt"`
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
	ID             uuid.UUID      `json:"id"`
	Months         int            `json:"months"`
	PlanLabel      string         `json:"plan_label"`
	StartsOn       string         `json:"starts_on"`
	EndsOn         string         `json:"ends_on"`
	Amount         string         `json:"amount"`
	AccessScope    string         `json:"access_scope"`
	AccessBranches []AccessBranch `json:"access_branches"`
	Receipt        *ReceiptRef    `json:"receipt"`
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
	Amount    string    `json:"amount"`
	Method    string    `json:"method"`
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
	ID        uuid.UUID
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
	Method         string
	Today          time.Time
}

// Renewal is a new period for an existing member, collected at CollectedBranchID.
type Renewal struct {
	GymID             uuid.UUID
	ActorID           uuid.UUID
	MemberID          uuid.UUID
	PeriodID          uuid.UUID
	CollectedBranchID uuid.UUID
	Method            string
	Today             time.Time
}

// PlanFunc decides the period of a renewal once the repository has locked the member.
type PlanFunc func(homeBranchID uuid.UUID, latestEnd *time.Time) (Period, error)

// SaleIDs are the rows an enrollment or renewal created (or found, when repeated).
type SaleIDs struct {
	MemberID     uuid.UUID
	MembershipID uuid.UUID
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
	MemberReceipts(ctx context.Context, gymID, memberID uuid.UUID) ([]ReceiptSummary, error)
	Receipt(ctx context.Context, gymID, receiptID uuid.UUID) (ReceiptDetail, error)
}
