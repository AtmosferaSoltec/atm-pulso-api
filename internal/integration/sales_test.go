package integration_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setSales switches the sales module of a gym on or off (the superadmin's PATCH, like console-web).
func (h harness) setSales(t *testing.T, gym uuid.UUID, on bool) {
	t.Helper()
	features := []string{}
	if on {
		features = []string{"sales"}
	}
	res := h.call(t, http.MethodPatch, "/admin/gyms/"+gym.String()+"/features", h.superToken(t), map[string]any{"features": features})
	require.Equal(t, 200, res.status, res.body)
}

func (h harness) product(t *testing.T, admin, name, price string) string {
	t.Helper()
	res := h.call(t, http.MethodPost, "/products", admin, map[string]any{"name": name, "price": price})
	require.Equal(t, 201, res.status, res.body)
	return res.data()["id"].(string)
}

func (h harness) sell(t *testing.T, token string, body map[string]any) response {
	t.Helper()
	return h.call(t, http.MethodPost, "/sales", token, body)
}

func item(product string, quantity int) map[string]any {
	return map[string]any{"product_id": product, "quantity": quantity}
}

func (h harness) memberID(t *testing.T, admin string, branch uuid.UUID) string {
	t.Helper()
	res := h.enroll(t, admin, enrollBody(branch, uniqueDocument()))
	require.Equal(t, 201, res.status, res.body)
	return memberIDOf(res)
}

func TestProductsModuleOffAnswersFeatureDisabledEverywhere(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, reception := h.token(t, tn.Admin), h.token(t, tn.Receptionist)
	id := uuid.NewString()
	calls := []struct{ method, path string }{
		{"GET", "/products"}, {"POST", "/products"}, {"PATCH", "/products/" + id}, {"GET", "/sales"}, {"POST", "/sales"},
		{"GET", "/sales/pending"}, {"GET", "/sales/report"}, {"POST", "/sales/collections"}, {"GET", "/members/" + id + "/pending-sales"}, {"GET", "/members/" + id + "/sales"},
	}
	for _, token := range []string{admin, reception} {
		for _, call := range calls {
			res := h.call(t, call.method, call.path, token, map[string]any{})
			assert.Equal(t, 403, res.status, call.method+" "+call.path)
			assert.Equal(t, "FEATURE_DISABLED", res.errorCode(), call.method+" "+call.path)
		}
	}
	// On for gym one only: the sibling gym of the same group stays off.
	h.setSales(t, tn.GymOne, true)
	assert.Equal(t, 200, h.call(t, http.MethodGet, "/products", admin, nil).status)
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/products", h.token(t, tn.AdminTwo), nil).status)
	assert.Equal(t, 401, h.call(t, http.MethodGet, "/products", "", nil).status)
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/products", h.superToken(t), nil).status)
	// Off again: immediate.
	h.setSales(t, tn.GymOne, false)
	assert.Equal(t, "FEATURE_DISABLED", h.call(t, http.MethodGet, "/products", admin, nil).errorCode())
}

func TestProductCatalogRolesValidationAndIsolation(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	h.setSales(t, tn.GymOne, true)
	h.setSales(t, other.GymOne, true)
	admin, reception, otherAdmin := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, other.Admin)

	assert.Equal(t, 403, h.call(t, http.MethodPost, "/products", reception, map[string]any{"name": "Agua", "price": "2.50"}).status, "only the admin manages the catalog")
	created := h.call(t, http.MethodPost, "/products", admin, map[string]any{"name": "  Batido   de fresa ", "price": "8.5"})
	require.Equal(t, 201, created.status, created.body)
	assert.Equal(t, "Batido de fresa", created.data()["name"])
	assert.Equal(t, "8.50", created.data()["price"])
	assert.Equal(t, true, created.data()["is_active"])
	batido := created.data()["id"].(string)
	agua := h.product(t, admin, "Agua", "2.00")
	gaseosa := h.product(t, admin, "Gaseosa", "3.00")

	for name, bad := range map[string]map[string]any{
		"name empty": {"name": " ", "price": "1"}, "name long": {"name": string(make([]rune, 61)), "price": "1"},
		"price zero": {"name": "X1", "price": "0"}, "price negative": {"name": "X1", "price": "-1"}, "price cap": {"name": "X1", "price": "10000.01"},
		"price text": {"name": "X1", "price": "abc"}, "price decimals": {"name": "X1", "price": "1.234"}, "price number": {"name": "X1"},
		"position": {"name": "X1", "price": "1", "position": -2},
	} {
		res := h.call(t, http.MethodPost, "/products", admin, bad)
		assert.Equal(t, 400, res.status, name)
		assert.Equal(t, "VALIDATION_FAILED", res.errorCode(), name)
	}
	taken := h.call(t, http.MethodPost, "/products", admin, map[string]any{"name": " agua ", "price": "1"})
	assert.Equal(t, 409, taken.status)
	assert.Equal(t, "PRODUCT_NAME_TAKEN", taken.errorCode())
	assert.Equal(t, 201, h.call(t, http.MethodPost, "/products", otherAdmin, map[string]any{"name": "Agua", "price": "1"}).status, "another gym may reuse the name")

	// Idempotent by id: the same id of the same gym answers the stored product; an id of another gym is a conflict.
	fixed := uuid.NewString()
	assert.Equal(t, 201, h.call(t, http.MethodPost, "/products", admin, map[string]any{"id": fixed, "name": "Barra", "price": "5"}).status)
	assert.Equal(t, 200, h.call(t, http.MethodPost, "/products", admin, map[string]any{"id": fixed, "name": "Barra", "price": "5"}).status)
	assert.Equal(t, 409, h.call(t, http.MethodPost, "/products", otherAdmin, map[string]any{"id": fixed, "name": "Barra", "price": "5"}).status)
	assert.Len(t, h.call(t, http.MethodGet, "/products?include_inactive=true", admin, nil).body["data"], 4)

	// Order: by position, creation puts the product last; PATCH moves it.
	names := func(token, query string) []string {
		res := h.call(t, http.MethodGet, "/products"+query, token, nil)
		require.Equal(t, 200, res.status)
		var out []string
		for _, raw := range res.body["data"].([]any) {
			out = append(out, raw.(map[string]any)["name"].(string))
		}
		return out
	}
	assert.Equal(t, []string{"Batido de fresa", "Agua", "Gaseosa", "Barra"}, names(admin, ""))
	assert.Equal(t, 200, h.call(t, http.MethodPatch, "/products/"+batido, admin, map[string]any{"position": 7}).status)
	assert.Equal(t, 200, h.call(t, http.MethodPatch, "/products/"+gaseosa, admin, map[string]any{"position": 0}).status)
	assert.Equal(t, 0, indexOf(names(admin, ""), "Gaseosa"))
	assert.Equal(t, 3, indexOf(names(admin, ""), "Batido de fresa"))

	// Edit and deactivate: reception only sees active ones and cannot edit.
	assert.Equal(t, 403, h.call(t, http.MethodPatch, "/products/"+agua, reception, map[string]any{"price": "9"}).status)
	edited := h.call(t, http.MethodPatch, "/products/"+agua, admin, map[string]any{"price": "2.50", "is_active": false})
	require.Equal(t, 200, edited.status, edited.body)
	assert.Equal(t, "2.50", edited.data()["price"])
	assert.NotContains(t, names(reception, "?include_inactive=true"), "Agua", "reception never lists inactive products")
	assert.Contains(t, names(admin, "?include_inactive=true"), "Agua")
	assert.NotContains(t, names(admin, ""), "Agua")
	assert.Equal(t, 400, h.call(t, http.MethodPatch, "/products/"+agua, admin, map[string]any{"price": "0"}).status)
	assert.Equal(t, 409, h.call(t, http.MethodPatch, "/products/"+agua, admin, map[string]any{"name": "Barra"}).status)
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/products/"+agua, otherAdmin, map[string]any{"price": "1"}).status, "another gym never edits it")
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/products/"+uuid.NewString(), admin, map[string]any{"price": "1"}).status)
}

func indexOf(list []string, value string) int {
	for i, v := range list {
		if v == value {
			return i
		}
	}
	return -1
}

func TestSellingValidationPriceSnapshotBranchesAndIdempotency(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	h.setSales(t, tn.GymOne, true)
	h.setSales(t, other.GymOne, true)
	admin, reception, otherAdmin := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, other.Admin)
	batido, agua := h.product(t, admin, "Batido", "8.00"), h.product(t, admin, "Agua", "2.50")
	foreign := h.product(t, otherAdmin, "Foreign", "1.00")
	hidden := h.extraBranch(t, tn)
	member := h.memberID(t, admin, tn.BranchA)
	foreignMember := h.memberID(t, otherAdmin, other.BranchA)

	ok := h.sell(t, reception, map[string]any{"branch_id": tn.BranchA, "method": "yape_plin", "items": []any{item(batido, 2), item(agua, 1), item(batido, 1)}})
	require.Equal(t, 201, ok.status, ok.body)
	sale := ok.data()
	assert.Equal(t, "paid", sale["status"])
	assert.Equal(t, "26.50", sale["total"], "8.00 x 3 + 2.50")
	assert.Equal(t, float64(4), sale["units"])
	assert.Equal(t, "yape_plin", sale["method"])
	assert.Nil(t, sale["member"])
	assert.Equal(t, "Recepción de Prueba", sale["sold_by"].(map[string]any)["full_name"])
	assert.Equal(t, "Sede A", sale["branch"].(map[string]any)["name"])
	assert.Len(t, sale["items"], 2, "repeated products are merged")
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, sale["sold_on"])
	assert.Equal(t, false, sale["was_pending"])

	// The price is a photo: changing the catalog does not change the old sale.
	require.Equal(t, 200, h.call(t, http.MethodPatch, "/products/"+batido, admin, map[string]any{"price": "10.00", "name": "Batido XL"}).status)
	list := h.call(t, http.MethodGet, "/sales", admin, nil)
	require.Equal(t, 200, list.status, list.body)
	first := list.body["data"].([]any)[0].(map[string]any)
	assert.Equal(t, "26.50", first["total"])
	for _, raw := range first["items"].([]any) {
		it := raw.(map[string]any)
		if it["name"] == "Batido" {
			assert.Equal(t, "8.00", it["unit_price"])
			assert.Equal(t, "24.00", it["line_total"])
		}
	}
	newer := h.sell(t, admin, map[string]any{"branch_id": tn.BranchA, "method": "cash", "items": []any{item(batido, 1)}})
	assert.Equal(t, "10.00", newer.data()["total"])

	// Member optional on a paid sale; a member of another gym is refused.
	withMember := h.sell(t, admin, map[string]any{"branch_id": tn.BranchA, "method": "cash", "member_id": member, "items": []any{item(agua, 1)}})
	require.Equal(t, 201, withMember.status, withMember.body)
	assert.Equal(t, member, withMember.data()["member"].(map[string]any)["id"])
	assert.Equal(t, "paid", withMember.data()["status"])
	assert.Equal(t, "MEMBER_INVALID", planFieldCode(h.sell(t, admin, map[string]any{"branch_id": tn.BranchA, "method": "cash", "member_id": foreignMember, "items": []any{item(agua, 1)}}), "member_id"))

	// Validation, field by field.
	valid := func() map[string]any {
		return map[string]any{"branch_id": tn.BranchA, "method": "cash", "items": []any{item(agua, 1)}}
	}
	with := func(key string, value any) map[string]any { b := valid(); b[key] = value; return b }
	for name, body := range map[string]map[string]any{
		"no method": with("method", ""), "bad method": with("method", "bitcoin"), "bad status": with("status", "refunded"),
		"no items": with("items", []any{}), "zero qty": with("items", []any{item(agua, 0)}), "negative qty": with("items", []any{item(agua, -1)}),
		"qty over cap": with("items", []any{item(agua, 100)}), "merged over cap": with("items", []any{item(agua, 60), item(agua, 60)}),
		"foreign product": with("items", []any{item(foreign, 1)}), "unknown product": with("items", []any{item(uuid.NewString(), 1)}),
		"pending sale no member": with("status", "pending"),
	} {
		res := h.sell(t, admin, body)
		assert.Equal(t, 400, res.status, name)
		assert.Equal(t, "VALIDATION_FAILED", res.errorCode(), name)
	}
	assert.Equal(t, "MEMBER_REQUIRED", planFieldCode(h.sell(t, admin, with("status", "pending")), "member_id"))
	// Inactive product, and a sale over the cap (10000 x 99 > 100000).
	require.Equal(t, 200, h.call(t, http.MethodPatch, "/products/"+agua, admin, map[string]any{"is_active": false}).status)
	assert.Equal(t, "PRODUCT_INVALID", planFieldCode(h.sell(t, admin, valid()), "items"))
	huge := h.product(t, admin, "Oro", "10000.00")
	assert.Equal(t, "TOTAL_TOO_LARGE", planFieldCode(h.sell(t, admin, with("items", []any{item(huge, 11)})), "items"))
	assert.Equal(t, 201, h.sell(t, admin, with("items", []any{item(huge, 10)})).status, "exactly 100000.00 fits")

	// Branches: reception cannot sell in a branch it does not reach, nor in another gym's.
	denied := h.sell(t, reception, with("branch_id", hidden))
	_ = denied
	body := with("branch_id", hidden)
	body["items"] = []any{item(batido, 1)}
	res := h.sell(t, reception, body)
	assert.Equal(t, 403, res.status)
	assert.Equal(t, "BRANCH_NOT_ACCESSIBLE", res.errorCode())
	body["items"] = []any{item(batido, 1)}
	assert.Equal(t, 201, h.sell(t, admin, body).status, "the admin reaches every branch of the gym")
	body["branch_id"] = other.BranchA
	assert.Equal(t, 400, h.sell(t, admin, body).status)
	assert.Equal(t, "SCOPE_NOT_ALLOWED", h.sell(t, admin, body).errorCode())

	// Idempotent by id; the id of another gym's sale is a conflict.
	id := uuid.NewString()
	retry := map[string]any{"id": id, "branch_id": tn.BranchA, "method": "cash", "items": []any{item(batido, 2)}}
	assert.Equal(t, 201, h.sell(t, admin, retry).status)
	again := h.sell(t, admin, retry)
	assert.Equal(t, 200, again.status)
	assert.Equal(t, "20.00", again.data()["total"])
	count := h.call(t, http.MethodGet, "/sales?page_size=100", admin, nil)
	n := 0
	for _, raw := range count.body["data"].([]any) {
		if raw.(map[string]any)["id"] == id {
			n++
		}
	}
	assert.Equal(t, 1, n)
	retry["branch_id"] = other.BranchA
	assert.Equal(t, 409, h.sell(t, otherAdmin, map[string]any{"id": id, "branch_id": other.BranchA, "method": "cash", "items": []any{item(foreign, 1)}}).status)
}

func TestPendingSalesBalancesAndCollecting(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	h.setSales(t, tn.GymOne, true)
	h.setSales(t, other.GymOne, true)
	admin, reception, otherAdmin := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, other.Admin)
	batido, agua := h.product(t, admin, "Batido", "8.00"), h.product(t, admin, "Agua", "2.50")
	ana, beto := h.memberID(t, admin, tn.BranchA), h.memberID(t, admin, tn.BranchB)
	hidden := h.extraBranch(t, tn)

	pending := func(token, member string, items ...any) response {
		return h.sell(t, token, map[string]any{"branch_id": tn.BranchA, "status": "pending", "member_id": member, "method": "cash", "items": items})
	}
	f1 := pending(reception, ana, item(batido, 1))
	f2 := pending(admin, ana, item(agua, 2), item(batido, 1))
	f3 := pending(admin, beto, item(agua, 1))
	for _, f := range []response{f1, f2, f3} {
		require.Equal(t, 201, f.status, f.body)
		assert.Equal(t, "pending", f.data()["status"])
		assert.Equal(t, true, f.data()["was_pending"])
		assert.Nil(t, f.data()["method"], "a pending sale has no payment method until it is collected (a method sent is ignored)")
		assert.Nil(t, f.data()["collected_at"])
	}
	paid := h.sell(t, admin, map[string]any{"branch_id": tn.BranchA, "method": "cash", "member_id": ana, "items": []any{item(batido, 5)}})
	require.Equal(t, 201, paid.status)

	// Balance per member: 8.00 + 13.00 = 21.00; the paid sale does not count.
	balance := h.call(t, http.MethodGet, "/members/"+ana+"/pending-sales", reception, nil)
	require.Equal(t, 200, balance.status, balance.body)
	assert.Equal(t, "21.00", balance.data()["balance"])
	assert.Equal(t, float64(2), balance.data()["pending_count"])
	assert.Len(t, balance.data()["sales"], 2)
	list := h.call(t, http.MethodGet, "/sales/pending", reception, nil)
	require.Equal(t, 200, list.status)
	rows := list.body["data"].([]any)
	require.Len(t, rows, 2)
	assert.Equal(t, "21.00", rows[0].(map[string]any)["balance"], "the biggest debt first")
	assert.Equal(t, ana, rows[0].(map[string]any)["member"].(map[string]any)["id"])
	assert.Equal(t, "2.50", rows[1].(map[string]any)["balance"])
	// Purchase history of the member: every sale charged to them, paid and pending, newest first, from any branch.
	history := h.call(t, http.MethodGet, "/members/"+ana+"/sales", reception, nil)
	require.Equal(t, 200, history.status, history.body)
	assert.Equal(t, float64(3), history.body["meta"].(map[string]any)["total"])
	assert.Len(t, history.body["data"], 3)
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/members/"+ana+"/sales", otherAdmin, nil).status)
	assert.Equal(t, 400, h.call(t, http.MethodGet, "/members/"+ana+"/sales?page_size=500", admin, nil).status)
	// Another gym sees nothing of it; its member lookup is a 404.
	assert.Empty(t, h.call(t, http.MethodGet, "/sales/pending", otherAdmin, nil).body["data"])
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/members/"+ana+"/pending-sales", otherAdmin, nil).status)
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/members/"+uuid.NewString()+"/pending-sales", admin, nil).status)
	assert.Equal(t, "0.00", h.call(t, http.MethodGet, "/members/"+h.memberID(t, admin, tn.BranchA)+"/pending-sales", admin, nil).data()["balance"])

	// Collecting: validation, branch access, ownership.
	collect := func(token string, body map[string]any) response {
		return h.call(t, http.MethodPost, "/sales/collections", token, body)
	}
	f1ID, f2ID, f3ID := f1.data()["id"].(string), f2.data()["id"].(string), f3.data()["id"].(string)
	assert.Equal(t, 400, collect(admin, map[string]any{"member_id": ana, "branch_id": tn.BranchA, "method": "bitcoin"}).status)
	assert.Equal(t, 400, collect(admin, map[string]any{"member_id": ana, "branch_id": tn.BranchA, "method": "cash", "sale_ids": []any{uuid.Nil.String()}}).status)
	assert.Equal(t, "BRANCH_NOT_ACCESSIBLE", collect(reception, map[string]any{"member_id": ana, "branch_id": hidden, "method": "cash"}).errorCode())
	assert.Equal(t, 400, collect(admin, map[string]any{"member_id": ana, "branch_id": other.BranchA, "method": "cash"}).status)
	assert.Equal(t, 404, collect(otherAdmin, map[string]any{"member_id": ana, "branch_id": other.BranchA, "method": "cash"}).status, "another gym's member")
	assert.Equal(t, 404, collect(admin, map[string]any{"member_id": ana, "branch_id": tn.BranchA, "method": "cash", "sale_ids": []any{f3ID}}).status, "a pending sale of ANOTHER member is not found")
	assert.Equal(t, 404, collect(admin, map[string]any{"member_id": ana, "branch_id": tn.BranchA, "method": "cash", "sale_ids": []any{uuid.NewString()}}).status)
	assert.Equal(t, "21.00", h.call(t, http.MethodGet, "/members/"+ana+"/pending-sales", admin, nil).data()["balance"], "failed collections change nothing")

	// Collect ONE sale (whole sales only), by reception.
	one := collect(reception, map[string]any{"member_id": ana, "branch_id": tn.BranchA, "method": "yape_plin", "sale_ids": []any{f1ID}})
	require.Equal(t, 201, one.status, one.body)
	assert.Equal(t, "8.00", one.data()["amount"])
	assert.Equal(t, "13.00", one.data()["remaining_balance"])
	assert.Equal(t, "Recepción de Prueba", one.data()["collected_by"].(map[string]any)["full_name"])
	assert.Equal(t, []any{f1ID}, one.data()["sale_ids"])
	assert.Equal(t, "13.00", h.call(t, http.MethodGet, "/members/"+ana+"/pending-sales", admin, nil).data()["balance"])
	// The same sale again: someone already paid it.
	again := collect(admin, map[string]any{"member_id": ana, "branch_id": tn.BranchA, "method": "cash", "sale_ids": []any{f1ID, f2ID}})
	assert.Equal(t, 409, again.status)
	assert.Equal(t, "SALE_NOT_PENDING", again.errorCode())
	assert.Equal(t, "13.00", h.call(t, http.MethodGet, "/members/"+ana+"/pending-sales", admin, nil).data()["balance"], "all or nothing")

	// The collected sale keeps its history.
	sales := h.call(t, http.MethodGet, "/sales?member_id="+ana+"&status=paid&page_size=100", admin, nil)
	var collected map[string]any
	for _, raw := range sales.body["data"].([]any) {
		if raw.(map[string]any)["id"] == f1ID {
			collected = raw.(map[string]any)
		}
	}
	require.NotNil(t, collected)
	assert.Equal(t, "paid", collected["status"])
	assert.Equal(t, true, collected["was_pending"])
	assert.Equal(t, "yape_plin", collected["method"])
	assert.Equal(t, one.data()["id"], collected["collection_id"])
	assert.NotNil(t, collected["collected_at"])

	// Collect all, idempotent by id.
	collectionID := uuid.NewString()
	all := collect(admin, map[string]any{"id": collectionID, "member_id": ana, "branch_id": tn.BranchB, "method": "cash"})
	require.Equal(t, 201, all.status, all.body)
	assert.Equal(t, "13.00", all.data()["amount"])
	assert.Equal(t, "0.00", all.data()["remaining_balance"])
	assert.Equal(t, []any{f2ID}, all.data()["sale_ids"])
	retry := collect(admin, map[string]any{"id": collectionID, "member_id": ana, "branch_id": tn.BranchB, "method": "cash"})
	assert.Equal(t, 200, retry.status, retry.body)
	assert.Equal(t, "13.00", retry.data()["amount"])
	assert.Equal(t, 409, collect(admin, map[string]any{"id": collectionID, "member_id": beto, "branch_id": tn.BranchB, "method": "cash"}).status, "the id belongs to another collection")
	nothing := collect(admin, map[string]any{"member_id": ana, "branch_id": tn.BranchA, "method": "cash"})
	assert.Equal(t, 409, nothing.status)
	assert.Equal(t, "NOTHING_TO_COLLECT", nothing.errorCode())
	assert.Len(t, h.call(t, http.MethodGet, "/sales/pending", admin, nil).body["data"], 1, "only Beto still owes")
	assert.Equal(t, 200, h.call(t, http.MethodGet, "/members/"+beto+"/pending-sales", admin, nil).status)
}

func TestConcurrentCollectionsAndSales(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	h.setSales(t, tn.GymOne, true)
	admin := h.token(t, tn.Admin)
	agua := h.product(t, admin, "Agua", "2.50")
	member := h.memberID(t, admin, tn.BranchA)

	// 10 simultaneous pending sales of different sales add up exactly.
	var wg sync.WaitGroup
	statuses := make(chan int, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- h.sell(t, admin, map[string]any{"branch_id": tn.BranchA, "status": "pending", "member_id": member, "items": []any{item(agua, 2)}}).status
		}()
	}
	wg.Wait()
	close(statuses)
	for s := range statuses {
		assert.Equal(t, 201, s)
	}
	assert.Equal(t, "50.00", h.call(t, http.MethodGet, "/members/"+member+"/pending-sales", admin, nil).data()["balance"])

	// 6 simultaneous "collect all": exactly one wins, the rest find nothing to collect; the amount is paid once.
	results := make(chan response, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- h.call(t, http.MethodPost, "/sales/collections", admin, map[string]any{"member_id": member, "branch_id": tn.BranchA, "method": "cash"})
		}()
	}
	wg.Wait()
	close(results)
	won, lost := 0, 0
	for res := range results {
		switch res.status {
		case 201:
			won++
			assert.Equal(t, "50.00", res.data()["amount"])
		case 409:
			lost++
			assert.Equal(t, "NOTHING_TO_COLLECT", res.errorCode())
		default:
			t.Errorf("unexpected %d %v", res.status, res.body)
		}
	}
	assert.Equal(t, 1, won)
	assert.Equal(t, 5, lost)
	var collections int
	require.NoError(t, h.pool.QueryRow(t.Context(), `SELECT count(*) FROM sale_collections WHERE gym_id = $1`, tn.GymOne).Scan(&collections))
	assert.Equal(t, 1, collections)
}

func TestSalesListScopeAndReport(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	h.setSales(t, tn.GymOne, true)
	h.setSales(t, other.GymOne, true)
	admin, reception, otherAdmin := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, other.Admin)
	batido, agua := h.product(t, admin, "Batido", "8.00"), h.product(t, admin, "Agua", "2.50")
	hidden := h.extraBranch(t, tn)
	member := h.memberID(t, admin, tn.BranchA)
	otherProduct := h.product(t, otherAdmin, "Foreign", "1.00")

	// Membership income before: payments and dashboard must not move with product sales.
	paymentsBefore := h.call(t, http.MethodGet, "/payments", admin, nil)
	dashBefore := h.call(t, http.MethodGet, "/dashboard", admin, nil)
	require.Equal(t, 200, paymentsBefore.status, paymentsBefore.body)
	require.Equal(t, 200, dashBefore.status, dashBefore.body)

	a1 := h.sell(t, admin, map[string]any{"branch_id": tn.BranchA, "method": "cash", "items": []any{item(batido, 3), item(agua, 2)}})          // 29.00
	b1 := h.sell(t, reception, map[string]any{"branch_id": tn.BranchB, "method": "card", "items": []any{item(agua, 4)}})                       // 10.00
	d1 := h.sell(t, admin, map[string]any{"branch_id": hidden, "method": "cash", "items": []any{item(batido, 1)}})                             // 8.00
	f1 := h.sell(t, admin, map[string]any{"branch_id": tn.BranchA, "status": "pending", "member_id": member, "items": []any{item(batido, 2)}}) // 16.00
	for _, r := range []response{a1, b1, d1, f1} {
		require.Equal(t, 201, r.status, r.body)
	}
	h.sell(t, otherAdmin, map[string]any{"branch_id": other.BranchA, "method": "cash", "items": []any{item(otherProduct, 5)}})

	// Listing: the admin sees the whole gym, reception only its branches, never another gym's.
	all := h.call(t, http.MethodGet, "/sales", admin, nil)
	assert.Equal(t, float64(4), all.body["meta"].(map[string]any)["total"])
	mine := h.call(t, http.MethodGet, "/sales", reception, nil)
	assert.Equal(t, float64(3), mine.body["meta"].(map[string]any)["total"], "the hidden branch's sale is not listed")
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/sales?branch_id="+hidden.String(), reception, nil).status)
	assert.Equal(t, 1, int(h.call(t, http.MethodGet, "/sales?branch_id="+tn.BranchB.String(), reception, nil).body["meta"].(map[string]any)["total"].(float64)))
	assert.Equal(t, 1, int(h.call(t, http.MethodGet, "/sales?status=pending", admin, nil).body["meta"].(map[string]any)["total"].(float64)))
	assert.Equal(t, 400, h.call(t, http.MethodGet, "/sales?status=x", admin, nil).status)
	assert.Equal(t, 400, h.call(t, http.MethodGet, "/sales?from=yesterday", admin, nil).status)
	assert.Equal(t, 400, h.call(t, http.MethodGet, "/sales?page_size=1000", admin, nil).status)
	assert.Equal(t, float64(1), h.call(t, http.MethodGet, "/sales", otherAdmin, nil).body["meta"].(map[string]any)["total"])

	// Report: admin only.
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/sales/report", reception, nil).status)
	report := h.call(t, http.MethodGet, "/sales/report", admin, nil)
	require.Equal(t, 200, report.status, report.body)
	totals := report.data()["totals"].(map[string]any)
	assert.Equal(t, float64(4), totals["sales_count"])
	assert.Equal(t, float64(3+2+4+1+2), totals["units"])
	assert.Equal(t, "63.00", totals["total_sold"])
	assert.Equal(t, "47.00", totals["collected_total"])
	assert.Equal(t, "16.00", totals["pending_total"], "the open pending sales")
	byProduct := report.data()["by_product"].([]any)
	require.Len(t, byProduct, 2)
	// Both sold 6 units: ties are ordered by name.
	assert.Equal(t, "Agua", byProduct[0].(map[string]any)["name"])
	assert.Equal(t, float64(6), byProduct[0].(map[string]any)["units"])
	assert.Equal(t, "15.00", byProduct[0].(map[string]any)["total"])
	assert.Equal(t, "Batido", byProduct[1].(map[string]any)["name"])
	assert.Equal(t, float64(6), byProduct[1].(map[string]any)["units"])
	assert.Equal(t, "48.00", byProduct[1].(map[string]any)["total"])
	today := iso(limaToday())
	byDay := report.data()["by_period"].([]any)
	require.Len(t, byDay, 1)
	assert.Equal(t, today, byDay[0].(map[string]any)["period"])
	assert.Equal(t, "63.00", byDay[0].(map[string]any)["total"])

	month := h.call(t, http.MethodGet, "/sales/report?group_by=month&from="+iso(limaToday().AddDate(0, -2, 0))+"&to="+today, admin, nil)
	require.Equal(t, 200, month.status, month.body)
	assert.Equal(t, today[:7], month.data()["by_period"].([]any)[0].(map[string]any)["period"])

	branch := h.call(t, http.MethodGet, "/sales/report?branch_id="+tn.BranchB.String(), admin, nil)
	assert.Equal(t, "10.00", branch.data()["totals"].(map[string]any)["total_sold"])
	empty := h.call(t, http.MethodGet, "/sales/report?from=2001-01-01&to=2001-01-31", admin, nil)
	assert.Equal(t, "0.00", empty.data()["totals"].(map[string]any)["total_sold"])
	assert.Empty(t, empty.data()["by_product"])
	for _, q := range []string{"group_by=year", "from=2026-02-01&to=2026-01-01", "from=2020-01-01&to=2026-01-01", "from=nope"} {
		assert.Equal(t, 400, h.call(t, http.MethodGet, "/sales/report?"+q, admin, nil).status, q)
	}
	assert.Equal(t, 400, h.call(t, http.MethodGet, "/sales/report?branch_id="+other.BranchA.String(), admin, nil).status, "another gym's branch")
	oreport := h.call(t, http.MethodGet, "/sales/report", otherAdmin, nil)
	assert.Equal(t, "5.00", oreport.data()["totals"].(map[string]any)["total_sold"])

	// Collecting a pending sale keeps the total sold and moves it from pending to collected.
	require.Equal(t, 201, h.call(t, http.MethodPost, "/sales/collections", admin, map[string]any{"member_id": member, "branch_id": tn.BranchA, "method": "cash"}).status)
	after := h.call(t, http.MethodGet, "/sales/report", admin, nil).data()["totals"].(map[string]any)
	assert.Equal(t, "63.00", after["total_sold"])
	assert.Equal(t, "63.00", after["collected_total"])
	assert.Equal(t, "0.00", after["pending_total"])

	// Product sales never touch the membership income.
	paymentsAfter := h.call(t, http.MethodGet, "/payments", admin, nil)
	dashAfter := h.call(t, http.MethodGet, "/dashboard", admin, nil)
	assert.Equal(t, fmt.Sprint(paymentsBefore.body), fmt.Sprint(paymentsAfter.body))
	assert.Equal(t, fmt.Sprint(dashBefore.body), fmt.Sprint(dashAfter.body))
}
