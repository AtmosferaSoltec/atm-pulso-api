package gymlogo

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/storageapi"
)

type rig struct {
	auth    *authapi.Fake
	storage *storageapi.Fake
	r       *Resolver
	clock   *time.Time
}

func newRig() *rig {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	g := &rig{auth: authapi.NewFake(), storage: storageapi.NewFake(), clock: &now}
	g.r = New(g.auth, g.storage, slog.New(slog.NewTextHandler(io.Discard, nil)))
	g.r.now = func() time.Time { return *g.clock }
	return g
}

func (g *rig) gym(logoURL string, expires time.Duration) uuid.UUID {
	tenant, file := uuid.New(), uuid.NewString()
	g.auth.SetTenant(authapi.Tenant{ID: tenant.String(), LogoFileID: &file})
	g.storage.SetLink(file, logoURL, g.clock.Add(expires))
	return tenant
}

func TestResolvesTheLogoOfTheGym(t *testing.T) {
	g := newRig()
	one, two := g.gym("https://f/one", time.Hour), g.gym("https://f/two", time.Hour)
	require.NotNil(t, g.r.LogoURL(t.Context(), one))
	assert.Equal(t, "https://f/one", *g.r.LogoURL(t.Context(), one))
	assert.Equal(t, "https://f/two", *g.r.LogoURL(t.Context(), two), "each gym gets only its own logo")
}

func TestNoLogoUnknownGymAndNilResolver(t *testing.T) {
	g := newRig()
	plain := uuid.New()
	g.auth.SetTenant(authapi.Tenant{ID: plain.String()})
	assert.Nil(t, g.r.LogoURL(t.Context(), plain))
	assert.Nil(t, g.r.LogoURL(t.Context(), uuid.New()), "a company the system does not serve")
	assert.Nil(t, g.r.LogoURL(t.Context(), uuid.Nil))
	var none *Resolver
	assert.Nil(t, none.LogoURL(t.Context(), plain))
}

func TestAnAnswerIsCachedBrieflyAndThenRefreshed(t *testing.T) {
	g := newRig()
	gym := g.gym("https://f/a", time.Hour)
	g.r.LogoURL(t.Context(), gym)
	g.r.LogoURL(t.Context(), gym)
	assert.Equal(t, 1, g.storage.Calls, "second call served from the cache")
	*g.clock = g.clock.Add(DefaultTTL + time.Second)
	g.r.LogoURL(t.Context(), gym)
	assert.Equal(t, 2, g.storage.Calls)
}

func TestALinkAboutToExpireIsNeverReused(t *testing.T) {
	g := newRig()
	gym := g.gym("https://f/short", 90*time.Second) // usable for 30 s only
	g.r.LogoURL(t.Context(), gym)
	*g.clock = g.clock.Add(31 * time.Second)
	g.r.LogoURL(t.Context(), gym)
	assert.Equal(t, 2, g.storage.Calls)
}

func TestOutagesDegradeToNilAndRecover(t *testing.T) {
	g := newRig()
	gym := g.gym("https://f/a", time.Hour)

	g.storage.Err = storageapi.ErrUnavailable
	assert.Nil(t, g.r.LogoURL(t.Context(), gym))
	assert.Nil(t, g.r.LogoURL(t.Context(), gym))
	assert.Equal(t, 1, g.storage.Calls, "a failure is remembered so an outage does not slow every request")

	g.storage.Err = nil
	*g.clock = g.clock.Add(DefaultFailureTTL + time.Second)
	got := g.r.LogoURL(t.Context(), gym)
	require.NotNil(t, got)
	assert.Equal(t, "https://f/a", *got)

	other := g.gym("https://f/b", time.Hour)
	g.auth.Err = authapi.ErrUnavailable
	assert.Nil(t, g.r.LogoURL(t.Context(), other), "atm-auth-api down")
}

func TestAFileStorageDoesNotServeMeansNoLogo(t *testing.T) {
	g := newRig()
	tenant, file := uuid.New(), uuid.NewString()
	g.auth.SetTenant(authapi.Tenant{ID: tenant.String(), LogoFileID: &file})
	assert.Nil(t, g.r.LogoURL(t.Context(), tenant))
}
