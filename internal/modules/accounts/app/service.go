package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

// Deps are what the Service needs.
type Deps struct {
	Gyms     GymRepository
	Team     TeamRepository
	Clients  ClientRepository
	Catalogs CatalogRepository
	Users    UserRepository
	Hasher   PasswordHasher
	Issuer   TokenIssuer
	Verifier TokenVerifier
	Clock    clock.Clock
}

// Service is the accounts use cases: sign in, authenticate a request, see and edit
// your own profile, change your password.
type Service struct {
	Deps
	attempts *attempts
	// dummyHash is compared against when the DNI does not exist, so an unknown DNI
	// takes as long to answer as a wrong password.
	dummyHash string
}

// NewService builds the service. It hashes a throwaway password once, to have a
// valid hash to compare against for unknown DNIs.
func NewService(deps Deps) (*Service, error) {
	dummy, err := deps.Hasher.Hash("not-a-real-password")
	if err != nil {
		return nil, fmt.Errorf("prepare dummy hash: %w", err)
	}
	return &Service{Deps: deps, attempts: newAttempts(), dummyHash: dummy}, nil
}

// Profile is a user with where they work.
type Profile struct {
	User  domain.User
	Scope Scope
}

// Session is the result of signing in or changing the password.
type Session struct {
	Token         Token
	Profile       Profile
	LoginRequired bool
}

func loginKey(documentNumber string) string { return "login:" + documentNumber }
func passwordKey(id uuid.UUID) string       { return "password:" + id.String() }

// Login checks the DNI and password and issues an access token. Nothing is saved:
// no session row, no refresh token. Wrong DNI, wrong password and inactive user all
// answer domain.ErrInvalidCredentials.
func (s *Service) Login(ctx context.Context, documentNumber, password string) (Session, error) {
	if err := domain.LoginInput(documentNumber, password); err != nil {
		return Session{}, err
	}
	now := s.Clock.Now()
	key := loginKey(documentNumber)
	if wait := s.attempts.lockedFor(key, now); wait > 0 {
		return Session{}, &domain.LockedError{RetryAfter: wait}
	}

	creds, err := s.Users.FindCredentialsByDocument(ctx, documentNumber)
	if errors.Is(err, domain.ErrUserNotFound) {
		_, _ = s.Hasher.Verify(password, s.dummyHash)
		s.attempts.fail(key, now)
		return Session{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, fmt.Errorf("find user: %w", err)
	}

	matches, err := s.Hasher.Verify(password, creds.PasswordHash)
	if err != nil {
		return Session{}, fmt.Errorf("verify password: %w", err)
	}
	if !matches || !creds.IsActive {
		s.attempts.fail(key, now)
		return Session{}, domain.ErrInvalidCredentials
	}
	s.attempts.reset(key)
	return s.session(ctx, creds.User, now)
}

func (s *Service) session(ctx context.Context, user domain.User, now time.Time) (Session, error) {
	token, err := s.Issuer.Issue(user.ID, user.PasswordVersion, now)
	if err != nil {
		return Session{}, fmt.Errorf("issue token: %w", err)
	}
	profile, err := s.profileOf(ctx, user)
	if err != nil {
		return Session{}, err
	}
	return Session{Token: token, Profile: profile}, nil
}

func (s *Service) profileOf(ctx context.Context, user domain.User) (Profile, error) {
	scope, err := s.Users.ScopeOf(ctx, user)
	if err != nil {
		return Profile{}, fmt.Errorf("load scope: %w", err)
	}
	return Profile{User: user, Scope: scope}, nil
}

// Authenticate verifies an access token and rebuilds who it belongs to from the
// database. That live lookup is what makes deactivating a user, changing a role or
// changing a password effective at once, with no session table.
func (s *Service) Authenticate(ctx context.Context, accessToken string) (auth.Principal, error) {
	claims, err := s.Verifier.Verify(accessToken, s.Clock.Now())
	if err != nil {
		return auth.Principal{}, auth.ErrUnauthorized
	}
	user, err := s.Users.FindByID(ctx, claims.UserID)
	if errors.Is(err, domain.ErrUserNotFound) {
		return auth.Principal{}, auth.ErrUnauthorized
	}
	if err != nil {
		return auth.Principal{}, fmt.Errorf("find user: %w", err)
	}
	if !user.IsActive || claims.PasswordVersion != user.PasswordVersion {
		return auth.Principal{}, auth.ErrUnauthorized
	}

	principal := auth.Principal{
		UserID:             user.ID,
		OrganizationID:     user.OrganizationID,
		Role:               auth.Role(user.Role),
		GymID:              user.GymID,
		MustChangePassword: user.MustChangePassword,
	}
	if user.Role == domain.RoleReceptionist {
		principal.BranchIDs, err = s.Users.BranchIDs(ctx, user.ID)
		if err != nil {
			return auth.Principal{}, fmt.Errorf("load branches: %w", err)
		}
	}
	return principal, nil
}

// Me returns the signed-in user's profile.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (Profile, error) {
	user, err := s.Users.FindByID(ctx, userID)
	if err != nil {
		return Profile{}, fmt.Errorf("find user: %w", err)
	}
	return s.profileOf(ctx, user)
}

// UpdateProfile changes the non-sensitive data the user may edit: name and phone.
// The DNI, role and scope are only changed by whoever manages the team.
func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, fullName, phone string) (Profile, error) {
	cleanName, cleanPhone, err := domain.ProfileChanges(fullName, phone)
	if err != nil {
		return Profile{}, err
	}
	user, err := s.Users.UpdateProfile(ctx, userID, cleanName, cleanPhone, s.Clock.Now())
	if err != nil {
		return Profile{}, fmt.Errorf("update profile: %w", err)
	}
	return s.profileOf(ctx, user)
}

// ChangePassword replaces the password after checking the current one. It signs out
// every older token. Required changes demand a new login; voluntary changes issue a fresh token.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, current, next string) (Session, error) {
	now := s.Clock.Now()
	key := passwordKey(userID)
	if wait := s.attempts.lockedFor(key, now); wait > 0 {
		return Session{}, &domain.LockedError{RetryAfter: wait}
	}

	creds, err := s.Users.FindCredentialsByID(ctx, userID)
	if err != nil {
		return Session{}, fmt.Errorf("find user: %w", err)
	}
	matches, err := s.Hasher.Verify(current, creds.PasswordHash)
	if err != nil {
		return Session{}, fmt.Errorf("verify password: %w", err)
	}
	if !matches {
		s.attempts.fail(key, now)
		return Session{}, domain.ErrCurrentPasswordIncorrect
	}
	s.attempts.reset(key)

	if err := domain.NewPassword(current, next); err != nil {
		return Session{}, err
	}
	hash, err := s.Hasher.Hash(next)
	if err != nil {
		return Session{}, fmt.Errorf("hash password: %w", err)
	}
	version, err := s.Users.UpdatePassword(ctx, userID, creds.PasswordVersion, hash, now)
	if err != nil {
		return Session{}, fmt.Errorf("update password: %w", err)
	}
	if creds.MustChangePassword {
		return Session{LoginRequired: true}, nil
	}
	creds.PasswordVersion = version
	return s.session(ctx, creds.User, now)
}
