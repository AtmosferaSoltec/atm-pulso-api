package integration_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
)

// TestGymJourney walks the product from the console to the reports, only through the API: the
// superadmin enables a company, its admin enters, hires reception, reception sells.
func TestGymJourney(t *testing.T) {
	h := newHarness(t)
	client, tenantID := uuid.New(), uuid.New()
	h.cleanupClient(t, client)

	// 1. In the console a client gets a company with Pulso; the superadmin adds a second branch.
	h.auth.SetTenant(authapi.Tenant{ID: tenantID.String(), RUC: "20444444444", DisplayName: "Gym Recorrido", ClientID: client.String(), ClientName: "Cliente Recorrido"})
	super := h.superToken(t)
	listed := h.call(t, http.MethodGet, "/admin/groups", super, nil)
	require.Equal(t, 200, listed.status, listed.body)
	_, gym := gymOfCompany(t, listed, tenantID)
	require.NotNil(t, gym)
	firstBranch := gym["branches"].([]any)[0].(map[string]any)["id"].(string)
	second := h.call(t, http.MethodPost, "/admin/gyms/"+gym["id"].(string)+"/branches", super, map[string]any{"name": "Sede Segunda"})
	require.Equal(t, 201, second.status, second.body)

	// 2. The console gave a person the admin role: they enter, see both branches and hire reception.
	adminDNI, adminAuth := uniqueDocument(), uuid.New()
	h.auth.SetMember(tenantID.String(), authapi.Member{UserID: adminAuth.String(), DNI: adminDNI, FullName: "Ana Admin", Role: "admin", Status: "active"})
	h.people.Store(adminDNI, person{AuthUserID: adminAuth, Document: adminDNI, Name: "Ana Admin", Role: "admin", ClientID: client, ClientName: "Cliente Recorrido", TenantID: tenantID})
	admin := h.token(t, adminDNI)
	me := h.call(t, http.MethodGet, "/me", admin, nil)
	require.Equal(t, 200, me.status, me.body)
	assert.Len(t, me.data()["branches"], 2)

	hired := h.call(t, http.MethodPost, "/users", admin, map[string]any{
		"full_name": "Rosa Recepción", "document_number": uniqueDocument(), "role": "receptionist", "branch_ids": []string{firstBranch}})
	require.Equal(t, 201, hired.status, hired.body)
	tn := tenant{ClientID: client, GroupName: "Cliente Recorrido"}
	receptionDNI := h.hired(t, tn, tenantID, hired)

	// 3. Reception enrolls a member, checks them in and sells a day pass in its branch only.
	reception := h.token(t, receptionDNI)
	memberDNI := uniqueDocument()
	sale := h.enroll(t, reception, map[string]any{"branch_id": firstBranch, "document_type": "dni", "document_number": memberDNI,
		"full_name": "Marco Miembro", "phone": "988111222", "months": 3, "amount": "270", "method": "yape_plin"})
	require.Equal(t, 201, sale.status, sale.body)
	assert.Equal(t, "R-000001", sale.data()["receipt"].(map[string]any)["number"])
	assert.Equal(t, "BRANCH_NOT_ACCESSIBLE", h.enroll(t, reception, enrollBody(uuidOf(t, second.data()["id"].(string)), uniqueDocument())).errorCode())

	entry := h.check(t, reception, uuidOf(t, firstBranch), memberDNI)
	require.Equal(t, 200, entry.status, entry.body)
	assert.Equal(t, true, entry.data()["allowed"])

	pass := h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": firstBranch, "method": "cash"})
	require.Equal(t, 201, pass.status, pass.body)
	assert.Equal(t, "D-000001", pass.data()["receipt"].(map[string]any)["number"])

	// 4. The admin reads the payments and the dashboard: the numbers add up.
	payments := h.call(t, http.MethodGet, "/payments", admin, nil)
	require.Equal(t, 200, payments.status, payments.body)
	totals := payments.body["meta"].(map[string]any)["totals"].(map[string]any)
	assert.Equal(t, "285.00", totals["amount"], "270 membership + 15 day pass")
	assert.Equal(t, 2.0, totals["count"])
	dashboard := h.call(t, http.MethodGet, "/dashboard", admin, nil)
	require.Equal(t, 200, dashboard.status, dashboard.body)
	assert.Equal(t, 1.0, dashboard.data()["members"].(map[string]any)["active"])
	assert.Equal(t, "285.00", dashboard.data()["income"].(map[string]any)["month"])
	assert.Equal(t, 1.0, dashboard.data()["access"].(map[string]any)["allowed_today"])
}

func uuidOf(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	require.NoError(t, err)
	return id
}
