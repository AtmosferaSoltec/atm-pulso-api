package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
)

func TestGymSettings(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	admin := h.token(t, tn.Admin)
	path := "/gyms/" + tn.GymOne.String()

	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, h.token(t, tn.Receptionist), map[string]any{}).status)
	// The identity of a gym (name, colour) is configured from the console; the gym only sets its rules.
	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, admin, map[string]any{"display_name": "Mine"}).status)
	colour := h.call(t, http.MethodPatch, path, admin, map[string]any{"brand_color": "#abcdef"})
	assert.Equal(t, 403, colour.status, "the colour is the console's, not the gym admin's")
	assert.Equal(t, "FORBIDDEN", colour.errorCode())
	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, h.superToken(t), map[string]any{"day_pass_price": "20.00"}).status, "the superadmin does not use the people routes")
	assert.Equal(t, 403, h.call(t, http.MethodPatch, "/gyms/"+tn.GymTwo.String(), admin, map[string]any{}).status, "an admin only manages the gym of their token")
	assert.Equal(t, 403, h.call(t, http.MethodPatch, "/gyms/"+other.GymOne.String(), admin, map[string]any{}).status)
	assert.Equal(t, 403, h.call(t, http.MethodPatch, "/gyms/"+ids.New().String(), admin, map[string]any{"brand_color": "#abcdef"}).status)
	assert.Equal(t, 200, h.call(t, http.MethodPatch, "/gyms/"+tn.GymTwo.String(), h.token(t, tn.AdminTwo), map[string]any{}).status, "the admin of the other gym manages that one")

	for field, values := range map[string][]any{"day_pass_price": {"-1.00", "1000000.01", 15.0, "15"}, "branch_sharing": {nil, "true"}} {
		for _, value := range values {
			assert.Equal(t, 400, h.call(t, http.MethodPatch, path, admin, map[string]any{field: value}).status, field)
		}
	}
	result := h.call(t, http.MethodPatch, path, admin, map[string]any{"day_pass_price": "22.50", "branch_sharing": false})
	require.Equal(t, 200, result.status, result.body)
	assert.Equal(t, "22.50", result.data()["day_pass_price"])
	assert.Equal(t, false, result.data()["day_pass_price_is_default"])
	assert.Equal(t, false, result.data()["branch_sharing"])
	cleared := h.call(t, http.MethodPatch, path, admin, map[string]any{"day_pass_price": nil})
	require.Equal(t, 200, cleared.status, cleared.body)
	assert.Equal(t, "15.00", cleared.data()["day_pass_price"])
	assert.Equal(t, false, cleared.data()["branch_sharing"], "a field not sent stays")
}
