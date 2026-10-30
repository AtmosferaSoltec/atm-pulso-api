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

func teamBranches(ctx context.Context, q queryReader, gymID uuid.UUID) (map[uuid.UUID][]app.TeamBranch, error) {
	rows, err := q.Query(ctx, `SELECT ub.user_id, b.id, b.name, b.gym_id FROM user_branches ub
		JOIN branches b ON b.id = ub.branch_id AND b.organization_id = ub.organization_id
		WHERE b.gym_id = $1 ORDER BY b.name, b.id`, gymID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byUser := map[uuid.UUID][]app.TeamBranch{}
	for rows.Next() {
		var userID uuid.UUID
		var b app.TeamBranch
		if err := rows.Scan(&userID, &b.ID, &b.Name, &b.GymID); err != nil {
			return nil, err
		}
		byUser[userID] = append(byUser[userID], b)
	}
	return byUser, rows.Err()
}

// Team lists the people of a gym.
func (r *Repository) Team(ctx context.Context, gymID uuid.UUID) ([]app.ManagedUser, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+userColumns+` FROM users WHERE gym_id = $1 ORDER BY full_name, id`, gymID)
	if err != nil {
		return nil, err
	}
	var users []app.ManagedUser
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		users = append(users, app.ManagedUser{User: user})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byUser, err := teamBranches(ctx, r.pool, gymID)
	if err != nil {
		return nil, err
	}
	for i := range users {
		users[i].Branches = byUser[users[i].ID]
	}
	return users, nil
}

// TeamUser finds one person of the gym.
func (r *Repository) TeamUser(ctx context.Context, gymID, id uuid.UUID) (app.ManagedUser, error) {
	user, err := scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1 AND gym_id = $2`, id, gymID))
	if errors.Is(err, domain.ErrUserNotFound) {
		return app.ManagedUser{}, domain.ErrResourceNotFound
	}
	if err != nil {
		return app.ManagedUser{}, err
	}
	byUser, err := teamBranches(ctx, r.pool, gymID)
	if err != nil {
		return app.ManagedUser{}, err
	}
	return app.ManagedUser{User: user, Branches: byUser[id]}, nil
}

// GymBranches loads branches of the gym; any id outside it is a foreign scope.
func (r *Repository) GymBranches(ctx context.Context, gymID uuid.UUID, branchIDs []uuid.UUID) ([]app.TeamBranch, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, gym_id FROM branches WHERE gym_id = $1 AND id = ANY($2) ORDER BY name, id`, gymID, branchIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	branches := []app.TeamBranch{}
	for rows.Next() {
		var b app.TeamBranch
		if err := rows.Scan(&b.ID, &b.Name, &b.GymID); err != nil {
			return nil, err
		}
		branches = append(branches, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(branches) != len(branchIDs) {
		return nil, domain.ErrForeignScope
	}
	return branches, nil
}

// SaveTeamUser creates or updates the person by (gym, auth user).
func (r *Repository) SaveTeamUser(ctx context.Context, in app.TeamSave) (app.ManagedUser, error) {
	var id uuid.UUID
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		query := `INSERT INTO users (id, organization_id, gym_id, auth_user_id, document_number, full_name, role)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (gym_id, auth_user_id) DO UPDATE SET full_name = EXCLUDED.full_name, role = EXCLUDED.role, updated_at = now()
			RETURNING id`
		if in.KeepProfile {
			// Listing the team must not overwrite what the person edited about themselves.
			query = `INSERT INTO users (id, organization_id, gym_id, auth_user_id, document_number, full_name, role)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (gym_id, auth_user_id) DO UPDATE SET role = EXCLUDED.role, updated_at = CASE WHEN users.role <> EXCLUDED.role THEN now() ELSE users.updated_at END
			RETURNING id`
		}
		if err := t.QueryRow(ctx, query, ids.New(), in.OrganizationID, in.GymID, in.AuthUserID, in.DocumentNumber, in.FullName, string(in.Role)).Scan(&id); err != nil {
			return err
		}
		if in.Role == domain.RoleAdmin {
			in.ReplaceBranches, in.Branches = true, nil
		}
		if !in.ReplaceBranches {
			return nil
		}
		if _, err := t.Exec(ctx, `DELETE FROM user_branches WHERE user_id = $1`, id); err != nil {
			return err
		}
		for _, b := range in.Branches {
			if _, err := t.Exec(ctx, `INSERT INTO user_branches (user_id, branch_id, organization_id) VALUES ($1, $2, $3)`, id, b.ID, in.OrganizationID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return app.ManagedUser{}, translateWrite(err)
	}
	return r.TeamUser(ctx, in.GymID, id)
}

// SetActive blocks or unblocks a person in Pulso.
func (r *Repository) SetActive(ctx context.Context, gymID, id uuid.UUID, active bool) (app.ManagedUser, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE users SET is_active = $3, updated_at = now() WHERE id = $1 AND gym_id = $2`, id, gymID, active)
	if err != nil {
		return app.ManagedUser{}, err
	}
	if tag.RowsAffected() == 0 {
		return app.ManagedUser{}, domain.ErrResourceNotFound
	}
	return r.TeamUser(ctx, gymID, id)
}
