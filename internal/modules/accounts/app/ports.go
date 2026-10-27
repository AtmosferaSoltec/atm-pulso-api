// Package app has the use cases of the accounts module and the ports they need.
// It depends on the domain and on interfaces it declares itself, never on adapters.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
)

// UserRepository stores users. Every method that takes an organization scope uses it
// to keep one client's data away from another's.
type UserRepository interface {
	// FindCredentialsByDocument returns domain.ErrUserNotFound when no user has that DNI.
	FindCredentialsByDocument(ctx context.Context, documentNumber string) (domain.Credentials, error)
	FindCredentialsByID(ctx context.Context, id uuid.UUID) (domain.Credentials, error)
	FindByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	// BranchIDs are the branches a receptionist works in.
	BranchIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	// ScopeOf describes the organization, gym and branches the user belongs to.
	ScopeOf(ctx context.Context, user domain.User) (Scope, error)
	UpdateProfile(ctx context.Context, id uuid.UUID, fullName, phone string, at time.Time) (domain.User, error)
	// UpdatePassword stores the new hash, bumps the password version and returns it.
	UpdatePassword(ctx context.Context, id uuid.UUID, expectedVersion int, passwordHash string, at time.Time) (int, error)
}

// PasswordHasher hashes and verifies passwords.
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Verify returns false (and no error) when the password simply does not match.
	Verify(password, encodedHash string) (bool, error)
}

// Token is an access token and the instant it stops working.
type Token struct {
	Value     string
	ExpiresAt time.Time
}

// TokenClaims is what a verified token says.
type TokenClaims struct {
	UserID uuid.UUID
	// PasswordVersion must still match the user's, or the token is dead.
	PasswordVersion int
}

// TokenIssuer creates access tokens.
type TokenIssuer interface {
	Issue(userID uuid.UUID, passwordVersion int, now time.Time) (Token, error)
}

// TokenVerifier checks an access token. Any error means the token must not be trusted
// (signature, algorithm, expiry, issuer...).
type TokenVerifier interface {
	Verify(token string, now time.Time) (TokenClaims, error)
}

// Scope names where a user works, for the web to show.
type Scope struct {
	Organization *Named
	Gym          *Named
	// Branches is only filled for a receptionist.
	Branches []BranchRef
}

// Named is an entity with its id and display name.
type Named struct {
	ID   uuid.UUID
	Name string
}

// BranchRef is a branch with the gym it belongs to.
type BranchRef struct {
	ID      uuid.UUID
	Name    string
	GymID   uuid.UUID
	GymName string
}
