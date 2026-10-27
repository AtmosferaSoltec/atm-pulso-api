package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGymSettings(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	owner, admin := h.token(t, tn.Owner, demoPassword), h.token(t, tn.Admin, demoPassword)
	path := "/gyms/" + tn.GymOne.String()
	for _, document := range []string{tn.Receptionist, h.newSuperadmin(t)} {
		assert.Equal(t, 403, h.call(t, http.MethodPatch, path, h.token(t, document, demoPassword), map[string]any{}).status)
	}
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/gyms/"+other.GymOne.String(), owner, map[string]any{}).status)
	assert.Equal(t, 403, h.call(t, http.MethodPatch, "/gyms/"+tn.GymTwo.String(), admin, map[string]any{}).status)
	for field, values := range map[string][]any{"display_name": {"", 123}, "brand_color": {"#ABCDEF", "blue", nil}, "day_pass_price": {"-1.00", "1000000.01", 15.0, "15"}, "branch_sharing": {nil, "true"}} {
		for _, value := range values {
			assert.Equal(t, 400, h.call(t, http.MethodPatch, path, owner, map[string]any{field: value}).status, field)
		}
	}
	result := h.call(t, http.MethodPatch, path, admin, map[string]any{"display_name": "Custom Gym", "brand_color": "#abcdef", "day_pass_price": "22.50", "branch_sharing": false})
	require.Equal(t, 200, result.status, result.body)
	assert.Equal(t, "22.50", result.data()["day_pass_price"])
	assert.Equal(t, false, result.data()["day_pass_price_is_default"])
	assert.Equal(t, false, result.data()["branch_sharing"])
	cleared := h.call(t, http.MethodPatch, path, owner, map[string]any{"display_name": nil, "day_pass_price": nil})
	require.Equal(t, 200, cleared.status, cleared.body)
	assert.Nil(t, cleared.data()["display_name"])
	assert.Equal(t, "15.00", cleared.data()["day_pass_price"])
	assert.Equal(t, "#abcdef", cleared.data()["brand_color"])
}
