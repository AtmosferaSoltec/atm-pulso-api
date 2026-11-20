package app

import (
	"context"
	"time"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// PaymentExport is everything a payments download shows: the rows, their totals and the filter as applied.
type PaymentExport struct {
	Rows   []PaymentRow
	Totals Totals
	// From and To are the effective range (defaults applied).
	From, To time.Time
	// BranchName is the branch filtered by; empty for every branch the caller reaches.
	BranchName string
}

// ExportPayments returns every row of the filter with its totals, for the downloads (admin only, at most
// MaxExportRows rows).
func (s *Service) ExportPayments(ctx context.Context, principal auth.Principal, q PaymentQuery) (PaymentExport, error) {
	rows, err := s.Export(ctx, principal, q)
	if err != nil {
		return PaymentExport{}, err
	}
	f, err := s.filter(ctx, principal, q)
	if err != nil {
		return PaymentExport{}, err
	}
	totals, err := s.Repo.Totals(ctx, principal.GymID, f)
	if err != nil {
		return PaymentExport{}, err
	}
	out := PaymentExport{Rows: rows, Totals: totals, From: f.From, To: f.To}
	if q.BranchID != nil {
		branch, err := s.Directory.Branch(ctx, principal.GymID, *q.BranchID)
		if err != nil {
			return PaymentExport{}, err
		}
		out.BranchName = branch.Name
	}
	return out, nil
}
