package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/tx"
)

type teamTransaction struct {
	transaction    pgx.Tx
	organizationID uuid.UUID
}

func (r *Repository) WithTeam(ctx context.Context, organizationID uuid.UUID, fn func(app.TeamTransaction) error) error {
	return translateWrite((tx.Runner{Pool: r.pool}).Run(ctx, func(transaction pgx.Tx) error {
		if _, err := transaction.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, organizationID.String()); err != nil {
			return err
		}
		return fn(&teamTransaction{transaction: transaction, organizationID: organizationID})
	}))
}
func (t *teamTransaction) User(ctx context.Context, id uuid.UUID) (app.ManagedUser, error) {
	var managed app.ManagedUser
	user, err := scanUser(t.transaction.QueryRow(ctx, `SELECT `+userColumns+`,password_hash FROM users WHERE id=$1 AND organization_id=$2 FOR UPDATE`, id, t.organizationID), &managed.Credentials.PasswordHash)
	if errors.Is(err, domain.ErrUserNotFound) {
		return app.ManagedUser{}, domain.ErrResourceNotFound
	}
	if err != nil {
		return app.ManagedUser{}, err
	}
	managed.Credentials.User = user
	managed.Branches = []app.TeamBranch{}
	if user.GymID != uuid.Nil {
		gym := &app.CatalogName{}
		if err := t.transaction.QueryRow(ctx, `SELECT id,name FROM gyms WHERE id=$1 AND organization_id=$2`, user.GymID, t.organizationID).Scan(&gym.ID, &gym.Name); err != nil {
			return app.ManagedUser{}, err
		}
		managed.Gym = gym
	}
	rows, err := t.transaction.Query(ctx, `SELECT b.id,b.name,b.gym_id FROM user_branches ub JOIN branches b ON b.id=ub.branch_id AND b.organization_id=ub.organization_id WHERE ub.user_id=$1 AND ub.organization_id=$2 ORDER BY b.name,b.id`, id, t.organizationID)
	if err != nil {
		return app.ManagedUser{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var b app.TeamBranch
		if err := rows.Scan(&b.ID, &b.Name, &b.GymID); err != nil {
			return app.ManagedUser{}, err
		}
		managed.Branches = append(managed.Branches, b)
	}
	return managed, rows.Err()
}
func (t *teamTransaction) Users(ctx context.Context) ([]app.ManagedUser, error) {
	rows, err := t.transaction.Query(ctx, `SELECT id FROM users WHERE organization_id=$1 ORDER BY full_name,id`, t.organizationID)
	if err != nil {
		return nil, err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	users := make([]app.ManagedUser, 0, len(ids))
	for _, id := range ids {
		user, err := t.User(ctx, id)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}
func (t *teamTransaction) GymExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var exists bool
	err := t.transaction.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM gyms WHERE id=$1 AND organization_id=$2)`, id, t.organizationID).Scan(&exists)
	return exists, err
}
func (t *teamTransaction) Branch(ctx context.Context, id uuid.UUID) (app.TeamBranch, error) {
	var branch app.TeamBranch
	err := t.transaction.QueryRow(ctx, `SELECT id,name,gym_id FROM branches WHERE id=$1 AND organization_id=$2`, id, t.organizationID).Scan(&branch.ID, &branch.Name, &branch.GymID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.TeamBranch{}, domain.ErrForeignScope
	}
	return branch, err
}
func (t *teamTransaction) Save(ctx context.Context, user app.ManagedUser, create bool) error {
	c := user.Credentials
	var gymID *uuid.UUID
	if c.GymID != uuid.Nil {
		gymID = &c.GymID
	}
	args := []any{c.ID, t.organizationID, c.DocumentNumber, c.FullName, c.Phone, string(c.Role), gymID, c.PasswordHash, c.PasswordVersion, c.IsActive, c.MustChangePassword}
	var err error
	if create {
		_, err = t.transaction.Exec(ctx, `INSERT INTO users(id,organization_id,document_number,full_name,phone,role,gym_id,password_hash,password_version,is_active,must_change_password)VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,$8,$9,$10,$11)`, args...)
	} else {
		_, err = t.transaction.Exec(ctx, `UPDATE users SET document_number=$3,full_name=$4,phone=NULLIF($5,''),role=$6,gym_id=$7,password_hash=$8,password_version=$9,is_active=$10,must_change_password=$11,updated_at=now() WHERE id=$1 AND organization_id=$2`, args...)
	}
	if err != nil {
		return err
	}
	if _, err := t.transaction.Exec(ctx, `DELETE FROM user_branches WHERE user_id=$1 AND organization_id=$2`, c.ID, t.organizationID); err != nil {
		return err
	}
	if c.Role == domain.RoleOwner {
		if _, err := t.transaction.Exec(ctx, `UPDATE organizations SET name=$2,contact_name=$2,contact_document_number=$3,updated_at=now() WHERE id=$1`, t.organizationID, c.FullName, c.DocumentNumber); err != nil {
			return err
		}
	}
	for _, b := range user.Branches {
		if _, err := t.transaction.Exec(ctx, `INSERT INTO user_branches(user_id,branch_id,organization_id)VALUES($1,$2,$3)`, c.ID, b.ID, t.organizationID); err != nil {
			return err
		}
	}
	return nil
}
