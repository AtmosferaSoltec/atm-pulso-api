package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/bodytracking/domain"
)

// MemberCard reads the name and document of a member of the gym (members table, read-only).
func (r *Repository) MemberCard(ctx context.Context, gymID, memberID uuid.UUID) (app.MemberCard, error) {
	var card app.MemberCard
	err := r.pool.QueryRow(ctx, `SELECT full_name, document_type, document_number FROM members WHERE gym_id = $1 AND id = $2`, gymID, memberID).
		Scan(&card.FullName, &card.DocumentType, &card.DocumentNumber)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.MemberCard{}, domain.ErrMemberNotFound
	}
	return card, err
}
