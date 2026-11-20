package integration_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func names(t *testing.T, res response) []string {
	t.Helper()
	require.Equal(t, 200, res.status, res.body)
	out := []string{}
	for _, c := range res.body["data"].([]any) {
		out = append(out, c.(map[string]any)["name"].(string))
	}
	return out
}

func TestProductCategoriesRolesUniquenessAndIsolation(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, reception, adminTwo := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, tn.AdminTwo)

	// The module guard applies.
	assert.Equal(t, "FEATURE_DISABLED", h.call(t, http.MethodGet, "/product-categories", admin, nil).errorCode())
	h.setSales(t, tn.GymOne, true)
	h.setSales(t, tn.GymTwo, true)

	create := func(token, name string) response {
		t.Helper()
		return h.call(t, http.MethodPost, "/product-categories", token, map[string]any{"name": name})
	}
	batidos := create(admin, "  Batidos  ")
	require.Equal(t, 201, batidos.status, batidos.body)
	assert.Equal(t, "Batidos", batidos.data()["name"])
	assert.Equal(t, 0.0, batidos.data()["position"])
	batidosID := batidos.data()["id"].(string)
	require.Equal(t, 201, create(admin, "Suplementos").status)
	assert.Equal(t, 2.0, create(admin, "Agua").data()["position"])

	taken := create(admin, "BATIDOS")
	assert.Equal(t, 409, taken.status)
	assert.Equal(t, "CATEGORY_NAME_TAKEN", taken.errorCode())
	assert.Equal(t, 400, create(admin, "   ").status)
	assert.Equal(t, 400, create(admin, "12345678901234567890123456789012345678901").status, "41 characters")
	assert.Equal(t, 201, create(admin, "1234567890123456789012345678901234567890").status, "40 characters")
	assert.Equal(t, 403, create(reception, "Recepción").status, "admins only")

	// Reception reads them; the other gym has its own list and may reuse the names.
	assert.Equal(t, []string{"Batidos", "Suplementos", "Agua", "1234567890123456789012345678901234567890"}, names(t, h.call(t, http.MethodGet, "/product-categories", reception, nil)))
	assert.Empty(t, names(t, h.call(t, http.MethodGet, "/product-categories", adminTwo, nil)))
	assert.Equal(t, 201, create(adminTwo, "Batidos").status)

	// Rename and reorder; another gym cannot touch it.
	patch := func(token, id string, body map[string]any) response {
		t.Helper()
		return h.call(t, http.MethodPatch, "/product-categories/"+id, token, body)
	}
	assert.Equal(t, 404, patch(adminTwo, batidosID, map[string]any{"name": "Mío"}).status)
	assert.Equal(t, 403, patch(reception, batidosID, map[string]any{"name": "Mío"}).status)
	assert.Equal(t, "CATEGORY_NAME_TAKEN", patch(admin, batidosID, map[string]any{"name": "agua"}).errorCode())
	renamed := patch(admin, batidosID, map[string]any{"name": "Batidos y jugos", "position": 9})
	require.Equal(t, 200, renamed.status, renamed.body)
	assert.Equal(t, "Batidos y jugos", renamed.data()["name"])
	assert.Equal(t, 9.0, renamed.data()["position"])
	assert.Equal(t, 400, patch(admin, batidosID, map[string]any{"position": -1}).status)
	assert.Equal(t, "Batidos y jugos", names(t, h.call(t, http.MethodGet, "/product-categories", admin, nil))[3], "ordered by position")

	assert.Equal(t, 404, h.call(t, http.MethodDelete, "/product-categories/"+batidosID, adminTwo, nil).status)
	assert.Equal(t, 403, h.call(t, http.MethodDelete, "/product-categories/"+batidosID, reception, nil).status)
	assert.Equal(t, 404, h.call(t, http.MethodDelete, "/product-categories/"+uuid.NewString(), admin, nil).status)
}

func TestProductsCarryTheirCategoryAndLoseItOnDelete(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	h.setSales(t, tn.GymOne, true)
	h.setSales(t, tn.GymTwo, true)
	admin, adminTwo := h.token(t, tn.Admin), h.token(t, tn.AdminTwo)
	category := h.call(t, http.MethodPost, "/product-categories", admin, map[string]any{"name": "Batidos"}).data()["id"].(string)
	foreign := h.call(t, http.MethodPost, "/product-categories", adminTwo, map[string]any{"name": "Ajena"}).data()["id"].(string)

	res := h.call(t, http.MethodPost, "/products", admin, map[string]any{"name": "Batido fresa", "price": "8", "category_id": category})
	require.Equal(t, 201, res.status, res.body)
	assert.Equal(t, category, res.data()["category_id"])
	assert.Equal(t, map[string]any{"id": category, "name": "Batidos"}, res.data()["category"])
	product := res.data()["id"].(string)

	plain := h.call(t, http.MethodPost, "/products", admin, map[string]any{"name": "Agua", "price": "2"})
	require.Equal(t, 201, plain.status)
	assert.Nil(t, plain.data()["category_id"])
	assert.Contains(t, plain.data(), "category")
	assert.Nil(t, plain.data()["category"])

	bad := h.call(t, http.MethodPost, "/products", admin, map[string]any{"name": "Robado", "price": "2", "category_id": foreign})
	assert.Equal(t, 400, bad.status, "a category of another gym")
	assert.Equal(t, 400, h.call(t, http.MethodPatch, "/products/"+product, admin, map[string]any{"category_id": foreign}).status)
	assert.Equal(t, 400, h.call(t, http.MethodPatch, "/products/"+product, admin, map[string]any{"category_id": uuid.NewString()}).status)

	// Absent keeps it, null clears it.
	kept := h.call(t, http.MethodPatch, "/products/"+product, admin, map[string]any{"price": "9"})
	assert.Equal(t, category, kept.data()["category_id"])
	cleared := h.call(t, http.MethodPatch, "/products/"+product, admin, map[string]any{"category_id": nil})
	require.Equal(t, 200, cleared.status, cleared.body)
	assert.Nil(t, cleared.data()["category_id"])
	assert.Equal(t, category, h.call(t, http.MethodPatch, "/products/"+product, admin, map[string]any{"category_id": category}).data()["category_id"])

	// Deleting the category leaves the product without one.
	assert.Equal(t, 204, h.call(t, http.MethodDelete, "/product-categories/"+category, admin, nil).status)
	list := h.call(t, http.MethodGet, "/products", admin, nil).body["data"].([]any)
	require.Len(t, list, 2)
	for _, p := range list {
		assert.Nil(t, p.(map[string]any)["category_id"])
		assert.Nil(t, p.(map[string]any)["category"])
	}
}

func TestConsoleManagesProductCategoriesInBulk(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	h.setSales(t, tn.GymOne, true)
	super, admin := h.superToken(t), h.token(t, tn.Admin)
	base := "/admin/gyms/" + tn.GymOne.String() + "/product-categories"
	require.Equal(t, 201, h.call(t, http.MethodPost, "/product-categories", admin, map[string]any{"name": "Batidos"}).status)

	res := h.call(t, http.MethodPost, base, super, map[string]any{"names": []string{"batidos", "Suplementos", "", "  Material ", "SUPLEMENTOS"}})
	require.Equal(t, 201, res.status, res.body)
	created := res.data()["created"].([]any)
	require.Len(t, created, 2)
	assert.Equal(t, "Suplementos", created[0].(map[string]any)["name"])
	assert.Equal(t, 1.0, created[0].(map[string]any)["position"])
	assert.Equal(t, "Material", created[1].(map[string]any)["name"])
	assert.Equal(t, []any{"batidos", "SUPLEMENTOS"}, res.data()["skipped"])

	again := h.call(t, http.MethodPost, base, super, map[string]any{"names": []string{"Material"}})
	assert.Equal(t, 200, again.status, "nothing new")
	assert.Empty(t, again.data()["created"])
	assert.Equal(t, 400, h.call(t, http.MethodPost, base, super, map[string]any{"names": []string{" "}}).status)

	list := h.call(t, http.MethodGet, base, super, nil)
	assert.Equal(t, []string{"Batidos", "Suplementos", "Material"}, names(t, list))
	material := list.body["data"].([]any)[2].(map[string]any)["id"].(string)
	product := h.call(t, http.MethodPost, "/products", admin, map[string]any{"name": "Cuerda", "price": "20", "category_id": material})
	require.Equal(t, 201, product.status)
	list = h.call(t, http.MethodGet, base, super, nil)
	assert.Equal(t, 1.0, list.body["data"].([]any)[2].(map[string]any)["product_count"])

	renamed := h.call(t, http.MethodPatch, base+"/"+material, super, map[string]any{"name": "Accesorios"})
	require.Equal(t, 200, renamed.status, renamed.body)
	assert.Equal(t, "Accesorios", h.call(t, http.MethodGet, "/products", admin, nil).body["data"].([]any)[0].(map[string]any)["category"].(map[string]any)["name"])

	// Another gym's path cannot reach this category; an unknown gym is 404; people cannot use /admin.
	other := "/admin/gyms/" + tn.GymTwo.String() + "/product-categories/" + material
	assert.Equal(t, 404, h.call(t, http.MethodPatch, other, super, map[string]any{"name": "X"}).status)
	assert.Equal(t, 404, h.call(t, http.MethodDelete, other, super, nil).status)
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/admin/gyms/"+uuid.NewString()+"/product-categories", super, nil).status)
	assert.Equal(t, 403, h.call(t, http.MethodGet, base, admin, nil).status)
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/product-categories", super, nil).status)

	assert.Equal(t, 204, h.call(t, http.MethodDelete, base+"/"+material, super, nil).status)
	assert.Nil(t, h.call(t, http.MethodGet, "/products", admin, nil).body["data"].([]any)[0].(map[string]any)["category"])
	assert.Equal(t, []string{"Batidos", "Suplementos"}, names(t, h.call(t, http.MethodGet, "/product-categories", admin, nil)))
}
