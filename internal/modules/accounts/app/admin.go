package app

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/logger"
)

// AdminBranch is a branch as the superadmin sees it.
type AdminBranch struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Address *string   `json:"address"`
}

// AdminGym is a company of a client with Pulso enabled.
type AdminGym struct {
	ID           uuid.UUID     `json:"id"`
	AuthTenantID uuid.UUID     `json:"auth_tenant_id"`
	RUC          *string       `json:"ruc"`
	Name         string        `json:"name"`
	Branches     []AdminBranch `json:"branches"`
}

// AdminClient is a customer (an organization) with its gyms.
type AdminClient struct {
	ID           uuid.UUID  `json:"id"`
	AuthClientID uuid.UUID  `json:"auth_client_id"`
	Name         string     `json:"name"`
	Gyms         []AdminGym `json:"gyms"`
}

// BranchChanges edits a branch.
type BranchChanges struct {
	Name    Optional[string] `json:"name"`
	Address Optional[string] `json:"address"`
}

// AdminRepository is what the superadmin's view needs.
type AdminRepository interface {
	Clients(ctx context.Context) ([]AdminClient, error)
	// AddBranch returns domain.ErrResourceNotFound when the gym does not exist.
	AddBranch(ctx context.Context, gymID uuid.UUID, name string, address *string) (AdminBranch, error)
	UpdateBranch(ctx context.Context, id uuid.UUID, changes BranchChanges) (AdminBranch, error)
	// DeleteBranch returns domain.ErrBranchInUse when the branch has history or people, or is the last of its gym.
	DeleteBranch(ctx context.Context, id uuid.UUID) error
}

const maxBranchName = 80

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

// SyncClients records every company that has Pulso enabled in atm-auth-api: its client as an
// organization, itself as a gym with a default branch. When atm-auth-api cannot be reached the
// superadmin still sees what is already stored.
func (s *Service) SyncClients(ctx context.Context) {
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

// ListClients is the superadmin's view: every client with its gyms and branches.
func (s *Service) ListClients(ctx context.Context) ([]AdminClient, error) {
	s.SyncClients(ctx)
	return s.Admin.Clients(ctx)
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
