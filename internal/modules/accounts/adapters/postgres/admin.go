package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/tx"
)

// Clients lists every organization with its gyms and branches.
func (r *Repository) Clients(ctx context.Context) ([]app.AdminClient, error) {
	clients := []app.AdminClient{}
	byOrg := map[uuid.UUID]int{}
	rows, err := r.pool.Query(ctx, `SELECT id, auth_client_id, name FROM organizations ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		client := app.AdminClient{Gyms: []app.AdminGym{}}
		if err := rows.Scan(&client.ID, &client.AuthClientID, &client.Name); err != nil {
			rows.Close()
			return nil, err
		}
		byOrg[client.ID] = len(clients)
		clients = append(clients, client)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	gymIndex := map[uuid.UUID][2]int{}
	rows, err = r.pool.Query(ctx, `SELECT id, organization_id, auth_tenant_id, tax_id, name, brand_color FROM gyms ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		gym := app.AdminGym{Branches: []app.AdminBranch{}}
		var org uuid.UUID
		if err := rows.Scan(&gym.ID, &org, &gym.AuthTenantID, &gym.RUC, &gym.Name, &gym.BrandColor); err != nil {
			rows.Close()
			return nil, err
		}
		oi := byOrg[org]
		gymIndex[gym.ID] = [2]int{oi, len(clients[oi].Gyms)}
		clients[oi].Gyms = append(clients[oi].Gyms, gym)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.pool.Query(ctx, `SELECT id, gym_id, name, address FROM branches ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var branch app.AdminBranch
		var gymID uuid.UUID
		if err := rows.Scan(&branch.ID, &gymID, &branch.Name, &branch.Address); err != nil {
			return nil, err
		}
		at := gymIndex[gymID]
		clients[at[0]].Gyms[at[1]].Branches = append(clients[at[0]].Gyms[at[1]].Branches, branch)
	}
	return clients, rows.Err()
}

// SetGymColor sets the brand colour of a gym and returns the gym with its branches.
func (r *Repository) SetGymColor(ctx context.Context, gymID uuid.UUID, color string) (app.AdminGym, error) {
	gym := app.AdminGym{Branches: []app.AdminBranch{}}
	err := r.pool.QueryRow(ctx, `UPDATE gyms SET brand_color = $2, updated_at = now() WHERE id = $1
		RETURNING id, auth_tenant_id, tax_id, name, brand_color`, gymID, color).
		Scan(&gym.ID, &gym.AuthTenantID, &gym.RUC, &gym.Name, &gym.BrandColor)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.AdminGym{}, domain.ErrResourceNotFound
	}
	if err != nil {
		return app.AdminGym{}, err
	}
	rows, err := r.pool.Query(ctx, `SELECT id, name, address FROM branches WHERE gym_id = $1 ORDER BY created_at, id`, gymID)
	if err != nil {
		return app.AdminGym{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var b app.AdminBranch
		if err := rows.Scan(&b.ID, &b.Name, &b.Address); err != nil {
			return app.AdminGym{}, err
		}
		gym.Branches = append(gym.Branches, b)
	}
	return gym, rows.Err()
}

// AddBranch creates a branch in a gym.
func (r *Repository) AddBranch(ctx context.Context, gymID uuid.UUID, name string, address *string) (app.AdminBranch, error) {
	branch := app.AdminBranch{ID: ids.New()}
	err := r.pool.QueryRow(ctx, `INSERT INTO branches (id, gym_id, organization_id, name, address)
		SELECT $1, g.id, g.organization_id, $3, $4 FROM gyms g WHERE g.id = $2
		RETURNING name, address`, branch.ID, gymID, name, address).Scan(&branch.Name, &branch.Address)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.AdminBranch{}, domain.ErrResourceNotFound
	}
	return branch, err
}

// UpdateBranch renames a branch or changes its address.
func (r *Repository) UpdateBranch(ctx context.Context, id uuid.UUID, changes app.BranchChanges) (app.AdminBranch, error) {
	var branch app.AdminBranch
	err := r.pool.QueryRow(ctx, `UPDATE branches SET
		name = CASE WHEN $2 THEN $3 ELSE name END,
		address = CASE WHEN $4 THEN $5 ELSE address END,
		updated_at = now()
		WHERE id = $1 RETURNING id, name, address`, id, changes.Name.Set, changes.Name.Value, changes.Address.Set, changes.Address.Value).
		Scan(&branch.ID, &branch.Name, &branch.Address)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.AdminBranch{}, domain.ErrResourceNotFound
	}
	return branch, err
}

// branchUsage lists the tables whose rows would be lost or orphaned by removing a branch.
const branchUsage = `SELECT EXISTS (SELECT 1 FROM members WHERE home_branch_id = $1)
	OR EXISTS (SELECT 1 FROM receipts WHERE branch_id = $1)
	OR EXISTS (SELECT 1 FROM payments WHERE branch_id = $1)
	OR EXISTS (SELECT 1 FROM day_passes WHERE branch_id = $1)
	OR EXISTS (SELECT 1 FROM access_entries WHERE branch_id = $1)
	OR EXISTS (SELECT 1 FROM membership_branches WHERE branch_id = $1)
	OR EXISTS (SELECT 1 FROM user_branches WHERE branch_id = $1)`

// DeleteBranch removes a branch nobody has used. A gym keeps at least one.
func (r *Repository) DeleteBranch(ctx context.Context, id uuid.UUID) error {
	return (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		var gymID uuid.UUID
		if err := t.QueryRow(ctx, `SELECT gym_id FROM branches WHERE id = $1 FOR UPDATE`, id).Scan(&gymID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrResourceNotFound
			}
			return err
		}
		var siblings int
		if err := t.QueryRow(ctx, `SELECT count(*) FROM branches WHERE gym_id = $1 AND id <> $2`, gymID, id).Scan(&siblings); err != nil {
			return err
		}
		if siblings == 0 {
			return domain.ErrBranchInUse
		}
		var used bool
		if err := t.QueryRow(ctx, branchUsage, id).Scan(&used); err != nil {
			return err
		}
		if used {
			return domain.ErrBranchInUse
		}
		_, err := t.Exec(ctx, `DELETE FROM branches WHERE id = $1`, id)
		return err
	})
}
