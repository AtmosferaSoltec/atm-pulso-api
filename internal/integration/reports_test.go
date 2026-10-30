package integration_test

import (
	"encoding/csv"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (h harness) rawGet(t *testing.T, path, token string) (int, http.Header, string) {
	t.Helper()
	res := h.callRaw(t, http.MethodGet, path, token)
	return res.Code, res.Header(), res.Body.String()
}

func TestPaymentsListTotalsAndFilters(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	admin, adminTwo := h.token(t, tn.Admin), h.token(t, tn.AdminTwo)
	reception := h.token(t, tn.Receptionist)
	today := limaToday()

	sale := func(token, branch, method, amount, name string) {
		t.Helper()
		body := enrollBody(tn.BranchA, uniqueDocument())
		body["branch_id"], body["method"], body["amount"], body["full_name"], body["months"] = branch, method, amount, name, 1
		require.Equal(t, 201, h.enroll(t, token, body).status)
	}
	sale(admin, tn.BranchA.String(), "cash", "100", "Cliente Efectivo")
	sale(admin, tn.BranchA.String(), "yape_plin", "190.50", "Cliente Yape")
	sale(adminTwo, tn.BranchC.String(), "card", "50", "Cliente Tarjeta")
	require.Equal(t, 201, h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": tn.BranchA, "method": "cash"}).status)

	res := h.call(t, http.MethodGet, "/payments", admin, nil)
	require.Equal(t, 200, res.status, res.body)
	meta := res.body["meta"].(map[string]any)
	assert.Equal(t, 3.0, meta["total"], "the card sale belongs to the other gym")
	totals := meta["totals"].(map[string]any)
	assert.Equal(t, "305.50", totals["amount"])
	assert.Equal(t, 3.0, totals["count"])
	assert.Equal(t, 1.0, totals["day_passes"])
	assert.Equal(t, "101.83", totals["average"])
	assert.Equal(t, "cash", totals["top_method"])
	byMethod := totals["by_method"].(map[string]any)
	assert.Equal(t, map[string]any{"yape_plin": "190.50", "cash": "115.00", "card": "0.00", "transfer": "0.00"}, byMethod)
	rows := res.body["data"].([]any)
	assert.Len(t, rows, 3)
	assert.Equal(t, iso(today), rows[0].(map[string]any)["paid_on"])

	filtered := func(query string) map[string]any {
		r := h.call(t, http.MethodGet, "/payments"+query, admin, nil)
		require.Equal(t, 200, r.status, r.body)
		return r.body
	}
	assert.Equal(t, 2.0, filtered("?method=cash")["meta"].(map[string]any)["total"])
	assert.Equal(t, 1.0, filtered("?kind=day_pass")["meta"].(map[string]any)["total"])
	assert.Equal(t, 1.0, filtered("?q=yape")["meta"].(map[string]any)["total"])
	assert.Equal(t, 0.0, filtered("?q=tarjeta")["meta"].(map[string]any)["total"], "the card sale is in the other gym")
	assert.Equal(t, 3.0, filtered("?gym_id=" + tn.GymOne.String())["meta"].(map[string]any)["total"])
	assert.Equal(t, 1.0, filtered("?method=cash&kind=membership")["meta"].(map[string]any)["total"])
	assert.Equal(t, 0.0, filtered("?from=" + iso(today.AddDate(0, 0, -5)) + "&to=" + iso(today.AddDate(0, 0, -1)))["meta"].(map[string]any)["total"])
	paged := filtered("?page=2&page_size=2")
	assert.Len(t, paged["data"], 1)
	assert.Equal(t, 3.0, paged["meta"].(map[string]any)["totals"].(map[string]any)["count"], "totals cover the whole filter, not the page")

	// Reach: an admin only the gym of their token, reception its branches and no totals; nobody sees another tenant.
	assert.Equal(t, 1.0, h.call(t, http.MethodGet, "/payments", adminTwo, nil).body["meta"].(map[string]any)["total"], "the other gym's admin sees their sale")
	assert.Equal(t, 1.0, h.call(t, http.MethodGet, "/payments?branch_id="+tn.BranchC.String(), adminTwo, nil).body["meta"].(map[string]any)["total"])
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/payments?branch_id="+tn.BranchC.String(), admin, nil).status, "a branch of the other gym is out of reach")
	recepRes := h.call(t, http.MethodGet, "/payments", reception, nil)
	assert.Equal(t, 200, recepRes.status)
	assert.Equal(t, 3.0, recepRes.body["meta"].(map[string]any)["total"])
	assert.NotContains(t, recepRes.body["meta"], "totals", "reception must not get KPIs")
	assert.Equal(t, 0.0, h.call(t, http.MethodGet, "/payments", h.token(t, other.Admin), nil).body["meta"].(map[string]any)["total"])
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/payments?branch_id="+tn.BranchC.String(), reception, nil).status)

	for _, bad := range []string{"?method=bitcoin", "?kind=x", "?from=2026-13-01", "?from=" + iso(today) + "&to=" + iso(today.AddDate(0, 0, -1)), "?from=2020-01-01&to=2026-01-01"} {
		assert.Equal(t, 400, h.call(t, http.MethodGet, "/payments"+bad, admin, nil).status, bad)
	}
}

func TestPaymentsCSVExport(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin := h.token(t, tn.Admin)
	reception := h.token(t, tn.Receptionist)

	body := enrollBody(tn.BranchA, uniqueDocument())
	body["full_name"], body["method"] = "=HYPERLINK(1) Pérez, Juan", "yape_plin"
	require.Equal(t, 201, h.enroll(t, admin, body).status)
	require.Equal(t, 201, h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": tn.BranchA, "method": "cash"}).status)

	code, header, raw := h.rawGet(t, "/payments/export", admin)
	require.Equal(t, 200, code, raw)
	assert.Contains(t, header.Get("Content-Type"), "text/csv")
	assert.Contains(t, header.Get("Content-Disposition"), "attachment")
	require.True(t, strings.HasPrefix(raw, "\xEF\xBB\xBF"), "UTF-8 BOM so Excel opens it")
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(raw, "\xEF\xBB\xBF"))).ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 3, "header plus two payments")
	assert.Equal(t, []string{"Boleta", "Miembro", "Documento", "Plan", "Importe", "Medio de pago", "Fecha"}, records[0])
	var membership []string
	for _, r := range records[1:] {
		if strings.HasPrefix(r[0], "R-") {
			membership = r
		}
	}
	assert.Equal(t, "'=HYPERLINK(1) Pérez, Juan", membership[1], "formula injection is neutralized and commas are escaped")
	assert.Equal(t, "3 meses", membership[3])
	assert.Equal(t, "Yape/Plin", membership[5])

	code, _, _ = h.rawGet(t, "/payments/export", reception)
	assert.Equal(t, 403, code, "reception cannot download")
	code, _, _ = h.rawGet(t, "/payments/export?method=nope", admin)
	assert.Equal(t, 400, code)
}

func TestDashboardFiguresAndRoles(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	admin, adminTwo := h.token(t, tn.Admin), h.token(t, tn.AdminTwo)
	reception := h.token(t, tn.Receptionist)
	today := limaToday()

	// A new organization shows zeros, not sample data.
	empty := h.call(t, http.MethodGet, "/dashboard", h.token(t, other.Admin), nil)
	require.Equal(t, 200, empty.status, empty.body)
	assert.Equal(t, iso(today), empty.data()["date"])
	assert.Equal(t, map[string]any{"active": 0.0, "expiring": 0.0, "expired": 0.0, "scheduled": 0.0}, empty.data()["members"])
	assert.Equal(t, map[string]any{"today": "0.00", "month": "0.00"}, empty.data()["income"])
	assert.Empty(t, empty.data()["expiring_soon"])

	h.memberWith(t, admin, tn.BranchA, today.AddDate(0, -1, 0), 3, nil)
	soon := h.memberWith(t, admin, tn.BranchA, today.AddDate(0, -1, 0), 1, map[string]any{"full_name": "Por Vencer Uno"})
	h.memberWith(t, admin, tn.BranchA, today.AddDate(0, -6, 0), 1, nil)
	h.memberWith(t, adminTwo, tn.BranchC, today.AddDate(0, -1, 0), 1, nil)
	h.check(t, admin, tn.BranchA, soon)
	h.check(t, admin, tn.BranchA, uniqueDocument())

	res := h.call(t, http.MethodGet, "/dashboard", admin, nil)
	require.Equal(t, 200, res.status, res.body)
	assert.Equal(t, map[string]any{"active": 1.0, "expiring": 1.0, "expired": 1.0, "scheduled": 0.0}, res.data()["members"], "the member of the other gym is not counted")
	assert.Equal(t, map[string]any{"today": "810.00", "month": "810.00"}, res.data()["income"], "3 sales of 270 in this gym")
	assert.Equal(t, map[string]any{"allowed_today": 1.0, "denied_today": 1.0}, res.data()["access"])
	soonList := res.data()["expiring_soon"].([]any)
	assert.Len(t, soonList, 1)
	assert.Equal(t, "Por Vencer Uno", soonList[0].(map[string]any)["full_name"])
	assert.NotEmpty(t, soonList[0].(map[string]any)["phone"])

	otherGym := h.call(t, http.MethodGet, "/dashboard", adminTwo, nil)
	require.Equal(t, 200, otherGym.status, otherGym.body)
	assert.Equal(t, 1.0, otherGym.data()["members"].(map[string]any)["expiring"])
	assert.Equal(t, map[string]any{"today": "270.00", "month": "270.00"}, otherGym.data()["income"])
	assert.Equal(t, "Ana Prueba", otherGym.data()["expiring_soon"].([]any)[0].(map[string]any)["full_name"])
	assert.Equal(t, 1.0, h.call(t, http.MethodGet, "/dashboard?gym_id="+tn.GymTwo.String(), adminTwo, nil).data()["members"].(map[string]any)["expiring"])
	assert.Equal(t, 1.0, h.call(t, http.MethodGet, "/dashboard?branch_id="+tn.BranchA.String(), admin, nil).data()["members"].(map[string]any)["expiring"])
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/dashboard", reception, nil).status)
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/dashboard?branch_id="+tn.BranchC.String(), admin, nil).status)
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/dashboard?gym_id="+tn.GymTwo.String(), admin, nil).status)
}
