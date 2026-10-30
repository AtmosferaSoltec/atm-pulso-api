// Package app holds the use cases of access: checking a member in at a branch, the entry
// history and day passes.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/access/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/receipts"
)

// Directory reads the branches of the organization (platform/branchdir implements it).
type Directory interface {
	Branch(ctx context.Context, organizationID, branchID uuid.UUID) (branchdir.Info, error)
	Branches(ctx context.Context, organizationID uuid.UUID) ([]branchdir.Info, error)
}

// MemberAccess is a member with what the decision needs.
type MemberAccess struct {
	ID             uuid.UUID
	FullName       string
	DocumentNumber string
	HomeBranchID   uuid.UUID
	HomeSharing    bool
	Periods        []domain.Period
}

// Entry is one logged access attempt.
type Entry struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	BranchID       uuid.UUID
	MemberID       *uuid.UUID
	DisplayName    string
	DocumentNumber *string
	Allowed        bool
	Reason         string
	Kind           string
	EndsOn         *time.Time
	ExpiringInDays *int
	CheckedBy      uuid.UUID
	CheckedAt      time.Time
}

// EventFilter narrows the history.
type EventFilter struct {
	BranchIDs []uuid.UUID
	Result    string
	From, To  *time.Time // Lima dates, inclusive
	Query     string
	Page      int
	PageSize  int
}

// Summary counts a day.
type Summary struct {
	Allowed   int `json:"allowed"`
	Denied    int `json:"denied"`
	DayPasses int `json:"day_passes"`
}

// Visitor is the optional customer data of a day pass.
type Visitor struct {
	DocumentType   string `json:"document_type"`
	DocumentNumber string `json:"document_number"`
	FullName       string `json:"full_name"`
	Phone          string `json:"phone"`
}

// DayPassSale is a day pass about to be stored with its receipt, payment and entry.
type DayPassSale struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	ActorID        uuid.UUID
	BranchID       uuid.UUID
	Amount         string
	Method         string
	Visitor        Visitor
	Today          time.Time
}

// DayPassFilter narrows the day pass list.
type DayPassFilter struct {
	BranchIDs []uuid.UUID
	From, To  *time.Time
	Page      int
	PageSize  int
}

// Repository is the persistence port.
type Repository interface {
	// Member finds by id or by document; domain.ErrMemberNotFound when there is none.
	Member(ctx context.Context, organizationID uuid.UUID, id *uuid.UUID, document string) (MemberAccess, error)
	Entry(ctx context.Context, organizationID, id uuid.UUID) (Entry, error)
	InsertEntry(ctx context.Context, e Entry) error
	Events(ctx context.Context, organizationID uuid.UUID, f EventFilter) ([]EventView, int, error)
	Summary(ctx context.Context, organizationID uuid.UUID, branchIDs []uuid.UUID, from, to time.Time) (Summary, error)
	SellDayPass(ctx context.Context, sale DayPassSale) (existed bool, err error)
	DayPass(ctx context.Context, organizationID, id uuid.UUID) (DayPassView, error)
	DayPasses(ctx context.Context, organizationID uuid.UUID, f DayPassFilter) ([]DayPassView, int, error)
}

// Service implements the use cases.
type Service struct {
	Repo      Repository
	Directory Directory
	Clock     clock.Clock
}

func (s *Service) today() time.Time { return clock.LimaDate(s.Clock.Now()) }

func (s *Service) branchFor(ctx context.Context, p auth.Principal, id uuid.UUID) (branchdir.Info, error) {
	branch, err := s.Directory.Branch(ctx, p.OrganizationID, id)
	if err != nil {
		return branchdir.Info{}, err
	}
	if !p.CanAccessBranch(auth.BranchRef{ID: branch.ID, GymID: branch.GymID}) {
		return branchdir.Info{}, domain.ErrBranchNotAccessible
	}
	return branch, nil
}

// reach returns the branches the caller may see, narrowed to one when branchID is set.
func (s *Service) reach(ctx context.Context, p auth.Principal, branchID *uuid.UUID) ([]uuid.UUID, error) {
	if branchID != nil {
		branch, err := s.branchFor(ctx, p, *branchID)
		if err != nil {
			return nil, err
		}
		return []uuid.UUID{branch.ID}, nil
	}
	all, err := s.Directory.Branches(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	out := []uuid.UUID{}
	for _, b := range all {
		if p.CanAccessBranch(auth.BranchRef{ID: b.ID, GymID: b.GymID}) {
			out = append(out, b.ID)
		}
	}
	return out, nil
}

// CheckInput is the body of POST /access/checks.
type CheckInput struct {
	ID             uuid.UUID  `json:"id"`
	BranchID       uuid.UUID  `json:"branch_id"`
	MemberID       *uuid.UUID `json:"member_id"`
	DocumentNumber string     `json:"document_number"`
}

// MemberSummary identifies the member of an answer.
type MemberSummary struct {
	ID             uuid.UUID `json:"id"`
	FullName       string    `json:"full_name"`
	DocumentNumber string    `json:"document_number"`
}

// CheckResult is the answer to a check. A denied check is still a successful request.
type CheckResult struct {
	EventID        uuid.UUID      `json:"event_id"`
	Allowed        bool           `json:"allowed"`
	Reason         string         `json:"reason"`
	ExpiringInDays *int           `json:"expiring_in_days"`
	EndsOn         *string        `json:"ends_on"`
	Member         *MemberSummary `json:"member"`
	CheckedAt      time.Time      `json:"checked_at"`
}

func formatDate(t *time.Time) *string {
	if t == nil {
		return nil
	}
	out := t.Format("2006-01-02")
	return &out
}

func resultOf(e Entry) CheckResult {
	r := CheckResult{EventID: e.ID, Allowed: e.Allowed, Reason: e.Reason, ExpiringInDays: e.ExpiringInDays, EndsOn: formatDate(e.EndsOn), CheckedAt: e.CheckedAt}
	if e.MemberID != nil {
		number := ""
		if e.DocumentNumber != nil {
			number = *e.DocumentNumber
		}
		r.Member = &MemberSummary{ID: *e.MemberID, FullName: e.DisplayName, DocumentNumber: number}
	}
	return r
}

// Check decides and logs one access attempt. The decision is made on the server.
func (s *Service) Check(ctx context.Context, p auth.Principal, in CheckInput) (CheckResult, error) {
	branch, err := s.branchFor(ctx, p, in.BranchID)
	if err != nil {
		return CheckResult{}, err
	}
	if in.MemberID == nil && in.DocumentNumber == "" {
		return CheckResult{}, &domain.ValidationError{Issues: []domain.Issue{{Field: "document_number", Code: "DOCUMENT_NUMBER_INVALID"}}}
	}
	if in.ID != uuid.Nil {
		if existing, err := s.Repo.Entry(ctx, p.OrganizationID, in.ID); err == nil {
			return resultOf(existing), nil
		}
	}
	entry := Entry{ID: in.ID, OrganizationID: p.OrganizationID, BranchID: branch.ID, Kind: "check", CheckedBy: p.UserID}
	if entry.ID == uuid.Nil {
		entry.ID = ids.New()
	}
	member, err := s.Repo.Member(ctx, p.OrganizationID, in.MemberID, in.DocumentNumber)
	switch {
	case err == domain.ErrMemberNotFound && in.MemberID != nil:
		return CheckResult{}, err
	case err == domain.ErrMemberNotFound:
		number := in.DocumentNumber
		entry.DisplayName, entry.DocumentNumber, entry.Reason = "No registrado", &number, domain.ReasonNotRegister
	case err != nil:
		return CheckResult{}, err
	default:
		d := domain.Decide(member.Periods, branch.ID, member.HomeBranchID, member.HomeSharing, s.today())
		number := member.DocumentNumber
		entry.MemberID, entry.DisplayName, entry.DocumentNumber = &member.ID, member.FullName, &number
		entry.Allowed, entry.Reason, entry.EndsOn, entry.ExpiringInDays = d.Allowed, d.Reason, d.EndsOn, d.ExpiringInDays
	}
	if err := s.Repo.InsertEntry(ctx, entry); err != nil {
		return CheckResult{}, err
	}
	// The stored time is assigned by the database; read it back so the answer carries it.
	stored, err := s.Repo.Entry(ctx, p.OrganizationID, entry.ID)
	if err != nil {
		return CheckResult{}, err
	}
	return resultOf(stored), nil
}

// EventView is a row of the history.
type EventView struct {
	ID             uuid.UUID  `json:"id"`
	CheckedAt      time.Time  `json:"checked_at"`
	Branch         NamedID    `json:"branch"`
	Kind           string     `json:"kind"`
	Allowed        bool       `json:"allowed"`
	Reason         string     `json:"reason"`
	DisplayName    string     `json:"display_name"`
	DocumentNumber *string    `json:"document_number"`
	MemberID       *uuid.UUID `json:"member_id"`
}

// NamedID is an id with a name.
type NamedID struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// EventsQuery is the parsed query of GET /access/events.
type EventsQuery struct {
	BranchID *uuid.UUID
	Result   string
	From, To *time.Time
	Query    string
	Page     int
	PageSize int
}

// Events lists the history of the branches the caller reaches, newest first.
func (s *Service) Events(ctx context.Context, p auth.Principal, q EventsQuery) ([]EventView, int, error) {
	branches, err := s.reach(ctx, p, q.BranchID)
	if err != nil {
		return nil, 0, err
	}
	return s.Repo.Events(ctx, p.OrganizationID, EventFilter{BranchIDs: branches, Result: q.Result, From: q.From, To: q.To, Query: q.Query, Page: q.Page, PageSize: q.PageSize})
}

// SummaryOf counts a Lima day (today when date is nil).
func (s *Service) SummaryOf(ctx context.Context, p auth.Principal, branchID *uuid.UUID, date *time.Time) (Summary, error) {
	branches, err := s.reach(ctx, p, branchID)
	if err != nil {
		return Summary{}, err
	}
	day := s.today()
	if date != nil {
		day = *date
	}
	return s.Repo.Summary(ctx, p.OrganizationID, branches, day, day)
}

// DayPassView is a sold day pass.
type DayPassView struct {
	ID          uuid.UUID  `json:"id"`
	Receipt     ReceiptRef `json:"receipt"`
	Branch      NamedID    `json:"branch"`
	Amount      string     `json:"amount"`
	Method      string     `json:"method"`
	VisitorName *string    `json:"visitor_name"`
	Visitor     *Visitor   `json:"visitor,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ReceiptRef points to a receipt.
type ReceiptRef struct {
	ID     uuid.UUID `json:"id"`
	Number string    `json:"number"`
}

// DayPassInput is the body of POST /day-passes. The price is the gym's, never the client's:
// an amount is accepted only when it equals it.
type DayPassInput struct {
	ID       uuid.UUID `json:"id"`
	BranchID uuid.UUID `json:"branch_id"`
	Amount   string    `json:"amount"`
	Method   string    `json:"method"`
	Visitor  *Visitor  `json:"visitor"`
}

// SellDayPass sells a single entry: day pass, receipt D-NNNNNN, payment and an allowed entry.
func (s *Service) SellDayPass(ctx context.Context, p auth.Principal, in DayPassInput) (DayPassView, bool, error) {
	var issues []domain.Issue
	if !receipts.Methods[in.Method] {
		issues = append(issues, domain.Issue{Field: "method", Code: "METHOD_INVALID"})
	}
	visitor := Visitor{}
	if in.Visitor != nil {
		visitor = *in.Visitor
		issues = append(issues, visitorIssues(visitor)...)
	}
	if len(issues) > 0 {
		return DayPassView{}, false, &domain.ValidationError{Issues: issues}
	}
	branch, err := s.branchFor(ctx, p, in.BranchID)
	if err != nil {
		return DayPassView{}, false, err
	}
	if in.Amount != "" && !sameMoney(in.Amount, branch.DayPassPrice) {
		return DayPassView{}, false, &domain.ValidationError{Issues: []domain.Issue{{Field: "amount", Code: "AMOUNT_NOT_ALLOWED"}}}
	}
	id := in.ID
	if id == uuid.Nil {
		id = ids.New()
	}
	existed, err := s.Repo.SellDayPass(ctx, DayPassSale{ID: id, OrganizationID: p.OrganizationID, ActorID: p.UserID, BranchID: branch.ID,
		Amount: branch.DayPassPrice, Method: in.Method, Visitor: visitor, Today: s.today()})
	if err != nil {
		return DayPassView{}, false, err
	}
	view, err := s.Repo.DayPass(ctx, p.OrganizationID, id)
	return view, existed, err
}

// DayPass returns one sold day pass.
func (s *Service) DayPass(ctx context.Context, p auth.Principal, id uuid.UUID) (DayPassView, error) {
	return s.Repo.DayPass(ctx, p.OrganizationID, id)
}

// DayPassQuery is the parsed query of GET /day-passes.
type DayPassQuery struct {
	BranchID *uuid.UUID
	From, To *time.Time
	Page     int
	PageSize int
}

// DayPasses lists the day passes of the branches the caller reaches.
func (s *Service) DayPasses(ctx context.Context, p auth.Principal, q DayPassQuery) ([]DayPassView, int, error) {
	branches, err := s.reach(ctx, p, q.BranchID)
	if err != nil {
		return nil, 0, err
	}
	return s.Repo.DayPasses(ctx, p.OrganizationID, DayPassFilter{BranchIDs: branches, From: q.From, To: q.To, Page: q.Page, PageSize: q.PageSize})
}
