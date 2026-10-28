// Package app holds the read-only use cases of reports: the payments list with its totals,
// the CSV export and the dashboard. The module owns no tables.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/branchdir"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/receipts"
)

// MaxExportRows is the largest CSV the API produces.
const MaxExportRows = 10_000

// MaxRangeDays is the widest date range a report accepts.
const MaxRangeDays = 366

var (
	// ErrForbidden: the role cannot read this report.
	ErrForbidden = errors.New("forbidden")
	// ErrBranchNotAccessible: the user cannot reach that branch or gym.
	ErrBranchNotAccessible = errors.New("branch not accessible")
	// ErrExportTooLarge: more rows than MaxExportRows.
	ErrExportTooLarge = errors.New("export too large")
	// ErrRange: from is after to, or the range is wider than MaxRangeDays.
	ErrRange = errors.New("invalid range")
)

// Directory reads the branches of the organization (platform/branchdir implements it).
type Directory interface {
	Branch(ctx context.Context, organizationID, branchID uuid.UUID) (branchdir.Info, error)
	Branches(ctx context.Context, organizationID uuid.UUID) ([]branchdir.Info, error)
}

// Service implements the use cases.
type Service struct {
	Repo      Repository
	Directory Directory
	Clock     clock.Clock
}

// PaymentFilter is what the repository filters by; dates are Lima dates, inclusive.
type PaymentFilter struct {
	BranchIDs []uuid.UUID
	From, To  time.Time
	Method    string
	Kind      string
	Query     string
	Limit     int
	Offset    int
}

// PaymentRow is one payment with its receipt.
type PaymentRow struct {
	ID          uuid.UUID  `json:"id"`
	PaidOn      string     `json:"paid_on"`
	Receipt     ReceiptRef `json:"receipt"`
	Kind        string     `json:"kind"`
	PlanLabel   string     `json:"plan_label"`
	Amount      string     `json:"amount"`
	Method      string     `json:"method"`
	Member      *MemberRef `json:"member"`
	VisitorName *string    `json:"visitor_name"`
	Branch      BranchRef  `json:"branch"`
	// VoucherCount is how many proofs of payment (photos, PDFs) are attached.
	VoucherCount int     `json:"voucher_count"`
	DocumentType *string `json:"-"`
	Document     *string `json:"-"`
}

// ReceiptRef points to a receipt.
type ReceiptRef struct {
	ID     uuid.UUID `json:"id"`
	Number string    `json:"number"`
}

// MemberRef identifies the member of a payment.
type MemberRef struct {
	ID       uuid.UUID `json:"id"`
	FullName string    `json:"full_name"`
}

// BranchRef identifies a branch.
type BranchRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Totals are the KPIs of a filtered set.
type Totals struct {
	Amount    string            `json:"amount"`
	Count     int               `json:"count"`
	DayPasses int               `json:"day_passes"`
	Average   *string           `json:"average"`
	TopMethod *string           `json:"top_method"`
	ByMethod  map[string]string `json:"by_method"`
}

// DashboardView is the home screen of a gym.
type DashboardView struct {
	Date         string         `json:"date"`
	Members      MemberCounts   `json:"members"`
	Income       Income         `json:"income"`
	Access       AccessCounts   `json:"access"`
	ExpiringSoon []ExpiringItem `json:"expiring_soon"`
}

// MemberCounts are the members per status.
type MemberCounts struct {
	Active    int `json:"active"`
	Expiring  int `json:"expiring"`
	Expired   int `json:"expired"`
	Scheduled int `json:"scheduled"`
}

// Income is what was collected.
type Income struct {
	Today string `json:"today"`
	Month string `json:"month"`
}

// AccessCounts are today's checks.
type AccessCounts struct {
	AllowedToday int `json:"allowed_today"`
	DeniedToday  int `json:"denied_today"`
}

// ExpiringItem is a member about to expire.
type ExpiringItem struct {
	MemberID      uuid.UUID `json:"member_id"`
	FullName      string    `json:"full_name"`
	EndsOn        string    `json:"ends_on"`
	DaysRemaining int       `json:"days_remaining"`
	Phone         string    `json:"phone"`
}

// Repository is the read port.
type Repository interface {
	Payments(ctx context.Context, organizationID uuid.UUID, f PaymentFilter) ([]PaymentRow, int, error)
	Totals(ctx context.Context, organizationID uuid.UUID, f PaymentFilter) (Totals, error)
	Dashboard(ctx context.Context, organizationID uuid.UUID, branchIDs []uuid.UUID, today, monthStart time.Time) (DashboardView, error)
}

// PaymentQuery is the parsed query of GET /payments.
type PaymentQuery struct {
	BranchID, GymID *uuid.UUID
	From, To        *time.Time
	Method, Kind    string
	Query           string
	Page, PageSize  int
}

func (s *Service) today() time.Time { return clock.LimaDate(s.Clock.Now()) }

// reach returns the branches the caller may read, narrowed by branch or gym.
func (s *Service) reach(ctx context.Context, p auth.Principal, branchID, gymID *uuid.UUID) ([]uuid.UUID, error) {
	all, err := s.Directory.Branches(ctx, p.OrganizationID)
	if err != nil {
		return nil, err
	}
	out := []uuid.UUID{}
	found := branchID == nil && gymID == nil
	for _, b := range all {
		if !p.CanAccessBranch(auth.BranchRef{ID: b.ID, GymID: b.GymID}) {
			continue
		}
		if branchID != nil && b.ID != *branchID {
			continue
		}
		if gymID != nil && b.GymID != *gymID {
			continue
		}
		out = append(out, b.ID)
		found = true
	}
	if !found {
		return nil, ErrBranchNotAccessible
	}
	return out, nil
}

func (s *Service) filter(ctx context.Context, p auth.Principal, q PaymentQuery) (PaymentFilter, error) {
	branches, err := s.reach(ctx, p, q.BranchID, q.GymID)
	if err != nil {
		return PaymentFilter{}, err
	}
	today := s.today()
	from, to := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC), today
	if q.From != nil {
		from = *q.From
		if q.To == nil {
			to = from
			if today.After(from) {
				to = today
			}
		}
	}
	if q.To != nil {
		to = *q.To
		if q.From == nil {
			from = time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)
		}
	}
	if to.Before(from) || to.Sub(from) > MaxRangeDays*24*time.Hour {
		return PaymentFilter{}, ErrRange
	}
	return PaymentFilter{BranchIDs: branches, From: from, To: to, Method: q.Method, Kind: q.Kind, Query: q.Query}, nil
}

// ValidMethod reports whether a method filter is accepted ("" means any).
func ValidMethod(m string) bool { return m == "" || receipts.Methods[m] }

// Payments returns a page. Totals are omitted for reception, who must not get KPIs.
func (s *Service) Payments(ctx context.Context, p auth.Principal, q PaymentQuery) ([]PaymentRow, int, *Totals, error) {
	f, err := s.filter(ctx, p, q)
	if err != nil {
		return nil, 0, nil, err
	}
	f.Limit, f.Offset = q.PageSize, (q.Page-1)*q.PageSize
	rows, total, err := s.Repo.Payments(ctx, p.OrganizationID, f)
	if err != nil {
		return nil, 0, nil, err
	}
	if p.Role == auth.RoleReceptionist {
		return rows, total, nil, nil
	}
	totals, err := s.Repo.Totals(ctx, p.OrganizationID, f)
	if err != nil {
		return nil, 0, nil, err
	}
	return rows, total, &totals, nil
}

// Export returns every row of the filter for the CSV (owner and admin only).
func (s *Service) Export(ctx context.Context, p auth.Principal, q PaymentQuery) ([]PaymentRow, error) {
	if p.Role == auth.RoleReceptionist {
		return nil, ErrForbidden
	}
	f, err := s.filter(ctx, p, q)
	if err != nil {
		return nil, err
	}
	f.Limit = MaxExportRows + 1
	rows, _, err := s.Repo.Payments(ctx, p.OrganizationID, f)
	if err != nil {
		return nil, err
	}
	if len(rows) > MaxExportRows {
		return nil, ErrExportTooLarge
	}
	return rows, nil
}

// Dashboard returns the figures of the caller's reach (owner and admin only).
func (s *Service) Dashboard(ctx context.Context, p auth.Principal, branchID, gymID *uuid.UUID) (DashboardView, error) {
	if p.Role == auth.RoleReceptionist {
		return DashboardView{}, ErrForbidden
	}
	branches, err := s.reach(ctx, p, branchID, gymID)
	if err != nil {
		return DashboardView{}, err
	}
	today := s.today()
	return s.Repo.Dashboard(ctx, p.OrganizationID, branches, today, time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC))
}
