package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// OrganizationExists supports validated superadmin inspection.
func (r *Repository) OrganizationExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM organizations WHERE id=$1)`, id).Scan(&exists)
	return exists, err
}

// BranchInfo is a narrow structural port for other modules; every lookup is tenant scoped.
func (r *Repository) BranchInfo(ctx context.Context, organizationID, branchID uuid.UUID) (auth.BranchRef, bool, bool, error) {
	var branch auth.BranchRef
	var sharing bool
	err := r.pool.QueryRow(ctx, `SELECT b.id,b.gym_id,g.branch_sharing FROM branches b
 JOIN gyms g ON g.id=b.gym_id AND g.organization_id=b.organization_id
 WHERE b.id=$1 AND b.organization_id=$2`, branchID, organizationID).Scan(&branch.ID, &branch.GymID, &sharing)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.BranchRef{}, false, false, nil
	}
	return branch, sharing, err == nil, err
}
