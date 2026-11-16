package integration_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/testsupport"
)

// meFeatures and catalogFeatures read the modules of the caller's gym from the two endpoints that expose them.
func (h harness) meFeatures(t *testing.T, token string) []any {
	t.Helper()
	res := h.call(t, http.MethodGet, "/me", token, nil)
	require.Equal(t, 200, res.status, res.body)
	return res.data()["gym"].(map[string]any)["features"].([]any)
}

func (h harness) catalogFeatures(t *testing.T, token string) []any {
	t.Helper()
	res := h.call(t, http.MethodGet, "/catalog", token, nil)
	require.Equal(t, 200, res.status, res.body)
	return res.data()["gyms"].([]any)[0].(map[string]any)["features"].([]any)
}

func TestGymFeaturesAreSwitchedBySuperadminAndExposed(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	super, admin, reception := h.superToken(t), h.token(t, tn.Admin), h.token(t, tn.Receptionist)
	otherAdmin := h.token(t, other.Admin)
	path := "/admin/gyms/" + tn.GymOne.String() + "/features"

	// A new gym has only the base modules (the kiosk), never null.
	base := []any{"kiosk"}
	assert.Equal(t, base, h.meFeatures(t, admin))
	assert.Equal(t, base, h.catalogFeatures(t, reception))

	// Role matrix: only the superadmin's token switches modules.
	body := map[string]any{"features": []string{"sales"}}
	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, admin, body).status)
	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, reception, body).status)
	assert.Equal(t, 401, h.call(t, http.MethodPatch, path, "", body).status)
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/admin/gyms/"+uuid.NewString()+"/features", super, body).status)
	assert.Equal(t, base, h.meFeatures(t, admin), "refused requests change nothing")

	// Validation: the list is closed.
	for name, bad := range map[string]map[string]any{
		"unknown name": {"features": []string{"sales", "teleport"}},
		"empty name":   {"features": []string{""}},
		"upper case":   {"features": []string{"Sales"}},
		"null":         {"features": nil},
		"missing":      {},
		"not a list":   {"features": "sales"},
	} {
		res := h.call(t, http.MethodPatch, path, super, bad)
		assert.Equal(t, 400, res.status, name)
		assert.Equal(t, "VALIDATION_FAILED", res.errorCode(), name)
	}

	// Switch on, in any order and with repeats: stored once, in the fixed order. A base module sent along is
	// accepted and not stored (it is always on).
	on := h.call(t, http.MethodPatch, path, super, map[string]any{"features": []string{"kiosk", "sales", "sales"}})
	require.Equal(t, 200, on.status, on.body)
	assert.Equal(t, []any{"sales"}, on.data()["features"])
	assert.Equal(t, tn.GymOne.String(), on.data()["id"])
	// The very next request of the gym sees it (the principal is rebuilt on every request).
	assert.Equal(t, []any{"sales", "kiosk"}, h.meFeatures(t, admin))
	assert.Equal(t, []any{"sales", "kiosk"}, h.meFeatures(t, reception))
	assert.Equal(t, []any{"sales", "kiosk"}, h.catalogFeatures(t, admin))
	// Isolation: neither the sibling gym of the group nor another group gets anything.
	assert.Equal(t, base, h.meFeatures(t, h.token(t, tn.AdminTwo)))
	assert.Equal(t, base, h.meFeatures(t, otherAdmin))
	// The superadmin's listing carries them.
	groups := h.call(t, http.MethodGet, "/admin/groups", super, nil)
	_, gym := gymOfCompany(t, groups, tn.TenantOne)
	require.NotNil(t, gym)
	assert.Equal(t, []any{"sales"}, gym["features"], "the listing carries what is stored: the optional modules")
	assert.Equal(t, "active", gym["status"])
	// A gym admin cannot switch them from the gym route either.
	assert.Equal(t, 200, h.call(t, http.MethodPatch, "/gyms/"+tn.GymOne.String(), admin, map[string]any{"features": []string{"teleport"}}).status, "unknown fields are ignored")
	assert.Equal(t, []any{"sales", "kiosk"}, h.meFeatures(t, admin), "a gym admin cannot change its modules")

	// Replace the whole list, and switch everything off with an empty one.
	replaced := h.call(t, http.MethodPatch, path, super, map[string]any{"features": []string{"body_tracking"}})
	require.Equal(t, 200, replaced.status)
	assert.Equal(t, []any{"body_tracking", "kiosk"}, h.meFeatures(t, admin))
	off := h.call(t, http.MethodPatch, path, super, map[string]any{"features": []string{}})
	require.Equal(t, 200, off.status)
	assert.Equal(t, []any{}, off.data()["features"])
	assert.Equal(t, base, h.meFeatures(t, admin), "the base modules stay")
}

// TestRequireFeatureGuard mounts the reusable guard behind the real authentication on a probe router.
func TestRequireFeatureGuard(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	accountsModule := accounts.New(accounts.Deps{Pool: h.pool, Clock: clock.System{}, Auth: h.auth})
	guard := auth.NewGuard(auth.NewJWKSVerifier(auth.VerifierConfig{JWKSURL: h.tokens.JWKSURL, Issuer: testsupport.TestIssuer}), accountsModule.Resolver(), systemCode)
	gin.SetMode(gin.TestMode)
	probe := gin.New()
	protected := probe.Group("", guard.User())
	ok := func(c *gin.Context) { c.Status(http.StatusNoContent) }
	protected.GET("/products", auth.RequireFeature(auth.FeatureSales), ok)
	protected.GET("/body", auth.RequireFeature(auth.FeatureBodyTracking), ok)
	protected.GET("/kiosk", auth.RequireFeature(auth.FeatureKiosk), ok)
	call := func(path, token string) (int, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		probe.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	admin, reception := h.token(t, tn.Admin), h.token(t, tn.Receptionist)

	code, body := call("/products", admin)
	assert.Equal(t, 403, code)
	assert.Contains(t, body, `"FEATURE_DISABLED"`)
	code, _ = call("/products", reception)
	assert.Equal(t, 403, code, "roles do not matter: the gym lacks the module")

	_, err := h.pool.Exec(t.Context(), `UPDATE gyms SET features = '{sales}' WHERE id = $1`, tn.GymOne)
	require.NoError(t, err)
	code, _ = call("/products", admin)
	assert.Equal(t, 204, code, "switching the module on opens it at once")
	code, _ = call("/products", reception)
	assert.Equal(t, 204, code)
	code, body = call("/body", admin)
	assert.Equal(t, 403, code, "one module does not open another")
	assert.Contains(t, body, `"FEATURE_DISABLED"`)
	code, _ = call("/kiosk", reception)
	assert.Equal(t, 204, code, "a base module is open for every gym")
	// Another gym's token stays out.
	code, _ = call("/products", h.token(t, tn.AdminTwo))
	assert.Equal(t, 403, code)
	// An unknown name in the database is ignored, never trusted.
	_, err = h.pool.Exec(t.Context(), `UPDATE gyms SET features = '{sales,legacy}' WHERE id = $1`, tn.GymOne)
	require.NoError(t, err)
	assert.Equal(t, []any{"sales", "kiosk"}, h.meFeatures(t, admin))
	// No token, no entry.
	code, _ = call("/products", "")
	assert.Equal(t, 401, code)
}

func TestSuperadminSuspendsAndReactivatesAGym(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	super, admin, reception := h.superToken(t), h.token(t, tn.Admin), h.token(t, tn.Receptionist)
	path := "/admin/gyms/" + tn.GymOne.String() + "/status"

	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, admin, map[string]any{"status": "suspended"}).status, "only the superadmin")
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/admin/gyms/"+uuid.NewString()+"/status", super, map[string]any{"status": "suspended"}).status)
	bad := h.call(t, http.MethodPatch, path, super, map[string]any{"status": "paused"})
	assert.Equal(t, 400, bad.status)
	assert.Equal(t, "VALIDATION_FAILED", bad.errorCode())

	suspended := h.call(t, http.MethodPatch, path, super, map[string]any{"status": "suspended"})
	require.Equal(t, 200, suspended.status, suspended.body)
	assert.Equal(t, "suspended", suspended.data()["status"])
	for _, token := range []string{admin, reception} {
		res := h.call(t, http.MethodGet, "/me", token, nil)
		assert.Equal(t, 403, res.status)
		assert.Equal(t, "GYM_SUSPENDED", res.errorCode())
	}
	// The other gym of the same client keeps working.
	assert.Equal(t, 200, h.call(t, http.MethodGet, "/me", h.token(t, tn.AdminTwo), nil).status)

	active := h.call(t, http.MethodPatch, path, super, map[string]any{"status": "active"})
	require.Equal(t, 200, active.status, active.body)
	assert.Equal(t, 200, h.call(t, http.MethodGet, "/me", admin, nil).status, "reactivating opens it at once")
}

func TestSuperadminReadsTheModuleCatalog(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/admin/modules", h.token(t, tn.Admin), nil).status)
	res := h.call(t, http.MethodGet, "/admin/modules", h.superToken(t), nil)
	require.Equal(t, 200, res.status, res.body)
	kinds := map[string]string{}
	for _, item := range res.body["data"].([]any) {
		m := item.(map[string]any)
		kinds[m["code"].(string)] = m["kind"].(string)
	}
	assert.Equal(t, "base", kinds["members"])
	assert.Equal(t, "base", kinds["kiosk"])
	assert.Equal(t, "optional", kinds["sales"])
	assert.Equal(t, "optional", kinds["body_tracking"])
}
