package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
)

// UpdateGym applies the rules the admin of a gym may change.
func (r *Repository) UpdateGym(ctx context.Context, id uuid.UUID, input app.GymChanges) error {
	tag, err := r.pool.Exec(ctx, `UPDATE gyms SET
 brand_color=CASE WHEN $2 THEN $3 ELSE brand_color END,
 day_pass_price=CASE WHEN $4 THEN $5::numeric ELSE day_pass_price END,
 branch_sharing=CASE WHEN $6 THEN $7 ELSE branch_sharing END,updated_at=now()
 WHERE id=$1`, id, input.BrandColor.Set, input.BrandColor.Value, input.DayPassPrice.Set, input.DayPassPrice.Value, input.BranchSharing.Set, input.BranchSharing.Value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrResourceNotFound
	}
	return nil
}
