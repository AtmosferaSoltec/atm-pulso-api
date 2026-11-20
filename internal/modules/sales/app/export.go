package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// MaxExportRows is the largest sales list the downloads produce.
const MaxExportRows = 10_000

// ErrExportTooLarge: the period has more than MaxExportRows sales.
var ErrExportTooLarge = errors.New("export too large")

// SalesExport is the sales of a period for a download: every sale with its items and the totals of the report.
type SalesExport struct {
	Sales  []SaleView
	Totals ReportTotals
	// From and To are the effective range (defaults applied).
	From, To time.Time
	// BranchName is the branch filtered by; empty for the whole gym.
	BranchName string
}

// ExportSales lists the sales of the report's filter (period and branch), newest first, with the report's
// totals. Admin only, like the report.
func (s *Service) ExportSales(ctx context.Context, p auth.Principal, f ReportFilter) (SalesExport, error) {
	report, err := s.Report(ctx, p, f)
	if err != nil {
		return SalesExport{}, err
	}
	from, _ := time.Parse("2006-01-02", report.From)
	to, _ := time.Parse("2006-01-02", report.To)
	out := SalesExport{Totals: report.Totals, From: from, To: to}
	filter := SaleFilter{From: &from, To: &to, Page: 1, PageSize: MaxExportRows + 1}
	if f.BranchID != nil {
		branch, err := s.Directory.Branch(ctx, p.GymID, *f.BranchID)
		if err != nil {
			return SalesExport{}, err
		}
		out.BranchName = branch.Name
		filter.BranchIDs = []uuid.UUID{*f.BranchID}
	}
	recs, total, err := s.Repo.Sales(ctx, p.GymID, filter)
	if err != nil {
		return SalesExport{}, err
	}
	if total > MaxExportRows {
		return SalesExport{}, ErrExportTooLarge
	}
	if out.Sales, err = s.views(ctx, p.GymID, recs); err != nil {
		return SalesExport{}, err
	}
	return out, nil
}
