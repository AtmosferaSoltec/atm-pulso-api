// Package auth carries who makes the request in the context. Identity belongs to
// atm-auth-api: this package only checks its tokens and keeps the result. The user, the
// group, the gym and the role come from the verified token and the local records
// built from it, never from a body, URL or query.
package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
)

// Role is what a person is allowed to be in Pulso. It is the `app_role` atm-auth-api puts in the token.
type Role string

// The two roles of Pulso.
const (
	// RoleAdmin manages the current gym: its team, prices and rules; reaches all of its branches.
	RoleAdmin Role = "admin"
	// RoleReceptionist works the branches listed in Principal.BranchIDs.
	RoleReceptionist Role = "receptionist"
)

// Valid says whether r is one of the two roles.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleReceptionist }

// Principal is the authenticated person, rebuilt on every request, so a deactivated user or
// a changed role takes effect at once.
type Principal struct {
	UserID uuid.UUID
	// GroupID is the internal group the gym belongs to. Informational: it is never a query filter.
	GroupID uuid.UUID
	// GymID is the company the token was issued for and the tenant of every business query. Switching gym means asking auth for a token of another one.
	GymID uuid.UUID
	// TenantID is that same company in atm-auth-api.
	TenantID uuid.UUID
	Role     Role
	// BranchIDs are the branches a receptionist works in; empty for an admin.
	BranchIDs []uuid.UUID
}

// BranchRef identifies a branch and the gym it belongs to.
type BranchRef struct {
	ID    uuid.UUID
	GymID uuid.UUID
}

// CanAccessBranch says whether the person may work in a branch. An admin reaches every
// branch of the current gym, reception only the assigned ones. Isolation between gyms
// is enforced separately: every query filters by gym_id.
func (p Principal) CanAccessBranch(branch BranchRef) bool {
	switch p.Role {
	case RoleAdmin:
		return branch.GymID == p.GymID
	case RoleReceptionist:
		return slices.Contains(p.BranchIDs, branch.ID)
	default:
		return false
	}
}

// CanManageGym says whether the person manages a gym's rules and prices.
func (p Principal) CanManageGym(gymID uuid.UUID) bool {
	return p.Role == RoleAdmin && gymID == p.GymID
}

// CanManageTeam says whether the person may create and edit other users.
func (p Principal) CanManageTeam() bool { return p.Role == RoleAdmin }

var (
	// ErrUnauthorized: missing, invalid or expired token.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrNoAccess: a good token that does not open Pulso (another system, an unknown role, no company).
	ErrNoAccess = errors.New("no access")
	// ErrInactive: the person was deactivated in Pulso.
	ErrInactive = errors.New("user inactive")
)

// Codes of the guard, besides the shared ones.
const (
	CodeNoAccess     = "NO_ACCESS"
	CodeUserInactive = "USER_INACTIVE"
)

var (
	errNoAccess     = apierror.New(http.StatusForbidden, CodeNoAccess, "Tu cuenta no tiene acceso a Pulso. Contacta a Atmósfera.")
	errUserInactive = apierror.New(http.StatusForbidden, CodeUserInactive, "Tu usuario está desactivado. Habla con el administrador.")
)

// Resolver turns verified claims into who is asking. The accounts module implements it.
type Resolver interface {
	// Resolve returns ErrNoAccess, ErrInactive, or an unexpected error.
	Resolve(ctx context.Context, claims Claims) (Principal, error)
}

type principalKey struct{}
type claimsKey struct{}

// WithPrincipal stores who makes the request.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns who makes the request. It only exists on protected routes.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// ClaimsFromContext returns the verified token of the request (user or superadmin).
func ClaimsFromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(Claims)
	return c, ok
}

// Guard protects routes with atm-auth-api tokens.
type Guard struct {
	verifier TokenVerifier
	resolver Resolver
	system   string
}

// NewGuard returns a guard for the system code of Pulso in atm-auth-api.
func NewGuard(verifier TokenVerifier, resolver Resolver, system string) *Guard {
	return &Guard{verifier: verifier, resolver: resolver, system: system}
}

func (g *Guard) claims(c *gin.Context) (Claims, bool) {
	token, ok := bearerToken(c.GetHeader("Authorization"))
	if !ok {
		apierror.Respond(c, apierror.ErrUnauthorized)
		return Claims{}, false
	}
	claims, err := g.verifier.Verify(c.Request.Context(), token)
	if err != nil {
		apierror.Respond(c, apierror.ErrUnauthorized)
		return Claims{}, false
	}
	return claims, true
}

// User demands the token of a person who works for a company in Pulso. Everything mounted
// behind it is private: forgetting something leaves a route protected, not open.
func (g *Guard) User() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := g.claims(c)
		if !ok {
			return
		}
		if claims.Role == RoleClaimSuperadmin {
			apierror.Respond(c, apierror.ErrForbidden)
			return
		}
		if claims.Role != RoleClaimUser || claims.System != g.system || claims.Tenant == "" || claims.Client == "" {
			apierror.Respond(c, errNoAccess)
			return
		}
		principal, err := g.resolver.Resolve(c.Request.Context(), claims)
		switch {
		case errors.Is(err, ErrNoAccess):
			apierror.Respond(c, errNoAccess)
			return
		case errors.Is(err, ErrInactive):
			apierror.Respond(c, errUserInactive)
			return
		case err != nil:
			apierror.Respond(c, err)
			return
		}
		ctx := WithPrincipal(WithClaims(c.Request.Context(), claims), principal)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// Superadmin demands the token of the platform administrator (atm-auth-api's superadmin,
// who has no system and no company). A person's token is refused.
func (g *Guard) Superadmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, ok := g.claims(c)
		if !ok {
			return
		}
		if claims.Role != RoleClaimSuperadmin {
			apierror.Respond(c, apierror.ErrForbidden)
			return
		}
		c.Request = c.Request.WithContext(WithClaims(c.Request.Context(), claims))
		c.Next()
	}
}

// WithClaims stores the verified token.
func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", false
	}
	return strings.TrimSpace(token), true
}
