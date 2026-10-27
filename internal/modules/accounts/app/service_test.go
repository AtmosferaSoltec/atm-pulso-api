package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

var start = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)

// testClock is a clock the test moves by hand.
type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time          { return c.now }
func (c *testClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// fakeHasher is instant and transparent; the real argon2id has its own tests.
type fakeHasher struct{ verifications int }

func (*fakeHasher) Hash(password string) (string, error) { return "hash:" + password, nil }
func (h *fakeHasher) Verify(password, encoded string) (bool, error) {
	h.verifications++
	return encoded == "hash:"+password, nil
}

// fakeTokens issues "userID|password version" and understands it back.
type fakeTokens struct{ ttl time.Duration }

func (f fakeTokens) Issue(userID uuid.UUID, passwordVersion int, now time.Time) (Token, error) {
	return Token{Value: fmt.Sprintf("%s|%d", userID, passwordVersion), ExpiresAt: now.Add(f.ttl)}, nil
}

func (fakeTokens) Verify(token string, _ time.Time) (TokenClaims, error) {
	id, version, found := strings.Cut(token, "|")
	if !found {
		return TokenClaims{}, errors.New("bad token")
	}
	userID, err := uuid.Parse(id)
	if err != nil {
		return TokenClaims{}, err
	}
	var passwordVersion int
	if _, err := fmt.Sscanf(version, "%d", &passwordVersion); err != nil {
		return TokenClaims{}, err
	}
	return TokenClaims{UserID: userID, PasswordVersion: passwordVersion}, nil
}

// fakeUsers keeps users in memory.
type fakeUsers struct {
	byID     map[uuid.UUID]domain.Credentials
	branches map[uuid.UUID][]uuid.UUID
}

func newFakeUsers(users ...domain.Credentials) *fakeUsers {
	f := &fakeUsers{byID: map[uuid.UUID]domain.Credentials{}, branches: map[uuid.UUID][]uuid.UUID{}}
	for _, u := range users {
		f.byID[u.ID] = u
	}
	return f
}

func (f *fakeUsers) FindCredentialsByDocument(_ context.Context, document string) (domain.Credentials, error) {
	for _, u := range f.byID {
		if u.DocumentNumber == document {
			return u, nil
		}
	}
	return domain.Credentials{}, domain.ErrUserNotFound
}

func (f *fakeUsers) FindCredentialsByID(_ context.Context, id uuid.UUID) (domain.Credentials, error) {
	if u, ok := f.byID[id]; ok {
		return u, nil
	}
	return domain.Credentials{}, domain.ErrUserNotFound
}

func (f *fakeUsers) FindByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	u, err := f.FindCredentialsByID(ctx, id)
	return u.User, err
}

func (f *fakeUsers) BranchIDs(_ context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	return f.branches[id], nil
}

func (f *fakeUsers) ScopeOf(_ context.Context, u domain.User) (Scope, error) {
	scope := Scope{}
	if u.OrganizationID != uuid.Nil {
		scope.Organization = &Named{ID: u.OrganizationID, Name: "Grupo Atlas"}
	}
	return scope, nil
}

func (f *fakeUsers) UpdateProfile(_ context.Context, id uuid.UUID, name, phone string, at time.Time) (domain.User, error) {
	u, ok := f.byID[id]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	u.FullName, u.Phone = name, phone
	f.byID[id] = u
	return u.User, nil
}

func (f *fakeUsers) UpdatePassword(_ context.Context, id uuid.UUID, hash string, _ time.Time) (int, error) {
	u, ok := f.byID[id]
	if !ok {
		return 0, domain.ErrUserNotFound
	}
	u.PasswordHash = hash
	u.PasswordVersion++
	f.byID[id] = u
	return u.PasswordVersion, nil
}

type harness struct {
	service *Service
	users   *fakeUsers
	hasher  *fakeHasher
	clock   *testClock
	tokens  fakeTokens
}

const password = "Pulso2026!"

func newHarness(t *testing.T, users ...domain.Credentials) harness {
	t.Helper()
	h := harness{users: newFakeUsers(users...), hasher: &fakeHasher{}, clock: &testClock{now: start}, tokens: fakeTokens{ttl: 8 * time.Hour}}
	service, err := NewService(Deps{Users: h.users, Hasher: h.hasher, Issuer: h.tokens, Verifier: h.tokens, Clock: h.clock})
	require.NoError(t, err)
	h.service = service
	return h
}

func credentials(role domain.Role, document string) domain.Credentials {
	u := domain.User{ID: uuid.New(), DocumentNumber: document, FullName: "Andrea Salas", Role: role, IsActive: true, PasswordVersion: 1}
	if role != domain.RoleSuperadmin {
		u.OrganizationID = uuid.New()
	}
	if role == domain.RoleAdmin {
		u.GymID = uuid.New()
	}
	return domain.Credentials{User: u, PasswordHash: "hash:" + password}
}

func TestLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("signs in and returns a token with the profile", func(t *testing.T) {
		owner := credentials(domain.RoleOwner, "10000002")
		h := newHarness(t, owner)
		session, err := h.service.Login(ctx, "10000002", password)
		require.NoError(t, err)
		assert.Equal(t, owner.ID, session.Profile.User.ID)
		assert.Equal(t, "Grupo Atlas", session.Profile.Scope.Organization.Name)
		assert.Equal(t, start.Add(8*time.Hour), session.Token.ExpiresAt)
		assert.NotEmpty(t, session.Token.Value)
	})

	t.Run("a malformed request is a validation error, not a failed attempt", func(t *testing.T) {
		h := newHarness(t)
		_, err := h.service.Login(ctx, "abc", "")
		var validation *domain.ValidationError
		assert.True(t, errors.As(err, &validation))
		assert.Zero(t, h.hasher.verifications)
	})

	t.Run("unknown DNI looks exactly like a wrong password and still costs a hash check", func(t *testing.T) {
		h := newHarness(t, credentials(domain.RoleOwner, "10000002"))
		_, err := h.service.Login(ctx, "99999999", password)
		assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
		assert.Equal(t, 1, h.hasher.verifications, "the dummy hash equalizes timing")
	})

	t.Run("wrong password", func(t *testing.T) {
		h := newHarness(t, credentials(domain.RoleOwner, "10000002"))
		_, err := h.service.Login(ctx, "10000002", "wrong-password")
		assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
	})

	t.Run("an inactive user cannot sign in even with the right password", func(t *testing.T) {
		inactive := credentials(domain.RoleReceptionist, "10000003")
		inactive.IsActive = false
		h := newHarness(t, inactive)
		_, err := h.service.Login(ctx, "10000003", password)
		assert.ErrorIs(t, err, domain.ErrInvalidCredentials, "no hint that the account exists")
	})

	t.Run("five failures lock the DNI for fifteen minutes, then it opens again", func(t *testing.T) {
		h := newHarness(t, credentials(domain.RoleOwner, "10000002"))
		for range maxFailures {
			_, err := h.service.Login(ctx, "10000002", "wrong-password")
			require.ErrorIs(t, err, domain.ErrInvalidCredentials)
		}
		_, err := h.service.Login(ctx, "10000002", password)
		var locked *domain.LockedError
		require.True(t, errors.As(err, &locked), "even the right password is refused while locked")
		assert.Equal(t, attemptWindow, locked.RetryAfter)

		h.clock.advance(attemptWindow)
		_, err = h.service.Login(ctx, "10000002", password)
		assert.NoError(t, err)
	})

	t.Run("a success clears earlier failures", func(t *testing.T) {
		h := newHarness(t, credentials(domain.RoleOwner, "10000002"))
		for range maxFailures - 1 {
			_, _ = h.service.Login(ctx, "10000002", "wrong-password")
		}
		_, err := h.service.Login(ctx, "10000002", password)
		require.NoError(t, err)
		for range maxFailures - 1 {
			_, err = h.service.Login(ctx, "10000002", "wrong-password")
			require.ErrorIs(t, err, domain.ErrInvalidCredentials)
		}
	})

	t.Run("one DNI's failures do not lock another", func(t *testing.T) {
		h := newHarness(t, credentials(domain.RoleOwner, "10000002"), credentials(domain.RoleOwner, "10000004"))
		for range maxFailures {
			_, _ = h.service.Login(ctx, "10000002", "wrong-password")
		}
		_, err := h.service.Login(ctx, "10000004", password)
		assert.NoError(t, err)
	})
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()

	t.Run("builds the principal of each role from the live record", func(t *testing.T) {
		owner := credentials(domain.RoleOwner, "10000002")
		admin := credentials(domain.RoleAdmin, "10000005")
		reception := credentials(domain.RoleReceptionist, "10000003")
		superadmin := credentials(domain.RoleSuperadmin, "10000001")
		h := newHarness(t, owner, admin, reception, superadmin)
		miraflores, barranco := uuid.New(), uuid.New()
		h.users.branches[reception.ID] = []uuid.UUID{miraflores, barranco}

		token := func(c domain.Credentials) string {
			issued, err := h.tokens.Issue(c.ID, c.PasswordVersion, start)
			require.NoError(t, err)
			return issued.Value
		}

		p, err := h.service.Authenticate(ctx, token(owner))
		require.NoError(t, err)
		assert.Equal(t, auth.Principal{UserID: owner.ID, OrganizationID: owner.OrganizationID, Role: auth.RoleOwner}, p)

		p, err = h.service.Authenticate(ctx, token(admin))
		require.NoError(t, err)
		assert.Equal(t, auth.RoleAdmin, p.Role)
		assert.Equal(t, admin.GymID, p.GymID)

		p, err = h.service.Authenticate(ctx, token(reception))
		require.NoError(t, err)
		assert.Equal(t, []uuid.UUID{miraflores, barranco}, p.BranchIDs)

		p, err = h.service.Authenticate(ctx, token(superadmin))
		require.NoError(t, err)
		assert.Equal(t, uuid.Nil, p.OrganizationID)
		assert.False(t, p.CanOperate())
	})

	t.Run("rejects garbage, unknown users and inactive users", func(t *testing.T) {
		inactive := credentials(domain.RoleReceptionist, "10000003")
		inactive.IsActive = false
		h := newHarness(t, inactive)
		_, err := h.service.Authenticate(ctx, "garbage")
		assert.ErrorIs(t, err, auth.ErrUnauthorized)

		stranger, _ := h.tokens.Issue(uuid.New(), 1, start)
		_, err = h.service.Authenticate(ctx, stranger.Value)
		assert.ErrorIs(t, err, auth.ErrUnauthorized)

		token, _ := h.tokens.Issue(inactive.ID, 1, start)
		_, err = h.service.Authenticate(ctx, token.Value)
		assert.ErrorIs(t, err, auth.ErrUnauthorized, "deactivating a user takes effect at once")
	})

	t.Run("a password change signs out older tokens but not the new one, even in the same second", func(t *testing.T) {
		owner := credentials(domain.RoleOwner, "10000002")
		h := newHarness(t, owner)
		old, err := h.service.Login(ctx, "10000002", password)
		require.NoError(t, err)

		changed, err := h.service.ChangePassword(ctx, owner.ID, password, "NuevaClave123")
		require.NoError(t, err)

		_, err = h.service.Authenticate(ctx, old.Token.Value)
		assert.ErrorIs(t, err, auth.ErrUnauthorized, "the token from before the change is dead")
		_, err = h.service.Authenticate(ctx, changed.Token.Value)
		assert.NoError(t, err, "the token issued by the change keeps working")
	})
}

func TestProfile(t *testing.T) {
	ctx := context.Background()
	reception := credentials(domain.RoleReceptionist, "10000003")
	h := newHarness(t, reception)

	t.Run("updates name and phone, cleaned", func(t *testing.T) {
		profile, err := h.service.UpdateProfile(ctx, reception.ID, "  Luis   Rojas  ", "987654321")
		require.NoError(t, err)
		assert.Equal(t, "Luis Rojas", profile.User.FullName)
		assert.Equal(t, "987654321", profile.User.Phone)
		me, err := h.service.Me(ctx, reception.ID)
		require.NoError(t, err)
		assert.Equal(t, "Luis Rojas", me.User.FullName)
	})

	t.Run("rejects invalid data and changes nothing", func(t *testing.T) {
		_, err := h.service.UpdateProfile(ctx, reception.ID, "x", "123")
		var validation *domain.ValidationError
		require.True(t, errors.As(err, &validation))
		assert.Len(t, validation.Issues, 2)
		me, _ := h.service.Me(ctx, reception.ID)
		assert.Equal(t, "Luis Rojas", me.User.FullName)
	})
}

func TestChangePassword(t *testing.T) {
	ctx := context.Background()

	t.Run("wrong current password, and repeated failures lock it", func(t *testing.T) {
		owner := credentials(domain.RoleOwner, "10000002")
		h := newHarness(t, owner)
		for range maxFailures {
			_, err := h.service.ChangePassword(ctx, owner.ID, "not-it", "NuevaClave123")
			require.ErrorIs(t, err, domain.ErrCurrentPasswordIncorrect)
		}
		_, err := h.service.ChangePassword(ctx, owner.ID, password, "NuevaClave123")
		var locked *domain.LockedError
		assert.True(t, errors.As(err, &locked), "a stolen token cannot be used to guess the password")
	})

	t.Run("a weak or unchanged new password is refused without touching the stored hash", func(t *testing.T) {
		owner := credentials(domain.RoleOwner, "10000002")
		h := newHarness(t, owner)
		for _, next := range []string{"corta", password} {
			_, err := h.service.ChangePassword(ctx, owner.ID, password, next)
			var validation *domain.ValidationError
			assert.True(t, errors.As(err, &validation), next)
		}
		assert.Equal(t, "hash:"+password, h.users.byID[owner.ID].PasswordHash)
	})

	t.Run("stores the new hash and the sign-in works with it", func(t *testing.T) {
		owner := credentials(domain.RoleOwner, "10000002")
		h := newHarness(t, owner)
		_, err := h.service.ChangePassword(ctx, owner.ID, password, "NuevaClave123")
		require.NoError(t, err)
		_, err = h.service.Login(ctx, "10000002", password)
		assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
		_, err = h.service.Login(ctx, "10000002", "NuevaClave123")
		assert.NoError(t, err)
	})
}
