// Package auth carries who makes the request in the context. The user id, the
// organization and the role come only from here (the verified token and the live
// user record): no module accepts them from a body or a URL.
package auth

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/apierror"
)

// Role is what a user is allowed to be. It mirrors the front's permissions.ts.
type Role string

// The four roles of Pulso.
const (
	// RoleSuperadmin is Atmósfera's staff: creates clients, only consults them.
	RoleSuperadmin Role = "superadmin"
	// RoleOwner sees and manages every gym and branch of its organization.
	RoleOwner Role = "owner"
	// RoleAdmin manages one gym (Principal.GymID): its branches, reception users and prices.
	RoleAdmin Role = "admin"
	// RoleReceptionist works the branches listed in Principal.BranchIDs.
	RoleReceptionist Role = "receptionist"
)

// Principal is the authenticated person. It is rebuilt from the database on every
// request, so a deactivated user or a changed role takes effect immediately.
type Principal struct {
	UserID uuid.UUID
	// OrganizationID is uuid.Nil for the superadmin, who belongs to no client.
	OrganizationID uuid.UUID
	Role           Role
	// GymID is the gym an admin manages; uuid.Nil for the other roles.
	GymID uuid.UUID
	// BranchIDs are the branches a receptionist works in; empty for the other roles.
	BranchIDs []uuid.UUID
}

// BranchRef identifies a branch and the gym it belongs to.
type BranchRef struct {
	ID    uuid.UUID
	GymID uuid.UUID
}

// CanOperate says whether the user may change data. The superadmin only consults.
func (p Principal) CanOperate() bool { return p.Role != RoleSuperadmin }

// CanAccessBranch says whether the user may work in a branch. Owners reach every
// branch of their organization, admins those of their gym, reception the assigned
// ones. Isolation between organizations is enforced separately: every query
// filters by organization_id.
func (p Principal) CanAccessBranch(branch BranchRef) bool {
	switch p.Role {
	case RoleSuperadmin, RoleOwner:
		return true
	case RoleAdmin:
		return branch.GymID == p.GymID
	case RoleReceptionist:
		return slices.Contains(p.BranchIDs, branch.ID)
	default:
		return false
	}
}

// CanManageGym says whether the user manages a gym's rules, prices and identity.
func (p Principal) CanManageGym(gymID uuid.UUID) bool {
	switch p.Role {
	case RoleOwner:
		return true
	case RoleAdmin:
		return p.GymID == gymID
	default:
		return false
	}
}

// CanManageTeam says whether the user may create and edit other users.
func (p Principal) CanManageTeam() bool {
	return p.Role == RoleOwner || p.Role == RoleAdmin
}

// ErrUnauthorized: missing, invalid or expired token, or an inactive user.
var ErrUnauthorized = errors.New("unauthorized")

// Authenticator validates an access token and returns who it belongs to. The
// accounts module implements it.
type Authenticator interface {
	Authenticate(ctx context.Context, accessToken string) (Principal, error)
}

type contextKey struct{}

// WithPrincipal stores who makes the request.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, p)
}

// FromContext returns who makes the request. It only exists on protected routes.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(contextKey{}).(Principal)
	return p, ok
}

// Require demands "Authorization: Bearer <access token>". Everything mounted
// behind it is private: forgetting something leaves a route protected, not open.
func Require(authenticator Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			apierror.Respond(c, apierror.ErrUnauthorized)
			return
		}
		principal, err := authenticator.Authenticate(c.Request.Context(), token)
		if errors.Is(err, ErrUnauthorized) {
			apierror.Respond(c, apierror.ErrUnauthorized)
			return
		}
		if err != nil {
			apierror.Respond(c, err)
			return
		}
		c.Request = c.Request.WithContext(WithPrincipal(c.Request.Context(), principal))
		c.Next()
	}
}

func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", false
	}
	return strings.TrimSpace(token), true
}
