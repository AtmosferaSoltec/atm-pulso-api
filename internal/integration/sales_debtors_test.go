package integration_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// namedMember enrolls a member of the gym with the given name and returns its id and document.
func (h harness) namedMember(t *testing.T, admin string, branch uuid.UUID, name string) (string, string) {
	t.Helper()
	document := uniqueDocument()
	body := enrollBody(branch, document)
	body["full_name"] = name
	res := h.enroll(t, admin, body)
	require.Equal(t, 201, res.status, res.body)
	return memberIDOf(res), document
}

func debtorRows(t *testing.T, res response) []map[string]any {
	t.Helper()
	require.Equal(t, 200, res.status, res.body)
	var out []map[string]any
	for _, raw := range res.body["data"].([]any) {
		out = append(out, raw.(map[string]any))
	}
	return out
}

func debtorNames(rows []map[string]any) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r["member"].(map[string]any)["full_name"].(string))
	}
	return out
}

func TestSalesDebtorsListReachOrderSearchAndPaging(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	admin, reception, otherAdmin := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, other.Admin)

	// The module off answers FEATURE_DISABLED to both roles.
	for _, token := range []string{admin, reception} {
		off := h.call(t, http.MethodGet, "/sales/debtors", token, nil)
		assert.Equal(t, 403, off.status)
		assert.Equal(t, "FEATURE_DISABLED", off.errorCode())
	}
	assert.Equal(t, 401, h.call(t, http.MethodGet, "/sales/debtors", "", nil).status)
	h.setSales(t, tn.GymOne, true)
	h.setSales(t, other.GymOne, true)
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/sales/debtors", h.superToken(t), nil).status, "the superadmin is not staff of the gym")

	batido, agua := h.product(t, admin, "Batido", "8.00"), h.product(t, admin, "Agua", "2.50")
	hidden := h.extraBranch(t, tn) // reception does not work there
	ramirez, ramirezDoc := h.namedMember(t, admin, tn.BranchA, "José Ramírez")
	lucia, _ := h.namedMember(t, admin, tn.BranchB, "Lucía Torres")
	beto, _ := h.namedMember(t, admin, tn.BranchA, "Beto Pérez")
	ana, _ := h.namedMember(t, admin, tn.BranchA, "Ana Collected")
	_, _ = h.namedMember(t, admin, tn.BranchA, "Carla SinDeuda")
	foreign, _ := h.namedMember(t, otherAdmin, other.BranchA, "José Ramírez Otro")
	otherProduct := h.product(t, otherAdmin, "Foreign", "1.00")

	pendingSale := func(token string, branch uuid.UUID, member string, items ...any) string {
		res := h.sell(t, token, map[string]any{"branch_id": branch, "status": "pending", "member_id": member, "items": items})
		require.Equal(t, 201, res.status, res.body)
		return res.data()["id"].(string)
	}
	setSoldAt := func(sale string, at time.Time) {
		_, err := h.pool.Exec(t.Context(), `UPDATE sales SET sold_at = $2, sold_on = ($2 AT TIME ZONE 'America/Lima')::date WHERE id = $1`, sale, at)
		require.NoError(t, err)
	}
	base := time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC)
	// Ramírez owes two sales (36.00), the oldest of them sold in a branch reception does not reach.
	setSoldAt(pendingSale(admin, hidden, ramirez, item(batido, 2)), base.Add(48*time.Hour))
	setSoldAt(pendingSale(reception, tn.BranchA, ramirez, item(batido, 2), item(agua, 1)), base.Add(72*time.Hour)) // 18.50
	// Lucía owes the oldest sale; Beto ties with Ramírez's oldest and is ordered by name before him.
	setSoldAt(pendingSale(reception, tn.BranchB, lucia, item(agua, 1)), base)
	setSoldAt(pendingSale(admin, tn.BranchA, beto, item(agua, 2)), base.Add(48*time.Hour))
	// Ana's pending sale is collected: she is not a debtor. A paid sale never counts either.
	pendingSale(admin, tn.BranchA, ana, item(batido, 1))
	require.Equal(t, 201, h.call(t, http.MethodPost, "/sales/collections", admin, map[string]any{"member_id": ana, "branch_id": tn.BranchA, "method": "cash"}).status)
	require.Equal(t, 201, h.sell(t, admin, map[string]any{"branch_id": tn.BranchA, "method": "cash", "member_id": beto, "items": []any{item(batido, 9)}}).status)
	// Another gym's debtor.
	require.Equal(t, 201, h.sell(t, otherAdmin, map[string]any{"branch_id": other.BranchA, "status": "pending", "member_id": foreign, "items": []any{item(otherProduct, 1)}}).status)

	// Shape and order: the oldest debt first, ties by name.
	res := h.call(t, http.MethodGet, "/sales/debtors", admin, nil)
	rows := debtorRows(t, res)
	assert.Equal(t, []string{"Lucía Torres", "Beto Pérez", "José Ramírez"}, debtorNames(rows))
	assert.Equal(t, map[string]any{"page": float64(1), "page_size": float64(50), "total": float64(3)}, res.body["meta"])
	first := rows[2]
	assert.Equal(t, map[string]any{"id": ramirez, "full_name": "José Ramírez", "document_type": "dni", "document_number": ramirezDoc, "phone": "999111222"}, first["member"])
	assert.Equal(t, "34.50", first["balance"], "16.00 + 18.50")
	assert.Equal(t, float64(2), first["pending_count"])
	assert.Equal(t, base.Add(48*time.Hour).Format(time.RFC3339), first["oldest_sold_at"])
	assert.Equal(t, "2026-10-03", first["oldest_sold_on"])
	assert.Len(t, first, 5)
	assert.Equal(t, "5.00", rows[1]["balance"], "Beto's paid sale does not count")
	assert.Equal(t, "2026-10-01", rows[0]["oldest_sold_on"])

	// Reach: like pending-sales, balances are the gym's; reception sees the same list (hidden branch included).
	assert.Equal(t, debtorNames(rows), debtorNames(debtorRows(t, h.call(t, http.MethodGet, "/sales/debtors", reception, nil))))
	assert.Equal(t, "34.50", debtorRows(t, h.call(t, http.MethodGet, "/sales/debtors", reception, nil))[2]["balance"])

	// Another gym only sees its own.
	assert.Equal(t, []string{"José Ramírez Otro"}, debtorNames(debtorRows(t, h.call(t, http.MethodGet, "/sales/debtors", otherAdmin, nil))))

	// Search: accent and case insensitive on the name, or part of the document.
	search := func(q string) []string {
		return debtorNames(debtorRows(t, h.call(t, http.MethodGet, "/sales/debtors?q="+url.QueryEscape(q), reception, nil)))
	}
	assert.Equal(t, []string{"José Ramírez"}, search("ramirez"))
	assert.Equal(t, []string{"Lucía Torres"}, search("LUCIA"))
	assert.Equal(t, []string{"Beto Pérez", "José Ramírez"}, search("EZ"))
	assert.Equal(t, []string{"José Ramírez"}, search(ramirezDoc[2:]))
	assert.Equal(t, []string{}, search("Carla"), "a member without debt is never listed")
	assert.Equal(t, []string{}, search("Ana Collected"))
	filtered := h.call(t, http.MethodGet, "/sales/debtors?q=ramirez", admin, nil)
	assert.Equal(t, float64(1), filtered.body["meta"].(map[string]any)["total"])

	// Paging: total counts every debtor; pages follow the order.
	page := func(query string) response { return h.call(t, http.MethodGet, "/sales/debtors?"+query, admin, nil) }
	p1, p2, p3 := page("page_size=2"), page("page_size=2&page=2"), page("page_size=2&page=3")
	assert.Equal(t, []string{"Lucía Torres", "Beto Pérez"}, debtorNames(debtorRows(t, p1)))
	assert.Equal(t, []string{"José Ramírez"}, debtorNames(debtorRows(t, p2)))
	assert.Equal(t, []string{}, debtorNames(debtorRows(t, p3)))
	assert.Equal(t, map[string]any{"page": float64(2), "page_size": float64(2), "total": float64(3)}, p2.body["meta"])
	for _, bad := range []string{"page=0", "page_size=0", "page_size=101", "page=x"} {
		res := page(bad)
		assert.Equal(t, 400, res.status, bad)
		assert.Equal(t, "VALIDATION_FAILED", res.errorCode(), bad)
	}
	assert.Equal(t, 200, page("page_size=100").status)

	// Collecting all of Lucía's debt takes her off the list.
	require.Equal(t, 201, h.call(t, http.MethodPost, "/sales/collections", reception, map[string]any{"member_id": lucia, "branch_id": tn.BranchB, "method": "cash"}).status)
	assert.Equal(t, []string{"Beto Pérez", "José Ramírez"}, debtorNames(debtorRows(t, page(""))))

	// Turned off again: immediate.
	h.setSales(t, tn.GymOne, false)
	assert.Equal(t, "FEATURE_DISABLED", h.call(t, http.MethodGet, "/sales/debtors", reception, nil).errorCode())
}
