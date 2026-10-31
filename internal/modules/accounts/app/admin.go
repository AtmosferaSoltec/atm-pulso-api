package app

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/logger"
)

// AdminBranch is a branch as the superadmin sees it.
type AdminBranch struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Address *string   `json:"address"`
}

// AdminGym is a company with Pulso enabled: a brand that belongs to one group.
type AdminGym struct {
	ID           uuid.UUID `json:"id"`
	GroupID      uuid.UUID `json:"group_id"`
	AuthTenantID uuid.UUID `json:"auth_tenant_id"`
	AuthClientID uuid.UUID `json:"auth_client_id"`
	// RUC is null when the company has none.
	RUC        *string `json:"ruc"`
	Name       string  `json:"name"`
	BrandColor string  `json:"brand_color"`
	// Features are the optional modules the gym has switched on ("sales", "body_tracking", "kiosk").
	Features []string      `json:"features"`
	Branches []AdminBranch `json:"branches"`
}

// AdminGroup is an internal label that bundles related gyms. It is not a user.
type AdminGroup struct {
	ID   uuid.UUID  `json:"id"`
	Name string     `json:"name"`
	Gyms []AdminGym `json:"gyms"`
}

// GymIdentity is what the superadmin configures about a gym from the console: its colour and the
// group it belongs to.
type GymIdentity struct {
	BrandColor Optional[string]    `json:"brand_color"`
	GroupID    Optional[uuid.UUID] `json:"group_id"`
}

// BranchChanges edits a branch.
type BranchChanges struct {
	Name    Optional[string] `json:"name"`
	Address Optional[string] `json:"address"`
}

// AdminRepository is what the superadmin's view needs.
type AdminRepository interface {
	Groups(ctx context.Context) ([]AdminGroup, error)
	// CreateGroup stores an empty group.
	CreateGroup(ctx context.Context, name string) (AdminGroup, error)
	// RenameGroup returns domain.ErrResourceNotFound when the group does not exist.
	RenameGroup(ctx context.Context, id uuid.UUID, name string) (AdminGroup, error)
	// DeleteGroup returns domain.ErrResourceNotFound, or domain.ErrGroupInUse when it still has gyms.
	DeleteGroup(ctx context.Context, id uuid.UUID) error
	// UpdateGymIdentity sets the colour and/or the group of a gym (nil leaves it as is). It returns
	// domain.ErrResourceNotFound when the gym does not exist and domain.ErrForeignScope when the group does not.
	UpdateGymIdentity(ctx context.Context, gymID uuid.UUID, color *string, groupID *uuid.UUID) (AdminGym, error)
	// UpdateGymFeatures replaces the modules a gym has switched on. It returns domain.ErrResourceNotFound
	// when the gym does not exist.
	UpdateGymFeatures(ctx context.Context, gymID uuid.UUID, features []string) (AdminGym, error)
	// AddBranch returns domain.ErrResourceNotFound when the gym does not exist.
	AddBranch(ctx context.Context, gymID uuid.UUID, name string, address *string) (AdminBranch, error)
	UpdateBranch(ctx context.Context, id uuid.UUID, changes BranchChanges) (AdminBranch, error)
	// DeleteBranch returns domain.ErrBranchInUse when the branch has history or people, or is the last of its gym.
	DeleteBranch(ctx context.Context, id uuid.UUID) error
}

const maxBranchName = 80

// maxGroupName is the longest group name.
const maxGroupName = 80

func groupName(name string) (string, error) {
	clean := strings.Join(strings.Fields(name), " ")
	if n := utf8.RuneCountInString(clean); n < 1 || n > maxGroupName {
		return "", fieldError("name", "NAME_REQUIRED")
	}
	return clean, nil
}

func branchName(name string) (string, error) {
	clean := strings.Join(strings.Fields(name), " ")
	if n := utf8.RuneCountInString(clean); n < 1 || n > maxBranchName {
		return "", fieldError("name", "NAME_REQUIRED")
	}
	return clean, nil
}

func branchAddress(address *string) (*string, error) {
	if address == nil {
		return nil, nil
	}
	clean := strings.Join(strings.Fields(*address), " ")
	if clean == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(clean) > 200 {
		return nil, fieldError("address", "ADDRESS_INVALID")
	}
	return &clean, nil
}

// SyncGyms records every company that has Pulso enabled in atm-auth-api as a gym with a default
// branch, in the group of its client (a new one when it has none yet). When atm-auth-api cannot be reached the
// superadmin still sees what is already stored.
func (s *Service) SyncGyms(ctx context.Context) {
	tenants, err := s.Directory.Tenants(ctx)
	if err != nil {
		if !errors.Is(err, domain.ErrAuthUnavailable) {
			logger.From(ctx).Warn("sync clients", "error", err)
		}
		return
	}
	for _, tenant := range tenants {
		clientID, errClient := uuid.Parse(tenant.ClientID)
		tenantID, errTenant := uuid.Parse(tenant.ID)
		if errClient != nil || errTenant != nil {
			continue
		}
		name := tenant.Name
		if name == "" {
			name = tenant.RUC
		}
		clientName := tenant.ClientName
		if clientName == "" {
			clientName = name
		}
		if _, err := s.Tenancy.EnsureTenancy(ctx, TenancyInput{ClientID: clientID, ClientName: clientName,
			TenantID: tenantID, TenantName: name, TenantTaxID: tenant.RUC, RefreshNames: true}); err != nil {
			logger.From(ctx).Warn("sync client", "tenant_id", tenant.ID, "error", err)
		}
	}
}

// ListGroups is the superadmin's view: every group with its gyms and branches.
func (s *Service) ListGroups(ctx context.Context) ([]AdminGroup, error) {
	s.SyncGyms(ctx)
	groups, err := s.Admin.Groups(ctx)
	for gi := range groups {
		for yi := range groups[gi].Gyms {
			groups[gi].Gyms[yi].Features = KnownFeatures(groups[gi].Gyms[yi].Features)
		}
	}
	return groups, err
}

// CreateGroup adds an empty group. Only the superadmin does.
func (s *Service) CreateGroup(ctx context.Context, name string) (AdminGroup, error) {
	clean, err := groupName(name)
	if err != nil {
		return AdminGroup{}, err
	}
	return s.Admin.CreateGroup(ctx, clean)
}

// RenameGroup changes the name of a group.
func (s *Service) RenameGroup(ctx context.Context, id uuid.UUID, name string) (AdminGroup, error) {
	clean, err := groupName(name)
	if err != nil {
		return AdminGroup{}, err
	}
	return s.Admin.RenameGroup(ctx, id, clean)
}

// DeleteGroup removes a group that has no gyms.
func (s *Service) DeleteGroup(ctx context.Context, id uuid.UUID) error {
	return s.Admin.DeleteGroup(ctx, id)
}

// UpdateGymIdentity sets the colour of a gym and/or moves it to another group. Only the superadmin
// does, from the console. At least one field must be sent.
func (s *Service) UpdateGymIdentity(ctx context.Context, gymID uuid.UUID, input GymIdentity) (AdminGym, error) {
	var color *string
	var group *uuid.UUID
	if input.BrandColor.Set {
		if input.BrandColor.Value == nil || !colorPattern.MatchString(*input.BrandColor.Value) {
			return AdminGym{}, fieldError("brand_color", "BRAND_COLOR_INVALID")
		}
		color = input.BrandColor.Value
	}
	if input.GroupID.Set {
		if input.GroupID.Value == nil || *input.GroupID.Value == uuid.Nil {
			return AdminGym{}, fieldError("group_id", "GROUP_REQUIRED")
		}
		group = input.GroupID.Value
	}
	if color == nil && group == nil {
		return AdminGym{}, fieldError("brand_color", "BRAND_COLOR_INVALID")
	}
	return s.Admin.UpdateGymIdentity(ctx, gymID, color, group)
}

// UpdateGymFeatures switches the optional modules of a gym (the complete list: what is not sent is off). Only the
// superadmin does, from the console. Unknown names are refused; repeats are ignored.
func (s *Service) UpdateGymFeatures(ctx context.Context, gymID uuid.UUID, names []string) (AdminGym, error) {
	features, ok := auth.NormalizeFeatures(names)
	if !ok {
		return AdminGym{}, fieldError("features", "FEATURES_INVALID")
	}
	stored := make([]string, len(features))
	for i, f := range features {
		stored[i] = string(f)
	}
	return s.Admin.UpdateGymFeatures(ctx, gymID, stored)
}

// AddBranch creates a branch in a gym. Only the superadmin does: Atmósfera charges per branch.
func (s *Service) AddBranch(ctx context.Context, gymID uuid.UUID, name string, address *string) (AdminBranch, error) {
	cleanName, err := branchName(name)
	if err != nil {
		return AdminBranch{}, err
	}
	cleanAddress, err := branchAddress(address)
	if err != nil {
		return AdminBranch{}, err
	}
	return s.Admin.AddBranch(ctx, gymID, cleanName, cleanAddress)
}

// UpdateBranch renames a branch or changes its address.
func (s *Service) UpdateBranch(ctx context.Context, id uuid.UUID, changes BranchChanges) (AdminBranch, error) {
	if changes.Name.Set {
		if changes.Name.Value == nil {
			return AdminBranch{}, fieldError("name", "NAME_REQUIRED")
		}
		name, err := branchName(*changes.Name.Value)
		if err != nil {
			return AdminBranch{}, err
		}
		changes.Name.Value = &name
	}
	if changes.Address.Set {
		address, err := branchAddress(changes.Address.Value)
		if err != nil {
			return AdminBranch{}, err
		}
		changes.Address.Value = address
	}
	return s.Admin.UpdateBranch(ctx, id, changes)
}

// DeleteBranch removes a branch nobody has used.
func (s *Service) DeleteBranch(ctx context.Context, id uuid.UUID) error {
	return s.Admin.DeleteBranch(ctx, id)
}
