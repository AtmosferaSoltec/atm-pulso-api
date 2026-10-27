package app

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
)

type TeamUser struct {
	ID             uuid.UUID    `json:"id"`
	FullName       string       `json:"full_name"`
	DocumentNumber string       `json:"document_number"`
	Phone          *string      `json:"phone"`
	Role           domain.Role  `json:"role"`
	IsActive       bool         `json:"is_active"`
	IsSelf         bool         `json:"is_self"`
	Gym            *CatalogName `json:"gym"`
	Branches       []TeamBranch `json:"branches"`
}

type TeamBranch struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	GymID uuid.UUID `json:"gym_id"`
}

type NewUser struct {
	ID             uuid.UUID   `json:"id"`
	FullName       string      `json:"full_name"`
	DocumentNumber string      `json:"document_number"`
	Role           domain.Role `json:"role"`
	GymID          uuid.UUID   `json:"gym_id"`
	BranchIDs      []uuid.UUID `json:"branch_ids"`
}

type UserChanges struct {
	FullName       *string      `json:"full_name"`
	DocumentNumber *string      `json:"document_number"`
	Role           *domain.Role `json:"role"`
	GymID          *uuid.UUID   `json:"gym_id"`
	BranchIDs      *[]uuid.UUID `json:"branch_ids"`
}

// ManagedUser carries persistence data inside the module, never as an HTTP response.
type ManagedUser struct {
	Credentials domain.Credentials
	Branches    []TeamBranch
	Gym         *CatalogName
}

type TeamTransaction interface {
	User(context.Context, uuid.UUID) (ManagedUser, error)
	Users(context.Context) ([]ManagedUser, error)
	GymExists(context.Context, uuid.UUID) (bool, error)
	Branch(context.Context, uuid.UUID) (TeamBranch, error)
	Save(context.Context, ManagedUser, bool) error
}

type TeamRepository interface {
	WithTeam(context.Context, uuid.UUID, func(TeamTransaction) error) error
}

func teamView(user ManagedUser, self uuid.UUID) TeamUser {
	c := user.Credentials
	var phone *string
	if c.Phone != "" {
		phone = &c.Phone
	}
	branches := user.Branches
	if branches == nil {
		branches = []TeamBranch{}
	}
	return TeamUser{ID: c.ID, FullName: c.FullName, DocumentNumber: c.DocumentNumber, Phone: phone, Role: c.Role, IsActive: c.IsActive, IsSelf: c.ID == self, Gym: user.Gym, Branches: branches}
}
func teamVisible(p auth.Principal, user ManagedUser) bool {
	if p.Role == auth.RoleOwner || p.Role == auth.RoleSuperadmin {
		return true
	}
	if p.Role != auth.RoleAdmin {
		return false
	}
	if user.Credentials.ID == p.UserID {
		return true
	}
	if user.Credentials.Role != domain.RoleReceptionist || len(user.Branches) == 0 {
		return false
	}
	for _, branch := range user.Branches {
		if branch.GymID != p.GymID {
			return false
		}
	}
	return true
}
func manageUser(p auth.Principal, user ManagedUser) error {
	if !p.CanManageTeam() {
		return domain.ErrForbidden
	}
	if user.Credentials.ID == p.UserID {
		return domain.ErrCannotModifySelf
	}
	if user.Credentials.Role == domain.RoleOwner {
		return domain.ErrCannotModifyOwner
	}
	if !teamVisible(p, user) {
		return domain.ErrScopeNotAllowed
	}
	return nil
}
func (s *Service) ListUsers(ctx context.Context, p auth.Principal, page, size int) ([]TeamUser, int64, error) {
	if !p.CanManageTeam() && p.Role != auth.RoleSuperadmin {
		return nil, 0, domain.ErrForbidden
	}
	result := []TeamUser{}
	err := s.Team.WithTeam(ctx, p.OrganizationID, func(tx TeamTransaction) error {
		users, err := tx.Users(ctx)
		if err != nil {
			return err
		}
		for _, user := range users {
			if teamVisible(p, user) {
				result = append(result, teamView(user, p.UserID))
			}
		}
		return nil
	})
	total := len(result)
	start := min((page-1)*size, total)
	end := min(start+size, total)
	return result[start:end], int64(total), err
}
func assignScope(ctx context.Context, tx TeamTransaction, p auth.Principal, user *ManagedUser, input NewUser) error {
	if input.Role != domain.RoleAdmin && input.Role != domain.RoleReceptionist {
		return domain.ErrRoleNotAssignable
	}
	if input.Role == domain.RoleAdmin && p.Role != auth.RoleOwner {
		return domain.ErrRoleNotAssignable
	}
	user.Credentials.Role = input.Role
	user.Credentials.GymID = uuid.Nil
	user.Gym = nil
	user.Branches = []TeamBranch{}
	if input.Role == domain.RoleAdmin {
		if input.GymID == uuid.Nil {
			return fieldError("gym_id", "GYM_REQUIRED")
		}
		if len(input.BranchIDs) > 0 {
			return fieldError("branch_ids", "SCOPE_INVALID")
		}
		exists, err := tx.GymExists(ctx, input.GymID)
		if err != nil {
			return err
		}
		if !exists {
			return domain.ErrForeignScope
		}
		user.Credentials.GymID = input.GymID
		return nil
	}
	if input.GymID != uuid.Nil {
		return fieldError("gym_id", "SCOPE_INVALID")
	}
	if len(input.BranchIDs) == 0 {
		return fieldError("branch_ids", "BRANCH_REQUIRED")
	}
	seen := []uuid.UUID{}
	for _, id := range input.BranchIDs {
		if slices.Contains(seen, id) {
			continue
		}
		seen = append(seen, id)
		branch, err := tx.Branch(ctx, id)
		if err != nil {
			return err
		}
		if !p.CanAccessBranch(auth.BranchRef{ID: branch.ID, GymID: branch.GymID}) {
			return domain.ErrScopeNotAllowed
		}
		user.Branches = append(user.Branches, branch)
	}
	return nil
}
func (s *Service) CreateUser(ctx context.Context, p auth.Principal, input NewUser) (TeamUser, error) {
	if !p.CanManageTeam() {
		return TeamUser{}, domain.ErrForbidden
	}
	if !domain.ValidDocumentNumber(input.DocumentNumber) {
		return TeamUser{}, fieldError("document_number", "DOCUMENT_NUMBER_INVALID")
	}
	name, _, err := domain.ProfileChanges(input.FullName, "")
	if err != nil {
		return TeamUser{}, err
	}
	if input.ID == uuid.Nil {
		input.ID = ids.New()
	}
	var result TeamUser
	err = s.Team.WithTeam(ctx, p.OrganizationID, func(tx TeamTransaction) error {
		existing, err := tx.User(ctx, input.ID)
		if err == nil {
			if !teamVisible(p, existing) {
				return domain.ErrScopeNotAllowed
			}
			result = teamView(existing, p.UserID)
			return nil
		}
		if err != domain.ErrResourceNotFound {
			return err
		}
		user := ManagedUser{Credentials: domain.Credentials{User: domain.User{ID: input.ID, OrganizationID: p.OrganizationID, DocumentNumber: input.DocumentNumber, FullName: name, IsActive: true, PasswordVersion: 1, MustChangePassword: true}}}
		if err := assignScope(ctx, tx, p, &user, input); err != nil {
			return err
		}
		hash, err := s.Hasher.Hash(input.DocumentNumber)
		if err != nil {
			return err
		}
		user.Credentials.PasswordHash = hash
		if err := tx.Save(ctx, user, true); err != nil {
			return err
		}
		user, err = tx.User(ctx, input.ID)
		result = teamView(user, p.UserID)
		return err
	})
	return result, err
}
func (s *Service) UpdateUser(ctx context.Context, p auth.Principal, id uuid.UUID, input UserChanges) (TeamUser, error) {
	return s.mutateUser(ctx, p, id, func(ctx context.Context, tx TeamTransaction, user *ManagedUser) error {
		if input.FullName != nil {
			name, _, err := domain.ProfileChanges(*input.FullName, "")
			if err != nil {
				return err
			}
			user.Credentials.FullName = name
		}
		if input.DocumentNumber != nil && *input.DocumentNumber != user.Credentials.DocumentNumber {
			if !domain.ValidDocumentNumber(*input.DocumentNumber) {
				return fieldError("document_number", "DOCUMENT_NUMBER_INVALID")
			}
			user.Credentials.DocumentNumber = *input.DocumentNumber
			user.Credentials.PasswordVersion++
			if user.Credentials.MustChangePassword {
				hash, err := s.Hasher.Hash(*input.DocumentNumber)
				if err != nil {
					return err
				}
				user.Credentials.PasswordHash = hash
			}
		}
		if input.Role != nil || input.GymID != nil || input.BranchIDs != nil {
			scope := NewUser{Role: user.Credentials.Role, GymID: user.Credentials.GymID}
			for _, b := range user.Branches {
				scope.BranchIDs = append(scope.BranchIDs, b.ID)
			}
			if input.Role != nil {
				scope.Role = *input.Role
				if scope.Role != user.Credentials.Role {
					scope.GymID = uuid.Nil
					scope.BranchIDs = nil
				}
			}
			if input.GymID != nil {
				scope.GymID = *input.GymID
			}
			if input.BranchIDs != nil {
				scope.BranchIDs = *input.BranchIDs
			}
			return assignScope(ctx, tx, p, user, scope)
		}
		return nil
	})
}
func (s *Service) SetUserActive(ctx context.Context, p auth.Principal, id uuid.UUID, active bool) (TeamUser, error) {
	return s.mutateUser(ctx, p, id, func(_ context.Context, _ TeamTransaction, user *ManagedUser) error {
		user.Credentials.IsActive = active
		if !active {
			user.Credentials.PasswordVersion++
		}
		return nil
	})
}
func (s *Service) ResetUserPassword(ctx context.Context, p auth.Principal, id uuid.UUID) (TeamUser, error) {
	return s.mutateUser(ctx, p, id, func(_ context.Context, _ TeamTransaction, user *ManagedUser) error {
		hash, err := s.Hasher.Hash(user.Credentials.DocumentNumber)
		if err != nil {
			return err
		}
		user.Credentials.PasswordHash = hash
		user.Credentials.PasswordVersion++
		user.Credentials.MustChangePassword = true
		return nil
	})
}
func (s *Service) mutateUser(ctx context.Context, p auth.Principal, id uuid.UUID, change func(context.Context, TeamTransaction, *ManagedUser) error) (TeamUser, error) {
	if !p.CanManageTeam() {
		return TeamUser{}, domain.ErrForbidden
	}
	var result TeamUser
	err := s.Team.WithTeam(ctx, p.OrganizationID, func(tx TeamTransaction) error {
		user, err := tx.User(ctx, id)
		if err != nil {
			return err
		}
		if err := manageUser(p, user); err != nil {
			return err
		}
		if err := change(ctx, tx, &user); err != nil {
			return err
		}
		if err := tx.Save(ctx, user, false); err != nil {
			return err
		}
		updated, err := tx.User(ctx, id)
		result = teamView(updated, p.UserID)
		return err
	})
	return result, err
}
