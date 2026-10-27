package postgres

import (
	"context"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/google/uuid"
)

func (r *Repository) UpdateGym(ctx context.Context, org, id uuid.UUID, input app.GymChanges) error {
	tag, err := r.pool.Exec(ctx, `UPDATE gyms SET
 display_name=CASE WHEN $3 THEN $4 ELSE display_name END,
 brand_color=CASE WHEN $5 THEN $6 ELSE brand_color END,
 day_pass_price=CASE WHEN $7 THEN $8::numeric ELSE day_pass_price END,
 branch_sharing=CASE WHEN $9 THEN $10 ELSE branch_sharing END,updated_at=now()
 WHERE id=$1 AND organization_id=$2`, id, org, input.DisplayName.Set, input.DisplayName.Value, input.BrandColor.Set, input.BrandColor.Value, input.DayPassPrice.Set, input.DayPassPrice.Value, input.BranchSharing.Set, input.BranchSharing.Value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrResourceNotFound
	}
	return nil
}
