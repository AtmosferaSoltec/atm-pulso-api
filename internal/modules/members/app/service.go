package app

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/receipts"
)

// Service implements the use cases.
type Service struct {
	Repo      Repository
	Directory Directory
	// Logos may be nil (development without storage): the logo is then null.
	Logos LogoResolver
	Clock clock.Clock
}

// LogoResolver gives the temporary address of a gym's logo (see the accounts module); nil when none or
// unavailable. The tenant always comes from the principal.
type LogoResolver interface {
	LogoURL(ctx context.Context, tenantID uuid.UUID) *string
}

// EnrollInput is the body of POST /members.
type EnrollInput struct {
	ID             uuid.UUID `json:"id"`
	BranchID       uuid.UUID `json:"branch_id"`
	DocumentType   string    `json:"document_type"`
	DocumentNumber string    `json:"document_number"`
	FullName       string    `json:"full_name"`
	Phone          string    `json:"phone"`
	// PlanID is optional when the gym has exactly one active plan.
	PlanID          *uuid.UUID  `json:"plan_id"`
	Months          int         `json:"months"`
	StartsOn        string      `json:"starts_on"`
	Amount          string      `json:"amount"`
	Method          string      `json:"method"`
	AccessScope     string      `json:"access_scope"`
	AccessBranchIDs []uuid.UUID `json:"access_branch_ids"`
}

// RenewInput is the body of POST /members/{id}/renewals.
type RenewInput struct {
	ID       uuid.UUID `json:"id"`
	BranchID uuid.UUID `json:"branch_id"`
	// PlanID is optional when the gym has exactly one active plan. A renewal continues the plan chosen.
	PlanID          *uuid.UUID  `json:"plan_id"`
	Months          int         `json:"months"`
	StartsOn        string      `json:"starts_on"`
	Amount          string      `json:"amount"`
	Method          string      `json:"method"`
	AccessScope     string      `json:"access_scope"`
	AccessBranchIDs []uuid.UUID `json:"access_branch_ids"`
}

var amountPattern = regexp.MustCompile(`^[0-9]{1,7}(\.[0-9]{1,2})?$`)

// normalizeAmount accepts "270" or "270.5" and returns "270.00"; false when out of range.
func normalizeAmount(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if !amountPattern.MatchString(raw) {
		return "", false
	}
	whole, frac, _ := strings.Cut(raw, ".")
	frac = (frac + "00")[:2]
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	if len(whole) == 7 && (whole > "1000000" || (whole == "1000000" && frac != "00")) {
		return "", false
	}
	return whole + "." + frac, true
}

func (s *Service) today() time.Time { return clock.LimaDate(s.Clock.Now()) }

// branchFor resolves a branch of the caller's gym the caller may work in.
func (s *Service) branchFor(ctx context.Context, p auth.Principal, branchID uuid.UUID) (BranchInfo, error) {
	branch, err := s.Directory.Branch(ctx, p.GymID, branchID)
	if err != nil {
		return BranchInfo{}, err
	}
	if !p.CanAccessBranch(auth.BranchRef{ID: branch.ID, GymID: branch.GymID}) {
		return BranchInfo{}, domain.ErrBranchNotAccessible
	}
	return branch, nil
}

// List is the shared directory of the gym.
func (s *Service) List(ctx context.Context, p auth.Principal, f Filter) (ListResult, error) {
	return s.Repo.List(ctx, p.GymID, f, s.today())
}

// Get returns a member with every period.
func (s *Service) Get(ctx context.Context, p auth.Principal, id uuid.UUID) (MemberDetail, error) {
	return s.Repo.Member(ctx, p.GymID, id, s.today())
}

// Update changes name and phone only.
func (s *Service) Update(ctx context.Context, p auth.Principal, id uuid.UUID, name, phone *string) (MemberDetail, error) {
	current, err := s.Repo.Member(ctx, p.GymID, id, s.today())
	if err != nil {
		return MemberDetail{}, err
	}
	newName, newPhone := current.FullName, current.Phone
	var issues []domain.Issue
	if name != nil {
		if normalized, ok := domain.NormalizeName(*name); ok {
			newName = normalized
		} else {
			issues = append(issues, domain.Issue{Field: "full_name", Code: domain.CodeFullName})
		}
	}
	if phone != nil {
		if domain.ValidPhone(*phone) {
			newPhone = *phone
		} else {
			issues = append(issues, domain.Issue{Field: "phone", Code: domain.CodePhone})
		}
	}
	if len(issues) > 0 {
		return MemberDetail{}, &domain.ValidationError{Issues: issues}
	}
	if err := s.Repo.UpdateMember(ctx, p.GymID, id, newName, newPhone); err != nil {
		return MemberDetail{}, err
	}
	return s.Repo.Member(ctx, p.GymID, id, s.today())
}

// parseStart returns the start date, today when empty.
func (s *Service) parseStart(raw string) (time.Time, bool, bool) {
	if raw == "" {
		return time.Time{}, false, true
	}
	t, err := time.Parse("2006-01-02", raw)
	return t, true, err == nil
}

func sellIssues(months int, amount, method, scope string) (string, []domain.Issue) {
	var issues []domain.Issue
	if months < 1 || months > 24 {
		issues = append(issues, domain.Issue{Field: "months", Code: domain.CodeMonths})
	}
	normalized, ok := normalizeAmount(amount)
	if !ok {
		issues = append(issues, domain.Issue{Field: "amount", Code: domain.CodeAmount})
	}
	if !receipts.Methods[method] {
		issues = append(issues, domain.Issue{Field: "method", Code: domain.CodeMethod})
	}
	if scope != "" && !domain.Scope(scope).Valid() {
		issues = append(issues, domain.Issue{Field: "access_scope", Code: domain.CodeAccessScope})
	}
	return normalized, issues
}

// Enroll creates the member, the first period, the payment and the receipt atomically.
func (s *Service) Enroll(ctx context.Context, p auth.Principal, in EnrollInput) (Sale, bool, error) {
	var issues []domain.Issue
	name, nameOK := domain.NormalizeName(in.FullName)
	if !nameOK {
		issues = append(issues, domain.Issue{Field: "full_name", Code: domain.CodeFullName})
	}
	if in.DocumentType != "dni" && in.DocumentType != "ce" {
		issues = append(issues, domain.Issue{Field: "document_type", Code: domain.CodeDocumentType})
	} else if !domain.ValidDocument(in.DocumentType, in.DocumentNumber) {
		issues = append(issues, domain.Issue{Field: "document_number", Code: domain.CodeDocumentNumber})
	}
	if !domain.ValidPhone(in.Phone) {
		issues = append(issues, domain.Issue{Field: "phone", Code: domain.CodePhone})
	}
	amount, sellProblems := sellIssues(in.Months, in.Amount, in.Method, in.AccessScope)
	issues = append(issues, sellProblems...)
	start, given, startOK := s.parseStart(in.StartsOn)
	if !startOK {
		issues = append(issues, domain.Issue{Field: "starts_on", Code: domain.CodeStartDate})
	}
	if len(issues) > 0 {
		return Sale{}, false, &domain.ValidationError{Issues: issues}
	}
	if !given {
		start = s.today()
	}
	home, err := s.branchFor(ctx, p, in.BranchID)
	if err != nil {
		return Sale{}, false, err
	}
	scope := domain.Scope(in.AccessScope)
	if scope == "" {
		scope = domain.ScopeBranch
	}
	all, err := s.Directory.Branches(ctx, p.GymID)
	if err != nil {
		return Sale{}, false, err
	}
	branchIDs, err := domain.AccessBranches(scope, domain.Branch{ID: home.ID, GymID: home.GymID}, toDomain(all), in.AccessBranchIDs)
	if err != nil {
		return Sale{}, false, err
	}
	memberID := in.ID
	if memberID == uuid.Nil {
		memberID = ids.New()
	}
	saleIDs, existed, err := s.Repo.Enroll(ctx, Enrollment{
		GymID: p.GymID, ActorID: p.UserID, MemberID: memberID, HomeBranchID: home.ID,
		DocumentType: in.DocumentType, DocumentNumber: in.DocumentNumber, FullName: name, Phone: in.Phone,
		Period: Period{ID: ids.New(), Months: in.Months, StartsOn: start, EndsOn: domain.EndDate(start, in.Months),
			Amount: amount, Scope: scope, BranchIDs: branchIDs},
		PlanID: in.PlanID, Method: in.Method, Today: s.today(),
	})
	if err != nil {
		return Sale{}, false, err
	}
	sale, err := s.sale(ctx, p.GymID, saleIDs)
	return sale, existed, err
}

// Renew adds a period to an existing member.
func (s *Service) Renew(ctx context.Context, p auth.Principal, memberID uuid.UUID, in RenewInput) (Sale, bool, error) {
	amount, issues := sellIssues(in.Months, in.Amount, in.Method, in.AccessScope)
	start, given, startOK := s.parseStart(in.StartsOn)
	if !startOK {
		issues = append(issues, domain.Issue{Field: "starts_on", Code: domain.CodeStartDate})
	}
	if len(issues) > 0 {
		return Sale{}, false, &domain.ValidationError{Issues: issues}
	}
	collected, err := s.branchFor(ctx, p, in.BranchID)
	if err != nil {
		return Sale{}, false, err
	}
	all, err := s.Directory.Branches(ctx, p.GymID)
	if err != nil {
		return Sale{}, false, err
	}
	today := s.today()
	plan := func(home uuid.UUID, latestEnd *time.Time) (Period, error) {
		starts := start
		if !given {
			starts = today
			if latestEnd != nil && latestEnd.After(today) {
				starts = *latestEnd
			}
		}
		var homeBranch domain.Branch
		for _, b := range all {
			if b.ID == home {
				homeBranch = domain.Branch{ID: b.ID, GymID: b.GymID}
			}
		}
		scope := domain.Scope(in.AccessScope)
		if scope == "" {
			scope = domain.ScopeBranch
		}
		branchIDs, err := domain.AccessBranches(scope, homeBranch, toDomain(all), in.AccessBranchIDs)
		if err != nil {
			return Period{}, err
		}
		periodID := in.ID
		if periodID == uuid.Nil {
			periodID = ids.New()
		}
		return Period{ID: periodID, Months: in.Months, StartsOn: starts, EndsOn: domain.EndDate(starts, in.Months),
			Amount: amount, Scope: scope, BranchIDs: branchIDs}, nil
	}
	saleIDs, existed, err := s.Repo.Renew(ctx, Renewal{
		GymID: p.GymID, ActorID: p.UserID, MemberID: memberID, PeriodID: in.ID,
		CollectedBranchID: collected.ID, PlanID: in.PlanID, Method: in.Method, Today: today,
	}, plan)
	if err != nil {
		return Sale{}, false, err
	}
	sale, err := s.sale(ctx, p.GymID, saleIDs)
	return sale, existed, err
}

func (s *Service) sale(ctx context.Context, gymID uuid.UUID, saleIDs SaleIDs) (Sale, error) {
	detail, err := s.Repo.Member(ctx, gymID, saleIDs.MemberID, s.today())
	if err != nil {
		return Sale{}, err
	}
	for _, m := range detail.Memberships {
		if m.ID == saleIDs.MembershipID && m.Receipt != nil {
			return Sale{Member: detail.MemberView, Membership: m, Receipt: *m.Receipt}, nil
		}
	}
	return Sale{}, errors.New("sale not found after saving")
}

// Receipts lists a member's receipts.
func (s *Service) Receipts(ctx context.Context, p auth.Principal, memberID uuid.UUID) ([]ReceiptSummary, error) {
	if _, err := s.Repo.Member(ctx, p.GymID, memberID, s.today()); err != nil {
		return nil, err
	}
	return s.Repo.MemberReceipts(ctx, p.GymID, memberID)
}

// Receipt returns the printable data of a receipt.
func (s *Service) Receipt(ctx context.Context, p auth.Principal, id uuid.UUID) (ReceiptDetail, error) {
	receipt, err := s.Repo.Receipt(ctx, p.GymID, id)
	if err != nil {
		return receipt, err
	}
	if s.Logos != nil {
		receipt.Gym.LogoURL = s.Logos.LogoURL(ctx, p.TenantID)
	}
	return receipt, nil
}

func toDomain(branches []BranchInfo) []domain.Branch {
	out := make([]domain.Branch, len(branches))
	for i, b := range branches {
		out[i] = domain.Branch{ID: b.ID, GymID: b.GymID}
	}
	return out
}

// AmendMembership corrects a period that has not ended: months, start date, amount and payment method. The end date is
// recalculated here from start and months; the period cannot overlap another of the same plan (domain.OverlapError); the
// payment and the receipt are corrected in place (same number) and the change is recorded with the author (see
// business-rules.md section 7c). Both roles may do it, in a branch they can reach (the branch that collected the payment).
func (s *Service) AmendMembership(ctx context.Context, p auth.Principal, memberID, membershipID uuid.UUID, in AmendInput) (Sale, error) {
	var issues []domain.Issue
	if in.Months == nil && in.StartsOn == nil && in.Amount == nil && in.Method == nil {
		issues = append(issues, domain.Issue{Field: "body", Code: domain.CodeNoChanges})
	}
	if in.Months != nil && (*in.Months < 1 || *in.Months > 24) {
		issues = append(issues, domain.Issue{Field: "months", Code: domain.CodeMonths})
	}
	var newAmount string
	if in.Amount != nil {
		normalized, ok := normalizeAmount(*in.Amount)
		if !ok {
			issues = append(issues, domain.Issue{Field: "amount", Code: domain.CodeAmount})
		}
		newAmount = normalized
	}
	if in.Method != nil && !receipts.Methods[*in.Method] {
		issues = append(issues, domain.Issue{Field: "method", Code: domain.CodeMethod})
	}
	var newStart *time.Time
	if in.StartsOn != nil {
		t, err := time.Parse("2006-01-02", *in.StartsOn)
		if err != nil {
			issues = append(issues, domain.Issue{Field: "starts_on", Code: domain.CodeStartDate})
		}
		newStart = &t
	}
	if len(issues) > 0 {
		return Sale{}, &domain.ValidationError{Issues: issues}
	}
	today := s.today()
	_, err := s.Repo.AmendMembership(ctx, p.GymID, p.UserID, memberID, membershipID, func(cur CurrentPeriod) (PeriodChange, error) {
		if domain.Ended(domain.Period{StartsOn: cur.StartsOn, EndsOn: cur.EndsOn}, today) {
			return PeriodChange{}, domain.ErrMembershipEnded
		}
		if cur.PaymentBranchID != nil {
			branch, err := s.Directory.Branch(ctx, p.GymID, *cur.PaymentBranchID)
			if err != nil {
				return PeriodChange{}, err
			}
			if !p.CanAccessBranch(auth.BranchRef{ID: branch.ID, GymID: branch.GymID}) {
				return PeriodChange{}, domain.ErrBranchNotAccessible
			}
		}
		change := PeriodChange{Months: cur.Months, StartsOn: cur.StartsOn, Amount: cur.Amount, Method: cur.Method}
		if in.Months != nil {
			change.Months = *in.Months
		}
		if newStart != nil {
			change.StartsOn = *newStart
		}
		if in.Amount != nil {
			change.Amount = newAmount
		}
		if in.Method != nil {
			change.Method = *in.Method
		}
		change.EndsOn = domain.EndDate(change.StartsOn, change.Months)
		if change.EndsOn.Before(today) {
			return PeriodChange{}, &domain.ValidationError{Issues: []domain.Issue{{Field: "starts_on", Code: domain.CodePeriodEnded}}}
		}
		return change, nil
	})
	if err != nil {
		return Sale{}, err
	}
	return s.sale(ctx, p.GymID, SaleIDs{MemberID: memberID, MembershipID: membershipID})
}

// Amendments is the history of corrections of a period (admin only).
func (s *Service) Amendments(ctx context.Context, p auth.Principal, memberID, membershipID uuid.UUID) ([]AmendmentView, error) {
	if p.Role != auth.RoleAdmin {
		return nil, domain.ErrForbidden
	}
	return s.Repo.Amendments(ctx, p.GymID, memberID, membershipID)
}
