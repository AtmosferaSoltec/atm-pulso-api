package postgres

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
)

// UpdateGym applies the rules the admin of a gym may change.
func (r *Repository) UpdateGym(ctx context.Context, org, id uuid.UUID, input app.GymChanges) error {
	prices := "[]"
	if input.MembershipPrices.Set && input.MembershipPrices.Value != nil {
		encoded, err := json.Marshal(*input.MembershipPrices.Value)
		if err != nil {
			return err
		}
		prices = string(encoded)
	}
	tag, err := r.pool.Exec(ctx, `UPDATE gyms SET
 brand_color=CASE WHEN $3 THEN $4 ELSE brand_color END,
 day_pass_price=CASE WHEN $5 THEN $6::numeric ELSE day_pass_price END,
 branch_sharing=CASE WHEN $7 THEN $8 ELSE branch_sharing END,
 membership_prices=CASE WHEN $9 THEN $10::text::jsonb ELSE membership_prices END,updated_at=now()
 WHERE id=$1 AND organization_id=$2`, id, org, input.BrandColor.Set, input.BrandColor.Value, input.DayPassPrice.Set, input.DayPassPrice.Value, input.BranchSharing.Set, input.BranchSharing.Value, input.MembershipPrices.Set, prices)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrResourceNotFound
	}
	return nil
}
