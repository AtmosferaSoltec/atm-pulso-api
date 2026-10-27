package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
)

// Catalog reads only accounts-owned tables scoped to one organization.
func (r *Repository) Catalog(ctx context.Context, organizationID uuid.UUID) (app.Catalog, error) {
	return readCatalog(ctx, r.pool, organizationID)
}

func readCatalog(ctx context.Context, q queryReader, organizationID uuid.UUID) (app.Catalog, error) {
	catalog := app.Catalog{Gyms: []app.Gym{}}
	err := q.QueryRow(ctx, `SELECT id,name FROM organizations WHERE id=$1`, organizationID).Scan(&catalog.Organization.ID, &catalog.Organization.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Catalog{}, domain.ErrResourceNotFound
	}
	if err != nil {
		return app.Catalog{}, err
	}
	rows, err := q.Query(ctx, `SELECT id,name,display_name,tax_id,brand_color,COALESCE(day_pass_price,$2::numeric(14,2))::text,
 day_pass_price IS NULL,branch_sharing,membership_prices FROM gyms WHERE organization_id=$1 ORDER BY name,id`, organizationID, domain.DefaultDayPassPrice)
	if err != nil {
		return app.Catalog{}, err
	}
	defer rows.Close()
	for rows.Next() {
		gym := app.Gym{Branches: []app.Branch{}}
		var prices []byte
		if err := rows.Scan(&gym.ID, &gym.Name, &gym.DisplayName, &gym.TaxID, &gym.BrandColor, &gym.DayPassPrice, &gym.DayPassPriceIsDefault, &gym.BranchSharing, &prices); err != nil {
			return app.Catalog{}, err
		}
		if err := json.Unmarshal(prices, &gym.MembershipPrices); err != nil {
			return app.Catalog{}, err
		}
		catalog.Gyms = append(catalog.Gyms, gym)
	}
	if err := rows.Err(); err != nil {
		return app.Catalog{}, err
	}
	rows.Close()
	branches, err := q.Query(ctx, `SELECT id,gym_id,name,address FROM branches WHERE organization_id=$1 ORDER BY name,id`, organizationID)
	if err != nil {
		return app.Catalog{}, err
	}
	defer branches.Close()
	for branches.Next() {
		var branch app.Branch
		var gymID uuid.UUID
		if err := branches.Scan(&branch.ID, &gymID, &branch.Name, &branch.Address); err != nil {
			return app.Catalog{}, err
		}
		for i := range catalog.Gyms {
			if catalog.Gyms[i].ID == gymID {
				catalog.Gyms[i].Branches = append(catalog.Gyms[i].Branches, branch)
				break
			}
		}
	}
	return catalog, branches.Err()
}
