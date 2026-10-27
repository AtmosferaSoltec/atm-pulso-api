// Package postgres is the persistence adapter of members. It reads the branches and gyms
// owned by the accounts module with plain SELECTs (modules never import each other).
package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members/domain"
)

// Directory reads branches of an organization.
type Directory struct{ pool *pgxpool.Pool }

// NewDirectory builds the branch reader.
func NewDirectory(pool *pgxpool.Pool) *Directory { return &Directory{pool: pool} }

const branchSelect = `SELECT b.id, b.gym_id, b.name, COALESCE(g.display_name, g.name), g.branch_sharing
	FROM branches b JOIN gyms g ON g.id = b.gym_id AND g.organization_id = b.organization_id
	WHERE b.organization_id = $1`

func scanBranch(row pgx.Row) (app.BranchInfo, error) {
	var b app.BranchInfo
	err := row.Scan(&b.ID, &b.GymID, &b.Name, &b.GymName, &b.BranchSharing)
	return b, err
}

// Branch returns one branch of the organization.
func (d *Directory) Branch(ctx context.Context, organizationID, branchID uuid.UUID) (app.BranchInfo, error) {
	b, err := scanBranch(d.pool.QueryRow(ctx, branchSelect+` AND b.id = $2`, organizationID, branchID))
	if errors.Is(err, pgx.ErrNoRows) {
		return app.BranchInfo{}, domain.ErrBranchNotFound
	}
	return b, err
}

// Branches returns every branch of the organization.
func (d *Directory) Branches(ctx context.Context, organizationID uuid.UUID) ([]app.BranchInfo, error) {
	rows, err := d.pool.Query(ctx, branchSelect+` ORDER BY g.name, b.name, b.id`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []app.BranchInfo
	for rows.Next() {
		b, err := scanBranch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
