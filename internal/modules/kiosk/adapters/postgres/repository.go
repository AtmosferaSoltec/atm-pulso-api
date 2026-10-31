// Package postgres is the persistence adapter of the kiosk module.
package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/kiosk/domain"
)

// Repository stores kiosk_settings.
type Repository struct{ pool *pgxpool.Pool }

// NewRepository builds the repository.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) PINHash(ctx context.Context, gymID uuid.UUID) (string, error) {
	var hash string
	err := r.pool.QueryRow(ctx, `SELECT pin_hash FROM kiosk_settings WHERE gym_id = $1`, gymID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrPINNotSet
	}
	return hash, err
}

func (r *Repository) SetPINHash(ctx context.Context, gymID, actorID uuid.UUID, hash string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO kiosk_settings (gym_id, pin_hash, updated_by) VALUES ($1,$2,$3)
		ON CONFLICT (gym_id) DO UPDATE SET pin_hash = EXCLUDED.pin_hash, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		gymID, hash, actorID)
	return err
}
