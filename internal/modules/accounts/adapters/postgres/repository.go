// Package postgres implements the accounts module's storage port on PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
)

// Repository stores users and reads the tenancy tables the module owns.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository returns a repository on pool.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const userColumns = `id, organization_id, gym_id, document_number, full_name, phone, role, is_active, password_version`

func scanUser(row pgx.Row, extra ...any) (domain.User, error) {
	var (
		user         domain.User
		organization *uuid.UUID
		gym          *uuid.UUID
		phone        *string
		role         string
	)
	dest := append([]any{&user.ID, &organization, &gym, &user.DocumentNumber, &user.FullName, &phone, &role, &user.IsActive, &user.PasswordVersion}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.ErrUserNotFound
		}
		return domain.User{}, err
	}
	if organization != nil {
		user.OrganizationID = *organization
	}
	if gym != nil {
		user.GymID = *gym
	}
	if phone != nil {
		user.Phone = *phone
	}
	user.Role = domain.Role(role)
	return user, nil
}

func (r *Repository) credentials(ctx context.Context, where string, arg any) (domain.Credentials, error) {
	var creds domain.Credentials
	user, err := scanUser(r.pool.QueryRow(ctx, "SELECT "+userColumns+", password_hash FROM users WHERE "+where, arg), &creds.PasswordHash)
	if err != nil {
		return domain.Credentials{}, err
	}
	creds.User = user
	return creds, nil
}

// FindCredentialsByDocument finds a user by DNI, with their password hash.
func (r *Repository) FindCredentialsByDocument(ctx context.Context, documentNumber string) (domain.Credentials, error) {
	return r.credentials(ctx, "document_number = $1", documentNumber)
}

// FindCredentialsByID finds a user by id, with their password hash.
func (r *Repository) FindCredentialsByID(ctx context.Context, id uuid.UUID) (domain.Credentials, error) {
	return r.credentials(ctx, "id = $1", id)
}

// FindByID finds a user by id.
func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx, "SELECT "+userColumns+" FROM users WHERE id = $1", id))
}

// BranchIDs returns the branches a receptionist works in.
func (r *Repository) BranchIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `SELECT branch_id FROM user_branches WHERE user_id = $1 ORDER BY branch_id`, userID)
	if err != nil {
		return nil, fmt.Errorf("query user branches: %w", err)
	}
	defer rows.Close()
	branchIDs := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan user branch: %w", err)
		}
		branchIDs = append(branchIDs, id)
	}
	return branchIDs, rows.Err()
}

// ScopeOf names the organization, gym and branches the user belongs to. Every lookup
// is filtered by the user's organization, so a bad row can never leak another client's names.
func (r *Repository) ScopeOf(ctx context.Context, user domain.User) (app.Scope, error) {
	scope := app.Scope{Branches: []app.BranchRef{}}
	if user.OrganizationID == uuid.Nil {
		return scope, nil
	}

	var organization app.Named
	err := r.pool.QueryRow(ctx, `SELECT id, name FROM organizations WHERE id = $1`, user.OrganizationID).
		Scan(&organization.ID, &organization.Name)
	if err != nil {
		return app.Scope{}, fmt.Errorf("query organization: %w", err)
	}
	scope.Organization = &organization

	if user.GymID != uuid.Nil {
		var gym app.Named
		err := r.pool.QueryRow(ctx, `SELECT id, name FROM gyms WHERE id = $1 AND organization_id = $2`, user.GymID, user.OrganizationID).
			Scan(&gym.ID, &gym.Name)
		if err != nil {
			return app.Scope{}, fmt.Errorf("query gym: %w", err)
		}
		scope.Gym = &gym
	}

	if user.Role == domain.RoleReceptionist {
		rows, err := r.pool.Query(ctx, `
			SELECT b.id, b.name, g.id, g.name
			FROM user_branches ub
			JOIN branches b ON b.id = ub.branch_id
			JOIN gyms g ON g.id = b.gym_id AND g.organization_id = $2
			WHERE ub.user_id = $1
			ORDER BY g.name, b.name`, user.ID, user.OrganizationID)
		if err != nil {
			return app.Scope{}, fmt.Errorf("query branches: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var branch app.BranchRef
			if err := rows.Scan(&branch.ID, &branch.Name, &branch.GymID, &branch.GymName); err != nil {
				return app.Scope{}, fmt.Errorf("scan branch: %w", err)
			}
			scope.Branches = append(scope.Branches, branch)
		}
		if err := rows.Err(); err != nil {
			return app.Scope{}, err
		}
	}
	return scope, nil
}

// UpdateProfile changes name and phone. An empty phone is stored as NULL.
func (r *Repository) UpdateProfile(ctx context.Context, id uuid.UUID, fullName, phone string, at time.Time) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `
		UPDATE users SET full_name = $2, phone = NULLIF($3, ''), updated_at = $4
		WHERE id = $1
		RETURNING `+userColumns, id, fullName, phone, at))
}

// UpdatePassword stores a new hash and bumps the password version, which kills older tokens.
func (r *Repository) UpdatePassword(ctx context.Context, id uuid.UUID, passwordHash string, at time.Time) (int, error) {
	var version int
	err := r.pool.QueryRow(ctx, `
		UPDATE users SET password_hash = $2, password_version = password_version + 1, updated_at = $3
		WHERE id = $1
		RETURNING password_version`, id, passwordHash, at).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrUserNotFound
	}
	return version, err
}
