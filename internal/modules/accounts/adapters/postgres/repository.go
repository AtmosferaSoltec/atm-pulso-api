// Package postgres implements the accounts module's storage ports on PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/tx"
)

// DefaultBranchName is the branch every new gym starts with.
const DefaultBranchName = "Sede principal"

// Repository stores groups, gyms, branches and users: the tables the module owns.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository returns a repository on pool.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

type queryReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func translateWrite(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		if pgErr.ConstraintName == "users_document_number_key" {
			return domain.ErrDocumentTaken
		}
		return domain.ErrConflict
	}
	return err
}

// FindTenancy finds the gym of a company and its group, checking the company belongs to the client of the token.
func (r *Repository) FindTenancy(ctx context.Context, clientID, tenantID uuid.UUID) (app.Tenancy, error) {
	var tenancy app.Tenancy
	err := r.pool.QueryRow(ctx, `SELECT group_id, id, features FROM gyms WHERE auth_tenant_id = $1 AND auth_client_id = $2`,
		tenantID, clientID).Scan(&tenancy.GroupID, &tenancy.GymID, &tenancy.Features)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.Tenancy{}, domain.ErrResourceNotFound
	}
	return tenancy, err
}

// EnsureTenancy creates the gym (with its default branch) when missing. A new gym joins the group of
// any gym of the same auth client, or a new group named after the client; the superadmin can move it later.
func (r *Repository) EnsureTenancy(ctx context.Context, in app.TenancyInput) (app.Tenancy, error) {
	var tenancy app.Tenancy
	err := (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
		var newGroup bool
		groupID := uuid.Nil
		err := t.QueryRow(ctx, `SELECT group_id FROM gyms WHERE auth_client_id = $1 ORDER BY created_at, id LIMIT 1`, in.ClientID).Scan(&groupID)
		if errors.Is(err, pgx.ErrNoRows) {
			groupID, newGroup = ids.New(), true
			if _, err := t.Exec(ctx, `INSERT INTO groups (id, name) VALUES ($1, $2)`, groupID, in.ClientName); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		var created bool
		err = t.QueryRow(ctx, `INSERT INTO gyms (id, group_id, name, tax_id, auth_tenant_id, auth_client_id)
			VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6)
			ON CONFLICT (auth_tenant_id) DO NOTHING RETURNING true`,
			ids.New(), groupID, in.TenantName, in.TenantTaxID, in.TenantID, in.ClientID).Scan(&created)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var client uuid.UUID
		if err := t.QueryRow(ctx, `SELECT id, group_id, auth_client_id, features FROM gyms WHERE auth_tenant_id = $1`, in.TenantID).Scan(&tenancy.GymID, &tenancy.GroupID, &client, &tenancy.Features); err != nil {
			return err
		}
		if client != in.ClientID {
			return domain.ErrConflict
		}
		if !created && newGroup {
			// A concurrent request recorded the gym first: do not leave our empty group behind.
			if _, err := t.Exec(ctx, `DELETE FROM groups WHERE id = $1`, groupID); err != nil {
				return err
			}
		}
		if created {
			_, err := t.Exec(ctx, `INSERT INTO branches (id, gym_id, name) VALUES ($1, $2, $3)`,
				ids.New(), tenancy.GymID, DefaultBranchName)
			return err
		}
		if in.RefreshNames {
			_, err := t.Exec(ctx, `UPDATE gyms SET name = $2, tax_id = COALESCE(NULLIF($3, ''), tax_id), updated_at = now()
				WHERE id = $1 AND (name <> $2 OR tax_id IS DISTINCT FROM COALESCE(NULLIF($3, ''), tax_id))`,
				tenancy.GymID, in.TenantName, in.TenantTaxID)
			return err
		}
		return nil
	})
	if err != nil {
		return app.Tenancy{}, err
	}
	return tenancy, nil
}

const userColumns = `id, gym_id, auth_user_id, document_number, full_name, phone, role, is_active`

func scanUser(row pgx.Row) (domain.User, error) {
	var (
		user  domain.User
		phone *string
		role  string
	)
	if err := row.Scan(&user.ID, &user.GymID, &user.AuthUserID, &user.DocumentNumber, &user.FullName, &phone, &role, &user.IsActive); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.ErrUserNotFound
		}
		return domain.User{}, err
	}
	if phone != nil {
		user.Phone = *phone
	}
	user.Role = domain.Role(role)
	return user, nil
}

// EnsureUser finds the person of a gym or creates them, keeping the role in step with the token.
func (r *Repository) EnsureUser(ctx context.Context, in app.UserInput) (domain.User, error) {
	user, err := scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE gym_id = $1 AND auth_user_id = $2`, in.GymID, in.AuthUserID))
	switch {
	case errors.Is(err, domain.ErrUserNotFound):
		user, err = scanUser(r.pool.QueryRow(ctx, `INSERT INTO users (id, gym_id, auth_user_id, document_number, full_name, role)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (gym_id, auth_user_id) DO UPDATE SET role = EXCLUDED.role
			RETURNING `+userColumns, ids.New(), in.GymID, in.AuthUserID, in.DocumentNumber, in.FullName, string(in.Role)))
		return user, translateWrite(err)
	case err != nil:
		return domain.User{}, err
	case user.Role != in.Role:
		err = (tx.Runner{Pool: r.pool}).Run(ctx, func(t pgx.Tx) error {
			if _, err := t.Exec(ctx, `UPDATE users SET role = $2, updated_at = now() WHERE id = $1`, user.ID, string(in.Role)); err != nil {
				return err
			}
			if in.Role == domain.RoleAdmin {
				_, err := t.Exec(ctx, `DELETE FROM user_branches WHERE user_id = $1`, user.ID)
				return err
			}
			return nil
		})
		user.Role = in.Role
		return user, err
	}
	return user, nil
}

// FindByID finds a user by id.
func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
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

// ScopeOf names the gym and branches the person works with. Every lookup is filtered by the gym
// of the principal, so a bad row can never leak another gym's names.
func (r *Repository) ScopeOf(ctx context.Context, p auth.Principal) (app.Scope, error) {
	scope := app.Scope{Branches: []app.BranchRef{}}
	if err := r.pool.QueryRow(ctx, `SELECT id, COALESCE(display_name, name), brand_color, features FROM gyms WHERE id = $1`, p.GymID).
		Scan(&scope.Gym.ID, &scope.Gym.Name, &scope.Gym.BrandColor, &scope.Gym.Features); err != nil {
		return app.Scope{}, fmt.Errorf("query gym: %w", err)
	}
	query := `SELECT b.id, b.name, g.id, COALESCE(g.display_name, g.name)
		FROM branches b JOIN gyms g ON g.id = b.gym_id
		WHERE b.gym_id = $1`
	args := []any{p.GymID}
	if p.Role == auth.RoleReceptionist {
		query += ` AND b.id IN (SELECT branch_id FROM user_branches WHERE user_id = $2)`
		args = append(args, p.UserID)
	}
	rows, err := r.pool.Query(ctx, query+` ORDER BY b.name, b.id`, args...)
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
	return scope, rows.Err()
}

// UpdateProfile changes name and phone. An empty phone is stored as NULL.
func (r *Repository) UpdateProfile(ctx context.Context, id uuid.UUID, fullName, phone string, at time.Time) (domain.User, error) {
	return scanUser(r.pool.QueryRow(ctx, `
		UPDATE users SET full_name = $2, phone = NULLIF($3, ''), updated_at = $4
		WHERE id = $1
		RETURNING `+userColumns, id, fullName, phone, at))
}
