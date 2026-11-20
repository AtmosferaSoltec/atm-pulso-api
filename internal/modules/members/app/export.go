package app

import (
	"context"
	"errors"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// MaxExportRows is the largest member list the downloads produce.
const MaxExportRows = 10_000

// ErrExportTooLarge: the filter matches more than MaxExportRows members.
var ErrExportTooLarge = errors.New("export too large")

// MemberExport is the filtered directory for a download, with the counts per status.
type MemberExport struct {
	Items  []MemberView
	Counts Counts
	// HomeBranchName is the branch filtered by; empty for the whole gym.
	HomeBranchName string
}

// Export returns every member of the filter (no pagination). It follows the rules of the list: the directory
// is the gym's, so admin and reception may download what they may list.
func (s *Service) Export(ctx context.Context, p auth.Principal, f Filter) (MemberExport, error) {
	f.Page, f.PageSize = 1, MaxExportRows+1
	result, err := s.Repo.List(ctx, p.GymID, f, s.today())
	if err != nil {
		return MemberExport{}, err
	}
	if len(result.Items) > MaxExportRows {
		return MemberExport{}, ErrExportTooLarge
	}
	out := MemberExport{Items: result.Items, Counts: result.Counts}
	if f.HomeBranchID != nil {
		branch, err := s.Directory.Branch(ctx, p.GymID, *f.HomeBranchID)
		if err != nil {
			return MemberExport{}, err
		}
		out.HomeBranchName = branch.Name
	}
	return out, nil
}
