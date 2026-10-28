package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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
 display_name=CASE WHEN $3 THEN $4 ELSE display_name END,
 brand_color=CASE WHEN $5 THEN $6 ELSE brand_color END,
 day_pass_price=CASE WHEN $7 THEN $8::numeric ELSE day_pass_price END,
 branch_sharing=CASE WHEN $9 THEN $10 ELSE branch_sharing END,
 membership_prices=CASE WHEN $11 THEN $12::text::jsonb ELSE membership_prices END,updated_at=now()
 WHERE id=$1 AND organization_id=$2`, id, org, input.DisplayName.Set, input.DisplayName.Value, input.BrandColor.Set, input.BrandColor.Value, input.DayPassPrice.Set, input.DayPassPrice.Value, input.BranchSharing.Set, input.BranchSharing.Value, input.MembershipPrices.Set, prices)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrResourceNotFound
	}
	return nil
}

func (r *Repository) SetLogoKey(ctx context.Context, org, gymID uuid.UUID, key *string) (*string, error) {
	// The subselect reads the key before the update, which RETURNING would not.
	var previous *string
	err := r.pool.QueryRow(ctx, `UPDATE gyms g SET logo_key=$3,updated_at=now()
 FROM (SELECT id,logo_key FROM gyms WHERE id=$1 AND organization_id=$2 FOR UPDATE) old
 WHERE g.id=old.id RETURNING old.logo_key`, gymID, org, key).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrResourceNotFound
	}
	return previous, err
}

func (r *Repository) LogoKey(ctx context.Context, gymID uuid.UUID) (*string, error) {
	var key *string
	err := r.pool.QueryRow(ctx, `SELECT logo_key FROM gyms WHERE id=$1`, gymID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrResourceNotFound
	}
	return key, err
}
