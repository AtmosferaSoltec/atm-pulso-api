package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
)

// The logo of a gym comes from the company profile (atm-auth-api) as a file id and from atm-storage-api as a
// temporary link. Pulso exposes it as logo_url in /me, /catalog and the printable receipt, only for the
// caller's own gym.
func TestGymLogoComesFromTheCompanyProfile(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	logoOne, logoTwo := uuid.NewString(), uuid.NewString()
	h.auth.SetTenant(authapi.Tenant{ID: tn.TenantOne.String(), DisplayName: "Gym Uno", LogoFileID: &logoOne})
	h.auth.SetTenant(authapi.Tenant{ID: tn.TenantTwo.String(), DisplayName: "Gym Dos", LogoFileID: &logoTwo})
	h.storage.SetLink(logoOne, "https://files.test/uno?sig=abc", time.Now().Add(time.Hour))
	h.storage.SetLink(logoTwo, "https://files.test/dos?sig=def", time.Now().Add(time.Hour))

	admin, adminTwo, reception := h.token(t, tn.Admin), h.token(t, tn.AdminTwo), h.token(t, tn.Receptionist)
	logoOf := func(res response, path ...string) any {
		require.Equal(t, http.StatusOK, res.status, res.body)
		var node any = res.data()
		for _, key := range path {
			if list, ok := node.([]any); ok {
				node = list[0]
			}
			node = node.(map[string]any)[key]
		}
		if list, ok := node.([]any); ok {
			node = list[0]
		}
		return node.(map[string]any)["logo_url"]
	}

	// Every role of a gym sees that gym's logo, never a sibling's.
	assert.Equal(t, "https://files.test/uno?sig=abc", logoOf(h.call(t, http.MethodGet, "/me", admin, nil), "gym"))
	assert.Equal(t, "https://files.test/uno?sig=abc", logoOf(h.call(t, http.MethodGet, "/me", reception, nil), "gym"))
	assert.Equal(t, "https://files.test/dos?sig=def", logoOf(h.call(t, http.MethodGet, "/me", adminTwo, nil), "gym"))
	assert.Equal(t, "https://files.test/uno?sig=abc", logoOf(h.call(t, http.MethodGet, "/catalog", reception, nil), "gyms"))
	assert.Equal(t, "https://files.test/dos?sig=def", logoOf(h.call(t, http.MethodGet, "/catalog", adminTwo, nil), "gyms"))

	// The printable receipt carries it too.
	receiptID := h.sellWith(t, reception, tn.BranchA, "cash")
	assert.Equal(t, "https://files.test/uno?sig=abc", logoOf(h.call(t, http.MethodGet, "/receipts/"+receiptID, reception, nil), "gym"))
	// A foreign gym cannot read the receipt, so it never gets this gym's logo.
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodGet, "/receipts/"+receiptID, adminTwo, nil).status)
}

func TestGymWithoutLogoOrWithServicesDownStillWorks(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, adminTwo := h.token(t, tn.Admin), h.token(t, tn.AdminTwo)

	// No logo in the profile: null.
	h.auth.SetTenant(authapi.Tenant{ID: tn.TenantOne.String(), DisplayName: "Gym Uno"})
	me := h.call(t, http.MethodGet, "/me", admin, nil)
	require.Equal(t, http.StatusOK, me.status, me.body)
	assert.Nil(t, me.data()["gym"].(map[string]any)["logo_url"])

	// atm-storage-api down: the request succeeds and the logo is null.
	logo := uuid.NewString()
	h.auth.SetTenant(authapi.Tenant{ID: tn.TenantTwo.String(), DisplayName: "Gym Dos", LogoFileID: &logo})
	h.storage.Err = assert.AnError
	catalog := h.call(t, http.MethodGet, "/catalog", adminTwo, nil)
	require.Equal(t, http.StatusOK, catalog.status, catalog.body)
	assert.Nil(t, catalog.data()["gyms"].([]any)[0].(map[string]any)["logo_url"])
	assert.Len(t, catalog.data()["gyms"].([]any)[0].(map[string]any)["branches"], 1)
}
