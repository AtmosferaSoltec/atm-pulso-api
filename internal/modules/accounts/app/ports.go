// Package app has the use cases of the accounts module and the ports they need.
// It depends on the domain and on interfaces it declares itself, never on adapters.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

// Tenancy is the organization and gym a token belongs to.
type Tenancy struct {
	OrganizationID uuid.UUID
	GymID          uuid.UUID
}

// TenancyInput is what a token (or the company list of atm-auth-api) says about the
// customer and the company. The names only seed new records and refresh the existing ones.
type TenancyInput struct {
	ClientID     uuid.UUID
	ClientName   string
	TenantID     uuid.UUID
	TenantName   string
	TenantTaxID  string
	RefreshNames bool
}

// TenancyRepository stores organizations (auth clients) and gyms (auth companies).
type TenancyRepository interface {
	// FindTenancy returns domain.ErrResourceNotFound when the gym is not known yet.
	FindTenancy(ctx context.Context, clientID, tenantID uuid.UUID) (Tenancy, error)
	// EnsureTenancy creates what is missing: the organization, and the gym with its default
	// branch. It returns domain.ErrConflict when the company already belongs to another client.
	EnsureTenancy(ctx context.Context, in TenancyInput) (Tenancy, error)
}

// UserInput is a person as atm-auth-api describes them, for one gym.
type UserInput struct {
	OrganizationID uuid.UUID
	GymID          uuid.UUID
	AuthUserID     uuid.UUID
	DocumentNumber string
	FullName       string
	Role           domain.Role
}

// UserRepository stores the people who work in the gyms.
type UserRepository interface {
	// EnsureUser creates the person for the gym on first sight and keeps the role in step with the token.
	EnsureUser(ctx context.Context, in UserInput) (domain.User, error)
	FindByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	// BranchIDs are the branches a receptionist works in.
	BranchIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	// ScopeOf describes where the person works.
	ScopeOf(ctx context.Context, p auth.Principal) (Scope, error)
	UpdateProfile(ctx context.Context, id uuid.UUID, fullName, phone string, at time.Time) (domain.User, error)
}

// Scope names where a person works, for the web to show.
type Scope struct {
	Organization Named
	Gym          GymSummary
	// Branches are all the branches of the gym for an admin, the assigned ones for reception.
	Branches []BranchRef
}

// Named is an entity with its id and display name.
type Named struct {
	ID   uuid.UUID
	Name string
}

// GymSummary is the current gym with its look.
type GymSummary struct {
	ID         uuid.UUID
	Name       string
	BrandColor string
}

// BranchRef is a branch with the gym it belongs to.
type BranchRef struct {
	ID      uuid.UUID
	Name    string
	GymID   uuid.UUID
	GymName string
}

// DirectoryTenant is a company of atm-auth-api with Pulso enabled.
type DirectoryTenant struct {
	ID         string
	RUC        string
	Name       string
	ClientID   string
	ClientName string
}

// DirectoryMember is a person with access to a company in Pulso.
type DirectoryMember struct {
	UserID         string
	DocumentNumber string
	FullName       string
	Role           string
	Active         bool
}

// NewDirectoryMember is a person to add to a company.
type NewDirectoryMember struct {
	DocumentNumber string
	FullName       string
	Role           string
}

// Directory is atm-auth-api as the accounts module needs it. Its adapter turns failures into
// domain errors (ErrAuthUnavailable, ErrDocumentTaken, ErrRoleNotAssignable, ErrResourceNotFound).
type Directory interface {
	Tenants(ctx context.Context) ([]DirectoryTenant, error)
	// Tenant returns domain.ErrResourceNotFound when the company is unknown.
	Tenant(ctx context.Context, tenantID string) (DirectoryTenant, error)
	Members(ctx context.Context, tenantID string) ([]DirectoryMember, error)
	AddMember(ctx context.Context, tenantID string, in NewDirectoryMember) (DirectoryMember, error)
	ChangeRole(ctx context.Context, tenantID, userID, role string) error
	ResetPassword(ctx context.Context, tenantID, userID string) error
}
