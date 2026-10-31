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

	// A new gym has no module, and says so with an empty list (never null).
	assert.Equal(t, []any{}, h.meFeatures(t, admin))
	assert.Equal(t, []any{}, h.catalogFeatures(t, reception))

	// Role matrix: only the superadmin's token switches modules.
	body := map[string]any{"features": []string{"kiosk"}}
	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, admin, body).status)
	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, reception, body).status)
	assert.Equal(t, 401, h.call(t, http.MethodPatch, path, "", body).status)
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/admin/gyms/"+uuid.NewString()+"/features", super, body).status)
	assert.Equal(t, []any{}, h.meFeatures(t, admin), "refused requests change nothing")

	// Validation: the list is closed.
	for name, bad := range map[string]map[string]any{
		"unknown name": {"features": []string{"kiosk", "teleport"}},
		"empty name":   {"features": []string{""}},
		"upper case":   {"features": []string{"Kiosk"}},
		"null":         {"features": nil},
		"missing":      {},
		"not a list":   {"features": "kiosk"},
	} {
		res := h.call(t, http.MethodPatch, path, super, bad)
		assert.Equal(t, 400, res.status, name)
		assert.Equal(t, "VALIDATION_FAILED", res.errorCode(), name)
	}

	// Switch on, in any order and with repeats: stored once, in the fixed order.
	on := h.call(t, http.MethodPatch, path, super, map[string]any{"features": []string{"kiosk", "sales", "kiosk"}})
	require.Equal(t, 200, on.status, on.body)
	assert.Equal(t, []any{"sales", "kiosk"}, on.data()["features"])
	assert.Equal(t, tn.GymOne.String(), on.data()["id"])
	// The very next request of the gym sees it (the principal is rebuilt on every request).
	assert.Equal(t, []any{"sales", "kiosk"}, h.meFeatures(t, admin))
	assert.Equal(t, []any{"sales", "kiosk"}, h.meFeatures(t, reception))
	assert.Equal(t, []any{"sales", "kiosk"}, h.catalogFeatures(t, admin))
	// Isolation: neither the sibling gym of the group nor another group gets anything.
	assert.Equal(t, []any{}, h.meFeatures(t, h.token(t, tn.AdminTwo)))
	assert.Equal(t, []any{}, h.meFeatures(t, otherAdmin))
	// The superadmin's listing carries them.
	groups := h.call(t, http.MethodGet, "/admin/groups", super, nil)
	_, gym := gymOfCompany(t, groups, tn.TenantOne)
	require.NotNil(t, gym)
	assert.Equal(t, []any{"sales", "kiosk"}, gym["features"])
	// A gym admin cannot switch them from the gym route either.
	assert.Equal(t, 200, h.call(t, http.MethodPatch, "/gyms/"+tn.GymOne.String(), admin, map[string]any{"features": []string{"teleport"}}).status, "unknown fields are ignored")
	assert.Equal(t, []any{"sales", "kiosk"}, h.meFeatures(t, admin), "a gym admin cannot change its modules")

	// Replace the whole list, and switch everything off with an empty one.
	replaced := h.call(t, http.MethodPatch, path, super, map[string]any{"features": []string{"body_tracking"}})
	require.Equal(t, 200, replaced.status)
	assert.Equal(t, []any{"body_tracking"}, h.meFeatures(t, admin))
	off := h.call(t, http.MethodPatch, path, super, map[string]any{"features": []string{}})
	require.Equal(t, 200, off.status)
	assert.Equal(t, []any{}, off.data()["features"])
	assert.Equal(t, []any{}, h.meFeatures(t, admin))
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
	code, body = call("/kiosk", admin)
	assert.Equal(t, 403, code, "one module does not open another")
	assert.Contains(t, body, `"FEATURE_DISABLED"`)
	// Another gym's token stays out.
	code, _ = call("/products", h.token(t, tn.AdminTwo))
	assert.Equal(t, 403, code)
	// An unknown name in the database is ignored, never trusted.
	_, err = h.pool.Exec(t.Context(), `UPDATE gyms SET features = '{sales,legacy}' WHERE id = $1`, tn.GymOne)
	require.NoError(t, err)
	assert.Equal(t, []any{"sales"}, h.meFeatures(t, admin))
	// No token, no entry.
	code, _ = call("/products", "")
	assert.Equal(t, 401, code)
}
