package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/sales/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/peopledir"
)

// Service implements the use cases. The module switch (FEATURE_DISABLED) is a route guard, not here.
type Service struct {
	Repo      Repository
	Directory Directory
	People    People
	Clock     clock.Clock
}

func (s *Service) today() time.Time { return clock.LimaDate(s.Clock.Now()) }

func requireAdmin(p auth.Principal) error {
	if p.Role != auth.RoleAdmin {
		return domain.ErrForbidden
	}
	return nil
}

func invalid(field, code string) error {
	return &domain.ValidationError{Issues: []domain.Issue{{Field: field, Code: code}}}
}

// ---- Catalog ----

// Products lists the catalog in the order the gym wants. Reception always gets the active ones (what can be sold);
// an admin may ask for all of them.
func (s *Service) Products(ctx context.Context, p auth.Principal, includeInactive bool) ([]ProductView, error) {
	return s.Repo.Products(ctx, p.GymID, includeInactive && p.Role == auth.RoleAdmin)
}

// CreateProduct adds a product (admin).
func (s *Service) CreateProduct(ctx context.Context, p auth.Principal, in ProductInput) (ProductView, bool, error) {
	if err := requireAdmin(p); err != nil {
		return ProductView{}, false, err
	}
	var issues []domain.Issue
	name, ok := domain.ProductName(in.Name)
	if !ok {
		issues = append(issues, domain.Issue{Field: "name", Code: domain.CodeName})
	}
	cents, ok := domain.ProductPrice(in.Price)
	if !ok {
		issues = append(issues, domain.Issue{Field: "price", Code: domain.CodePrice})
	}
	position := -1
	if in.Position != nil {
		position = *in.Position
		if position < 0 || position > 10_000 {
			issues = append(issues, domain.Issue{Field: "position", Code: domain.CodePosition})
		}
	}
	if len(issues) > 0 {
		return ProductView{}, false, &domain.ValidationError{Issues: issues}
	}
	id := in.ID
	if id == uuid.Nil {
		id = ids.New()
	}
	active := in.IsActive == nil || *in.IsActive
	return s.Repo.CreateProduct(ctx, p.GymID, NewProduct{ID: id, Name: name, Price: domain.FormatMoney(cents), IsActive: active, Position: position})
}

// UpdateProduct changes the fields sent (admin). Products are never deleted, only deactivated: old sales keep their photo.
func (s *Service) UpdateProduct(ctx context.Context, p auth.Principal, id uuid.UUID, in ProductChanges) (ProductView, error) {
	if err := requireAdmin(p); err != nil {
		return ProductView{}, err
	}
	var issues []domain.Issue
	patch := ProductPatch{IsActive: in.IsActive}
	if in.Name != nil {
		name, ok := domain.ProductName(*in.Name)
		if !ok {
			issues = append(issues, domain.Issue{Field: "name", Code: domain.CodeName})
		}
		patch.Name = &name
	}
	if in.Price != nil {
		cents, ok := domain.ProductPrice(*in.Price)
		if !ok {
			issues = append(issues, domain.Issue{Field: "price", Code: domain.CodePrice})
		}
		price := domain.FormatMoney(cents)
		patch.Price = &price
	}
	if in.Position != nil {
		if *in.Position < 0 || *in.Position > 10_000 {
			issues = append(issues, domain.Issue{Field: "position", Code: domain.CodePosition})
		}
		patch.Position = in.Position
	}
	if len(issues) > 0 {
		return ProductView{}, &domain.ValidationError{Issues: issues}
	}
	return s.Repo.UpdateProduct(ctx, p.GymID, id, patch)
}

// ---- Sales ----

func (s *Service) accessibleBranch(ctx context.Context, p auth.Principal, branchID uuid.UUID) (BranchInfo, error) {
	branch, err := s.Directory.Branch(ctx, p.GymID, branchID)
	if err != nil {
		return BranchInfo{}, err
	}
	if !p.CanAccessBranch(auth.BranchRef{ID: branch.ID, GymID: branch.GymID}) {
		return BranchInfo{}, domain.ErrBranchNotAccessible
	}
	return branch, nil
}

// Sell registers a sale of one or several products, paid now or charged to a member (pending sale). Idempotent by id.
func (s *Service) Sell(ctx context.Context, p auth.Principal, in SaleInput) (SaleView, bool, error) {
	var issues []domain.Issue
	status := in.Status
	if status == "" {
		status = domain.StatusPaid
	}
	method := ""
	switch status {
	case domain.StatusPaid:
		method = in.Method
		if !domain.Methods[method] {
			issues = append(issues, domain.Issue{Field: "method", Code: domain.CodeMethod})
		}
	case domain.StatusPending:
		if in.MemberID == nil {
			issues = append(issues, domain.Issue{Field: "member_id", Code: domain.CodeMemberRequired})
		}
	default:
		issues = append(issues, domain.Issue{Field: "status", Code: domain.CodeStatus})
	}
	lines := make([]domain.Line, 0, len(in.Items))
	for _, item := range in.Items {
		lines = append(lines, domain.Line{ProductID: item.ProductID, Quantity: item.Quantity})
	}
	merged, ok := domain.MergeLines(lines)
	if !ok {
		issues = append(issues, domain.Issue{Field: "items", Code: domain.CodeItems})
	}
	if len(issues) > 0 {
		return SaleView{}, false, &domain.ValidationError{Issues: issues}
	}
	if _, err := s.accessibleBranch(ctx, p, in.BranchID); err != nil {
		return SaleView{}, false, err
	}
	if in.MemberID != nil {
		if _, err := s.People.Member(ctx, p.GymID, *in.MemberID); err != nil {
			if errors.Is(err, peopledir.ErrNotFound) {
				return SaleView{}, false, invalid("member_id", domain.CodeMember)
			}
			return SaleView{}, false, err
		}
	}
	id := in.ID
	if id == uuid.Nil {
		id = ids.New()
	}
	rec, existed, err := s.Repo.CreateSale(ctx, NewSale{GymID: p.GymID, ID: id, BranchID: in.BranchID, SoldBy: p.UserID,
		MemberID: in.MemberID, Status: status, Method: method, Lines: merged, Today: s.today()})
	if err != nil {
		return SaleView{}, false, err
	}
	views, err := s.views(ctx, p.GymID, []SaleRecord{rec})
	if err != nil {
		return SaleView{}, false, err
	}
	return views[0], existed, nil
}

// Sales lists sales, newest first. Reception sees only the branches it works in; the admin sees the whole gym.
func (s *Service) Sales(ctx context.Context, p auth.Principal, f SaleFilter, branchID *uuid.UUID) ([]SaleView, int, error) {
	if branchID != nil {
		if _, err := s.accessibleBranch(ctx, p, *branchID); err != nil {
			return nil, 0, err
		}
		f.BranchIDs = []uuid.UUID{*branchID}
	} else if p.Role != auth.RoleAdmin {
		f.BranchIDs = append([]uuid.UUID{}, p.BranchIDs...)
		if len(f.BranchIDs) == 0 {
			return []SaleView{}, 0, nil
		}
	}
	recs, total, err := s.Repo.Sales(ctx, p.GymID, f)
	if err != nil {
		return nil, 0, err
	}
	views, err := s.views(ctx, p.GymID, recs)
	return views, total, err
}

// ---- Pending (pending sales) ----

// PendingBalances lists the members who owe something, the biggest balance first. Balances are the gym's (the directory is
// shared between branches), so both roles see the same list.
func (s *Service) PendingBalances(ctx context.Context, p auth.Principal) ([]PendingBalance, error) {
	recs, err := s.Repo.PendingBalances(ctx, p.GymID)
	if err != nil {
		return nil, err
	}
	memberIDs := make([]uuid.UUID, 0, len(recs))
	for _, r := range recs {
		memberIDs = append(memberIDs, r.MemberID)
	}
	members, err := s.People.Members(ctx, p.GymID, memberIDs)
	if err != nil {
		return nil, err
	}
	out := make([]PendingBalance, 0, len(recs))
	for _, r := range recs {
		out = append(out, PendingBalance{Member: memberRef(members[r.MemberID], r.MemberID), Balance: r.Balance,
			PendingCount: r.PendingCount, OldestSoldOn: r.OldestSoldOn.Format("2006-01-02")})
	}
	return out, nil
}

// MemberPending is what one member owes and the pending sales behind it.
func (s *Service) MemberPending(ctx context.Context, p auth.Principal, memberID uuid.UUID) (MemberPending, error) {
	if _, err := s.People.Member(ctx, p.GymID, memberID); err != nil {
		if errors.Is(err, peopledir.ErrNotFound) {
			return MemberPending{}, domain.ErrMemberNotFound
		}
		return MemberPending{}, err
	}
	recs, err := s.Repo.MemberPending(ctx, p.GymID, memberID)
	if err != nil {
		return MemberPending{}, err
	}
	views, err := s.views(ctx, p.GymID, recs)
	if err != nil {
		return MemberPending{}, err
	}
	return MemberPending{MemberID: memberID, Balance: sumTotals(recs), PendingCount: len(recs), Sales: views}, nil
}

func sumTotals(recs []SaleRecord) string {
	var cents int64
	for _, r := range recs {
		c, _ := domain.ParseMoney(r.Total)
		cents += c
	}
	return domain.FormatMoney(cents)
}

// Collect ("Cobrar") records the payment of whole pending sales of one member: the ones listed, or all of them. The method and
// who collected it are kept. Partial payments of a single sale do not exist: the sale is paid or it is not.
func (s *Service) Collect(ctx context.Context, p auth.Principal, in CollectInput) (CollectionView, bool, error) {
	var issues []domain.Issue
	if !domain.Methods[in.Method] {
		issues = append(issues, domain.Issue{Field: "method", Code: domain.CodeMethod})
	}
	if in.MemberID == uuid.Nil {
		issues = append(issues, domain.Issue{Field: "member_id", Code: domain.CodeMember})
	}
	seen := map[uuid.UUID]bool{}
	var saleIDs []uuid.UUID
	for _, id := range in.SaleIDs {
		if id == uuid.Nil || len(in.SaleIDs) > 500 {
			issues = append(issues, domain.Issue{Field: "sale_ids", Code: domain.CodeSaleIDs})
			break
		}
		if !seen[id] {
			seen[id] = true
			saleIDs = append(saleIDs, id)
		}
	}
	if len(issues) > 0 {
		return CollectionView{}, false, &domain.ValidationError{Issues: issues}
	}
	branch, err := s.accessibleBranch(ctx, p, in.BranchID)
	if err != nil {
		return CollectionView{}, false, err
	}
	member, err := s.People.Member(ctx, p.GymID, in.MemberID)
	if err != nil {
		if errors.Is(err, peopledir.ErrNotFound) {
			return CollectionView{}, false, domain.ErrMemberNotFound
		}
		return CollectionView{}, false, err
	}
	id := in.ID
	if id == uuid.Nil {
		id = ids.New()
	}
	rec, existed, err := s.Repo.Collect(ctx, NewCollection{GymID: p.GymID, ID: id, MemberID: in.MemberID, BranchID: in.BranchID,
		By: p.UserID, Method: in.Method, SaleIDs: saleIDs, Today: s.today()})
	if err != nil {
		return CollectionView{}, false, err
	}
	by, err := s.People.Users(ctx, p.GymID, []uuid.UUID{rec.By})
	if err != nil {
		return CollectionView{}, false, err
	}
	return CollectionView{ID: rec.ID, Member: memberRef(member, member.ID), Branch: BranchRef{ID: branch.ID, Name: branch.Name},
		Method: rec.Method, Amount: rec.Amount, CollectedAt: rec.CollectedAt.UTC().Format(time.RFC3339),
		CollectedBy: UserRef{ID: rec.By, FullName: by[rec.By].FullName}, SaleIDs: rec.SaleIDs, RemainingBalance: rec.Remaining}, existed, nil
}

// ---- Report ----

// Report sums the sales of a period by product and by day or month (admin). It is SEPARATE from the membership income:
// nothing here is read by the payments report or the dashboard, and nothing of those is read here.
func (s *Service) Report(ctx context.Context, p auth.Principal, f ReportFilter) (Report, error) {
	if err := requireAdmin(p); err != nil {
		return Report{}, err
	}
	if f.GroupBy == "" {
		f.GroupBy = "day"
	}
	if f.GroupBy != "day" && f.GroupBy != "month" {
		return Report{}, invalid("group_by", domain.CodeGroupBy)
	}
	today := s.today()
	if f.To.IsZero() {
		f.To = today
	}
	if f.From.IsZero() {
		f.From = time.Date(f.To.Year(), f.To.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	if f.From.After(f.To) || f.To.Sub(f.From) > 800*24*time.Hour {
		return Report{}, invalid("from", domain.CodeRange)
	}
	if f.BranchID != nil {
		if _, err := s.Directory.Branch(ctx, p.GymID, *f.BranchID); err != nil {
			return Report{}, err
		}
	}
	return s.Repo.Report(ctx, p.GymID, f)
}

// ---- Views ----

func memberRef(person peopledir.Person, id uuid.UUID) MemberRef {
	return MemberRef{ID: id, FullName: person.FullName, DocumentNumber: person.DocumentNumber}
}

// views attaches the branch, staff and member names to stored sales.
func (s *Service) views(ctx context.Context, gymID uuid.UUID, recs []SaleRecord) ([]SaleView, error) {
	if len(recs) == 0 {
		return []SaleView{}, nil
	}
	branches, err := s.Directory.Branches(ctx, gymID)
	if err != nil {
		return nil, err
	}
	branchName := make(map[uuid.UUID]string, len(branches))
	for _, b := range branches {
		branchName[b.ID] = b.Name
	}
	var userIDs, memberIDs []uuid.UUID
	for _, r := range recs {
		userIDs = append(userIDs, r.SoldBy)
		if r.MemberID != nil {
			memberIDs = append(memberIDs, *r.MemberID)
		}
	}
	users, err := s.People.Users(ctx, gymID, userIDs)
	if err != nil {
		return nil, err
	}
	members, err := s.People.Members(ctx, gymID, memberIDs)
	if err != nil {
		return nil, err
	}
	out := make([]SaleView, 0, len(recs))
	for _, r := range recs {
		v := SaleView{ID: r.ID, Branch: BranchRef{ID: r.BranchID, Name: branchName[r.BranchID]}, Status: r.Status, WasPending: r.WasPending,
			Method: r.Method, Total: r.Total, Units: r.Units, SoldAt: r.SoldAt.UTC().Format(time.RFC3339), SoldOn: r.SoldOn.Format("2006-01-02"),
			SoldBy: UserRef{ID: r.SoldBy, FullName: users[r.SoldBy].FullName}, CollectionID: r.CollectionID, Items: r.Items}
		if r.MemberID != nil {
			ref := memberRef(members[*r.MemberID], *r.MemberID)
			v.Member = &ref
		}
		if r.CollectedAt != nil {
			at := r.CollectedAt.UTC().Format(time.RFC3339)
			v.CollectedAt = &at
		}
		if v.Items == nil {
			v.Items = []SaleItemView{}
		}
		out = append(out, v)
	}
	return out, nil
}

// MemberSales is the purchase history of a member (paid and pending), newest first, from every branch of the gym.
func (s *Service) MemberSales(ctx context.Context, p auth.Principal, memberID uuid.UUID, page, size int) ([]SaleView, int, error) {
	if _, err := s.People.Member(ctx, p.GymID, memberID); err != nil {
		if errors.Is(err, peopledir.ErrNotFound) {
			return nil, 0, domain.ErrMemberNotFound
		}
		return nil, 0, err
	}
	recs, total, err := s.Repo.Sales(ctx, p.GymID, SaleFilter{MemberID: &memberID, Page: page, PageSize: size})
	if err != nil {
		return nil, 0, err
	}
	views, err := s.views(ctx, p.GymID, recs)
	return views, total, err
}
