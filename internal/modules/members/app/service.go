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
	Clock     clock.Clock
}

// EnrollInput is the body of POST /members.
type EnrollInput struct {
	ID              uuid.UUID   `json:"id"`
	BranchID        uuid.UUID   `json:"branch_id"`
	DocumentType    string      `json:"document_type"`
	DocumentNumber  string      `json:"document_number"`
	FullName        string      `json:"full_name"`
	Phone           string      `json:"phone"`
	Months          int         `json:"months"`
	StartsOn        string      `json:"starts_on"`
	Amount          string      `json:"amount"`
	Method          string      `json:"method"`
	AccessScope     string      `json:"access_scope"`
	AccessBranchIDs []uuid.UUID `json:"access_branch_ids"`
}

// RenewInput is the body of POST /members/{id}/renewals.
type RenewInput struct {
	ID              uuid.UUID   `json:"id"`
	BranchID        uuid.UUID   `json:"branch_id"`
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

func requireOperator(p auth.Principal) error {
	if !p.CanOperate() {
		return domain.ErrForbidden
	}
	return nil
}

// branchFor resolves a branch of the caller's organization the caller may work in.
func (s *Service) branchFor(ctx context.Context, p auth.Principal, branchID uuid.UUID) (BranchInfo, error) {
	branch, err := s.Directory.Branch(ctx, p.OrganizationID, branchID)
	if err != nil {
		return BranchInfo{}, err
	}
	if !p.CanAccessBranch(auth.BranchRef{ID: branch.ID, GymID: branch.GymID}) {
		return BranchInfo{}, domain.ErrBranchNotAccessible
	}
	return branch, nil
}

// List is the shared directory of the organization.
func (s *Service) List(ctx context.Context, p auth.Principal, f Filter) (ListResult, error) {
	return s.Repo.List(ctx, p.OrganizationID, f, s.today())
}

// Get returns a member with every period.
func (s *Service) Get(ctx context.Context, p auth.Principal, id uuid.UUID) (MemberDetail, error) {
	return s.Repo.Member(ctx, p.OrganizationID, id, s.today())
}

// Update changes name and phone only.
func (s *Service) Update(ctx context.Context, p auth.Principal, id uuid.UUID, name, phone *string) (MemberDetail, error) {
	if err := requireOperator(p); err != nil {
		return MemberDetail{}, err
	}
	current, err := s.Repo.Member(ctx, p.OrganizationID, id, s.today())
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
	if err := s.Repo.UpdateMember(ctx, p.OrganizationID, id, newName, newPhone); err != nil {
		return MemberDetail{}, err
	}
	return s.Repo.Member(ctx, p.OrganizationID, id, s.today())
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
	if err := requireOperator(p); err != nil {
		return Sale{}, false, err
	}
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
	all, err := s.Directory.Branches(ctx, p.OrganizationID)
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
		OrganizationID: p.OrganizationID, ActorID: p.UserID, MemberID: memberID, HomeBranchID: home.ID,
		DocumentType: in.DocumentType, DocumentNumber: in.DocumentNumber, FullName: name, Phone: in.Phone,
		Period: Period{ID: ids.New(), Months: in.Months, StartsOn: start, EndsOn: domain.EndDate(start, in.Months),
			Amount: amount, Scope: scope, BranchIDs: branchIDs},
		Method: in.Method, Today: s.today(),
	})
	if err != nil {
		return Sale{}, false, err
	}
	sale, err := s.sale(ctx, p.OrganizationID, saleIDs)
	return sale, existed, err
}

// Renew adds a period to an existing member.
func (s *Service) Renew(ctx context.Context, p auth.Principal, memberID uuid.UUID, in RenewInput) (Sale, bool, error) {
	if err := requireOperator(p); err != nil {
		return Sale{}, false, err
	}
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
	all, err := s.Directory.Branches(ctx, p.OrganizationID)
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
		OrganizationID: p.OrganizationID, ActorID: p.UserID, MemberID: memberID, PeriodID: in.ID,
		CollectedBranchID: collected.ID, Method: in.Method, Today: today,
	}, plan)
	if err != nil {
		return Sale{}, false, err
	}
	sale, err := s.sale(ctx, p.OrganizationID, saleIDs)
	return sale, existed, err
}

func (s *Service) sale(ctx context.Context, org uuid.UUID, saleIDs SaleIDs) (Sale, error) {
	detail, err := s.Repo.Member(ctx, org, saleIDs.MemberID, s.today())
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
	if _, err := s.Repo.Member(ctx, p.OrganizationID, memberID, s.today()); err != nil {
		return nil, err
	}
	return s.Repo.MemberReceipts(ctx, p.OrganizationID, memberID)
}

// Receipt returns the printable data of a receipt.
func (s *Service) Receipt(ctx context.Context, p auth.Principal, id uuid.UUID) (ReceiptDetail, error) {
	return s.Repo.Receipt(ctx, p.OrganizationID, id)
}

func toDomain(branches []BranchInfo) []domain.Branch {
	out := make([]domain.Branch, len(branches))
	for i, b := range branches {
		out[i] = domain.Branch{ID: b.ID, GymID: b.GymID}
	}
	return out
}
