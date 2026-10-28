package integration_test

import (
	"net/http"
	"testing"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGymSettings(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	owner, admin := h.token(t, tn.Owner, demoPassword), h.token(t, tn.Admin, demoPassword)
	path := "/gyms/" + tn.GymOne.String()
	super := h.token(t, h.newSuperadmin(t), demoPassword)
	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, h.token(t, tn.Receptionist, demoPassword), map[string]any{}).status)
	// The superadmin sets the identity and nothing else; owners and admins everything but the identity.
	for _, body := range []map[string]any{{"day_pass_price": "20.00"}, {"branch_sharing": false}, {"membership_prices": []any{}}} {
		assert.Equal(t, 403, h.call(t, http.MethodPatch, path, super, body).status, body)
	}
	for _, body := range []map[string]any{{"display_name": "Mine"}, {"brand_color": "#abcdef"}} {
		assert.Equal(t, 403, h.call(t, http.MethodPatch, path, owner, body).status, body)
		assert.Equal(t, 403, h.call(t, http.MethodPatch, path, admin, body).status, body)
	}
	identity := h.call(t, http.MethodPatch, path, super, map[string]any{"display_name": "Custom Gym", "brand_color": "#abcdef"})
	require.Equal(t, 200, identity.status, identity.body)
	assert.Equal(t, "Custom Gym", identity.data()["display_name"])
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/gyms/"+ids.New().String(), super, map[string]any{"brand_color": "#abcdef"}).status)
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/gyms/"+other.GymOne.String(), owner, map[string]any{}).status)
	assert.Equal(t, 403, h.call(t, http.MethodPatch, "/gyms/"+tn.GymTwo.String(), admin, map[string]any{}).status)
	for field, values := range map[string][]any{"display_name": {"", 123}, "brand_color": {"#ABCDEF", "blue", nil}, "day_pass_price": {"-1.00", "1000000.01", 15.0, "15"}, "branch_sharing": {nil, "true"}} {
		who := owner
		if field == "display_name" || field == "brand_color" {
			who = super
		}
		for _, value := range values {
			assert.Equal(t, 400, h.call(t, http.MethodPatch, path, who, map[string]any{field: value}).status, field)
		}
	}
	result := h.call(t, http.MethodPatch, path, admin, map[string]any{"day_pass_price": "22.50", "branch_sharing": false})
	require.Equal(t, 200, result.status, result.body)
	assert.Equal(t, "22.50", result.data()["day_pass_price"])
	assert.Equal(t, false, result.data()["day_pass_price_is_default"])
	assert.Equal(t, false, result.data()["branch_sharing"])
	cleared := h.call(t, http.MethodPatch, path, owner, map[string]any{"day_pass_price": nil})
	require.Equal(t, 200, cleared.status, cleared.body)
	assert.Equal(t, "Custom Gym", cleared.data()["display_name"], "the identity set by the superadmin stays")
	restored := h.call(t, http.MethodPatch, path, super, map[string]any{"display_name": nil})
	require.Equal(t, 200, restored.status, restored.body)
	assert.Nil(t, restored.data()["display_name"])
	assert.Equal(t, "15.00", cleared.data()["day_pass_price"])
	assert.Equal(t, "#abcdef", cleared.data()["brand_color"])
}

func TestGymMembershipPrices(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	owner, admin := h.token(t, tn.Owner, demoPassword), h.token(t, tn.Admin, demoPassword)
	path := "/gyms/" + tn.GymOne.String()
	for _, value := range []any{
		nil, "100.00", []any{map[string]any{"months": 0, "amount": "10.00"}}, []any{map[string]any{"months": 25, "amount": "10.00"}},
		[]any{map[string]any{"months": 1, "amount": "10"}}, []any{map[string]any{"months": 1, "amount": "-1.00"}},
		[]any{map[string]any{"months": 1, "amount": "10.00"}, map[string]any{"months": 1, "amount": "12.00"}},
	} {
		assert.Equal(t, 400, h.call(t, http.MethodPatch, path, owner, map[string]any{"membership_prices": value}).status, value)
	}
	saved := h.call(t, http.MethodPatch, path, admin, map[string]any{"membership_prices": []any{
		map[string]any{"months": 2, "amount": "180.00"}, map[string]any{"months": 1, "amount": "100.00"},
	}})
	require.Equal(t, 200, saved.status, saved.body)
	// The order sent is the order kept.
	assert.Equal(t, []any{
		map[string]any{"months": float64(2), "amount": "180.00"}, map[string]any{"months": float64(1), "amount": "100.00"},
	}, saved.data()["membership_prices"])
	// Other fields leave the list alone; an empty list clears it.
	other := h.call(t, http.MethodPatch, path, owner, map[string]any{"day_pass_price": "20.00"})
	require.Equal(t, 200, other.status, other.body)
	assert.Len(t, other.data()["membership_prices"], 2)
	cleared := h.call(t, http.MethodPatch, path, owner, map[string]any{"membership_prices": []any{}})
	require.Equal(t, 200, cleared.status, cleared.body)
	assert.Empty(t, cleared.data()["membership_prices"])
}
