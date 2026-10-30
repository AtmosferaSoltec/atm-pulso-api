package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

// Deps are what the Service needs.
type Deps struct {
	Tenancy   TenancyRepository
	Users     UserRepository
	Gyms      GymRepository
	Team      TeamRepository
	Catalogs  CatalogRepository
	Admin     AdminRepository
	Directory Directory
	Clock     clock.Clock
}

// Service is the accounts use cases: know who asks, see and edit your own profile, manage
// the gym's rules and team, and the superadmin's view of clients and branches.
type Service struct{ Deps }

// NewService builds the service.
func NewService(deps Deps) *Service { return &Service{Deps: deps} }

// Profile is a user with where they work.
type Profile struct {
	User  domain.User
	Scope Scope
}

// Resolve turns the verified claims of atm-auth-api into who asks. Nothing is stored per
// session: the organization, gym and person are found (or created on first sight) from the
// token, so removing access or changing the role in the console shows at the next token.
func (s *Service) Resolve(ctx context.Context, claims auth.Claims) (auth.Principal, error) {
	authUser, errUser := uuid.Parse(claims.Subject)
	tenantID, errTenant := uuid.Parse(claims.Tenant)
	clientID, errClient := uuid.Parse(claims.Client)
	role := domain.Role(claims.AppRole)
	if errUser != nil || errTenant != nil || errClient != nil || !role.Valid() || !domain.ValidDocumentNumber(claims.DNI) {
		return auth.Principal{}, auth.ErrNoAccess
	}

	tenancy, err := s.Tenancy.FindTenancy(ctx, clientID, tenantID)
	if errors.Is(err, domain.ErrResourceNotFound) {
		tenancy, err = s.provision(ctx, claims, clientID, tenantID)
	}
	if err != nil {
		return auth.Principal{}, err
	}

	user, err := s.Users.EnsureUser(ctx, UserInput{
		OrganizationID: tenancy.OrganizationID, GymID: tenancy.GymID, AuthUserID: authUser,
		DocumentNumber: claims.DNI, FullName: claims.Name, Role: role,
	})
	if err != nil {
		return auth.Principal{}, fmt.Errorf("ensure user: %w", err)
	}
	if !user.IsActive {
		return auth.Principal{}, auth.ErrInactive
	}

	principal := auth.Principal{
		UserID: user.ID, OrganizationID: tenancy.OrganizationID, GymID: tenancy.GymID, TenantID: tenantID,
		Role: auth.Role(role),
	}
	if role == domain.RoleReceptionist {
		principal.BranchIDs, err = s.Users.BranchIDs(ctx, user.ID)
		if err != nil {
			return auth.Principal{}, fmt.Errorf("load branches: %w", err)
		}
	}
	return principal, nil
}

// provision records a company seen for the first time. The name of the company comes from
// atm-auth-api when it answers; the RUC of the token is the fallback.
func (s *Service) provision(ctx context.Context, claims auth.Claims, clientID, tenantID uuid.UUID) (Tenancy, error) {
	in := TenancyInput{ClientID: clientID, ClientName: claims.ClientName, TenantID: tenantID, TenantName: claims.TenantRUC, TenantTaxID: claims.TenantRUC}
	if tenant, err := s.Directory.Tenant(ctx, tenantID.String()); err == nil && tenant.Name != "" {
		in.TenantName = tenant.Name
		if tenant.RUC != "" {
			in.TenantTaxID = tenant.RUC
		}
	}
	if in.TenantName == "" {
		in.TenantName = tenantID.String()
	}
	if in.ClientName == "" {
		in.ClientName = in.TenantName
	}
	tenancy, err := s.Tenancy.EnsureTenancy(ctx, in)
	if errors.Is(err, domain.ErrConflict) {
		return Tenancy{}, auth.ErrNoAccess
	}
	if err != nil {
		return Tenancy{}, fmt.Errorf("ensure tenancy: %w", err)
	}
	return tenancy, nil
}

// Me returns the signed-in user's profile.
func (s *Service) Me(ctx context.Context, p auth.Principal) (Profile, error) {
	user, err := s.Users.FindByID(ctx, p.UserID)
	if err != nil {
		return Profile{}, fmt.Errorf("find user: %w", err)
	}
	return s.profileOf(ctx, p, user)
}

func (s *Service) profileOf(ctx context.Context, p auth.Principal, user domain.User) (Profile, error) {
	scope, err := s.Users.ScopeOf(ctx, p)
	if err != nil {
		return Profile{}, fmt.Errorf("load scope: %w", err)
	}
	return Profile{User: user, Scope: scope}, nil
}

// UpdateProfile changes what the user may edit about themselves: name and phone. The DNI and
// the role belong to atm-auth-api and to whoever manages the team.
func (s *Service) UpdateProfile(ctx context.Context, p auth.Principal, fullName, phone string) (Profile, error) {
	cleanName, cleanPhone, err := domain.ProfileChanges(fullName, phone)
	if err != nil {
		return Profile{}, err
	}
	user, err := s.Users.UpdateProfile(ctx, p.UserID, cleanName, cleanPhone, s.Clock.Now())
	if err != nil {
		return Profile{}, fmt.Errorf("update profile: %w", err)
	}
	return s.profileOf(ctx, p, user)
}
