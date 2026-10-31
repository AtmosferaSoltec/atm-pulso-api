package app

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// TeamUser is a person of the gym's team as the web sees them.
type TeamUser struct {
	ID             uuid.UUID    `json:"id"`
	FullName       string       `json:"full_name"`
	DocumentNumber string       `json:"document_number"`
	Phone          *string      `json:"phone"`
	Role           domain.Role  `json:"role"`
	IsActive       bool         `json:"is_active"`
	IsSelf         bool         `json:"is_self"`
	Branches       []TeamBranch `json:"branches"`
}

type TeamBranch struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	GymID uuid.UUID `json:"gym_id"`
}

// NewUser is a person to add to the team. The account itself lives in atm-auth-api: the DNI is
// their first password there and they must change it at their first login.
type NewUser struct {
	FullName       string      `json:"full_name"`
	DocumentNumber string      `json:"document_number"`
	Role           domain.Role `json:"role"`
	BranchIDs      []uuid.UUID `json:"branch_ids"`
}

// UserChanges edits a team member. The DNI cannot change: it is their identity in atm-auth-api.
type UserChanges struct {
	FullName  *string      `json:"full_name"`
	Role      *domain.Role `json:"role"`
	BranchIDs *[]uuid.UUID `json:"branch_ids"`
}

// ManagedUser is a person of the team with their branches.
type ManagedUser struct {
	domain.User
	Branches []TeamBranch
}

// TeamSave stores a person of the gym. Branches replace the assignments when ReplaceBranches is set.
type TeamSave struct {
	UserInput
	Branches        []TeamBranch
	ReplaceBranches bool
	// KeepProfile only records the person and their role, leaving the name they may have edited.
	KeepProfile bool
}

// TeamRepository stores the team of one gym.
type TeamRepository interface {
	// Team lists the people of the gym.
	Team(ctx context.Context, gymID uuid.UUID) ([]ManagedUser, error)
	// TeamUser returns domain.ErrResourceNotFound when the person is not in the gym.
	TeamUser(ctx context.Context, gymID, id uuid.UUID) (ManagedUser, error)
	// GymBranches returns domain.ErrForeignScope when any id is not a branch of the gym.
	GymBranches(ctx context.Context, gymID uuid.UUID, ids []uuid.UUID) ([]TeamBranch, error)
	// SaveTeamUser creates or updates the person by (gym, auth user) and returns them.
	SaveTeamUser(ctx context.Context, in TeamSave) (ManagedUser, error)
	SetActive(ctx context.Context, gymID, id uuid.UUID, active bool) (ManagedUser, error)
}

func (s *Service) teamView(user ManagedUser, self uuid.UUID, active bool) TeamUser {
	var phone *string
	if user.Phone != "" {
		phone = &user.Phone
	}
	branches := user.Branches
	if branches == nil {
		branches = []TeamBranch{}
	}
	return TeamUser{ID: user.ID, FullName: user.FullName, DocumentNumber: user.DocumentNumber, Phone: phone,
		Role: user.Role, IsActive: active, IsSelf: user.ID == self, Branches: branches}
}

func requireTeamManager(p auth.Principal) error {
	if !p.CanManageTeam() {
		return domain.ErrForbidden
	}
	return nil
}

// ListUsers lists the team of the current gym: the people atm-auth-api lets into it, with the
// branches Pulso assigned. People who never entered are recorded here the first time they are listed.
func (s *Service) ListUsers(ctx context.Context, p auth.Principal, page, size int) ([]TeamUser, int64, error) {
	if err := requireTeamManager(p); err != nil {
		return nil, 0, err
	}
	members, err := s.Directory.Members(ctx, p.TenantID.String())
	known := err == nil
	if err != nil && !errors.Is(err, domain.ErrAuthUnavailable) {
		return nil, 0, err
	}
	authActive := map[uuid.UUID]bool{}
	for _, member := range members {
		authUser, parseErr := uuid.Parse(member.UserID)
		role := domain.Role(member.Role)
		if parseErr != nil || !role.Valid() || !domain.ValidDocumentNumber(member.DocumentNumber) {
			continue
		}
		authActive[authUser] = member.Active
		if _, err := s.Team.SaveTeamUser(ctx, TeamSave{UserInput: UserInput{
			GymID: p.GymID, AuthUserID: authUser,
			DocumentNumber: member.DocumentNumber, FullName: member.FullName, Role: role,
		}, KeepProfile: true}); err != nil {
			return nil, 0, err
		}
	}
	users, err := s.Team.Team(ctx, p.GymID)
	if err != nil {
		return nil, 0, err
	}
	result := []TeamUser{}
	for _, user := range users {
		active := user.IsActive
		if known {
			enabled, listed := authActive[user.AuthUserID]
			if !listed {
				continue
			}
			active = active && enabled
		}
		result = append(result, s.teamView(user, p.UserID, active))
	}
	total := len(result)
	start := min((page-1)*size, total)
	end := min(start+size, total)
	return result[start:end], int64(total), nil
}

// branchesFor validates the scope asked for a role and loads the branches of the current gym.
func (s *Service) branchesFor(ctx context.Context, p auth.Principal, role domain.Role, ids []uuid.UUID) ([]TeamBranch, error) {
	if role == domain.RoleAdmin {
		if len(ids) > 0 {
			return nil, fieldError("branch_ids", "SCOPE_INVALID")
		}
		return []TeamBranch{}, nil
	}
	unique := []uuid.UUID{}
	for _, id := range ids {
		if !slices.Contains(unique, id) {
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return nil, fieldError("branch_ids", "BRANCH_REQUIRED")
	}
	return s.Team.GymBranches(ctx, p.GymID, unique)
}

// CreateUser adds a person to the team: the account and the access in atm-auth-api, then
// the branches here.
func (s *Service) CreateUser(ctx context.Context, p auth.Principal, input NewUser) (TeamUser, error) {
	if err := requireTeamManager(p); err != nil {
		return TeamUser{}, err
	}
	if !input.Role.Valid() {
		return TeamUser{}, domain.ErrRoleNotAssignable
	}
	if !domain.ValidDocumentNumber(input.DocumentNumber) {
		return TeamUser{}, fieldError("document_number", "DOCUMENT_NUMBER_INVALID")
	}
	name, _, err := domain.ProfileChanges(input.FullName, "")
	if err != nil {
		return TeamUser{}, err
	}
	branches, err := s.branchesFor(ctx, p, input.Role, input.BranchIDs)
	if err != nil {
		return TeamUser{}, err
	}
	member, err := s.Directory.AddMember(ctx, p.TenantID.String(), NewDirectoryMember{
		DocumentNumber: input.DocumentNumber, FullName: name, Role: string(input.Role),
	})
	if err != nil {
		return TeamUser{}, err
	}
	authUser, err := uuid.Parse(member.UserID)
	if err != nil {
		return TeamUser{}, domain.ErrAuthUnavailable
	}
	saved, err := s.Team.SaveTeamUser(ctx, TeamSave{
		UserInput: UserInput{GymID: p.GymID, AuthUserID: authUser,
			DocumentNumber: input.DocumentNumber, FullName: name, Role: input.Role},
		Branches: branches, ReplaceBranches: true,
	})
	if err != nil {
		return TeamUser{}, err
	}
	return s.teamView(saved, p.UserID, saved.IsActive), nil
}

func (s *Service) managed(ctx context.Context, p auth.Principal, id uuid.UUID) (ManagedUser, error) {
	if err := requireTeamManager(p); err != nil {
		return ManagedUser{}, err
	}
	user, err := s.Team.TeamUser(ctx, p.GymID, id)
	if err != nil {
		return ManagedUser{}, err
	}
	if user.ID == p.UserID {
		return ManagedUser{}, domain.ErrCannotModifySelf
	}
	return user, nil
}

// UpdateUser changes a person's name, role or branches. A new role is also applied in atm-auth-api.
func (s *Service) UpdateUser(ctx context.Context, p auth.Principal, id uuid.UUID, input UserChanges) (TeamUser, error) {
	user, err := s.managed(ctx, p, id)
	if err != nil {
		return TeamUser{}, err
	}
	save := TeamSave{UserInput: UserInput{GymID: p.GymID, AuthUserID: user.AuthUserID,
		DocumentNumber: user.DocumentNumber, FullName: user.FullName, Role: user.Role}}
	if input.FullName != nil {
		name, _, err := domain.ProfileChanges(*input.FullName, "")
		if err != nil {
			return TeamUser{}, err
		}
		save.FullName = name
	}
	role := user.Role
	if input.Role != nil {
		if !input.Role.Valid() {
			return TeamUser{}, domain.ErrRoleNotAssignable
		}
		role = *input.Role
	}
	if role != user.Role || input.BranchIDs != nil {
		ids := []uuid.UUID{}
		switch {
		case input.BranchIDs != nil:
			ids = *input.BranchIDs
		case role == user.Role:
			for _, b := range user.Branches {
				ids = append(ids, b.ID)
			}
		}
		save.Branches, err = s.branchesFor(ctx, p, role, ids)
		if err != nil {
			return TeamUser{}, err
		}
		save.ReplaceBranches = true
	}
	if role != user.Role {
		if err := s.Directory.ChangeRole(ctx, p.TenantID.String(), user.AuthUserID.String(), string(role)); err != nil {
			return TeamUser{}, err
		}
		save.Role = role
	}
	saved, err := s.Team.SaveTeamUser(ctx, save)
	if err != nil {
		return TeamUser{}, err
	}
	return s.teamView(saved, p.UserID, saved.IsActive), nil
}

// SetUserActive blocks or unblocks the person in Pulso. Their account in atm-auth-api is untouched.
func (s *Service) SetUserActive(ctx context.Context, p auth.Principal, id uuid.UUID, active bool) (TeamUser, error) {
	if _, err := s.managed(ctx, p, id); err != nil {
		return TeamUser{}, err
	}
	saved, err := s.Team.SetActive(ctx, p.GymID, id, active)
	if err != nil {
		return TeamUser{}, err
	}
	return s.teamView(saved, p.UserID, saved.IsActive), nil
}

// ResetUserPassword sends the person back to their DNI as password, in atm-auth-api.
func (s *Service) ResetUserPassword(ctx context.Context, p auth.Principal, id uuid.UUID) (TeamUser, error) {
	user, err := s.managed(ctx, p, id)
	if err != nil {
		return TeamUser{}, err
	}
	if err := s.Directory.ResetPassword(ctx, p.TenantID.String(), user.AuthUserID.String()); err != nil {
		return TeamUser{}, err
	}
	return s.teamView(user, p.UserID, user.IsActive), nil
}
