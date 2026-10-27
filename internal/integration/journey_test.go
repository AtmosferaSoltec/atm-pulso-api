package integration_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// firstLogin signs in with the temporary password (the DNI), sets a real one and signs in again.
func (h harness) firstLogin(t *testing.T, document, newPassword string) string {
	t.Helper()
	temporary := h.token(t, document, document)
	changed := h.call(t, http.MethodPost, "/me/password", temporary, map[string]string{"current_password": document, "new_password": newPassword})
	require.Equal(t, 200, changed.status, changed.body)
	assert.Equal(t, true, changed.data()["login_required"])
	return h.token(t, document, newPassword)
}

// TestOwnerJourney walks the product from the first superadmin to the reports, only through the API.
func TestOwnerJourney(t *testing.T) {
	h := newHarness(t)

	// 1. The superadmin (inserted by hand in production) creates a client.
	super := h.token(t, h.newSuperadmin(t), demoPassword)
	input := h.clientInput(t)
	created := h.call(t, http.MethodPost, "/clients", super, input)
	require.Equal(t, 201, created.status, created.body)
	assert.Equal(t, "free", created.data()["plan"].(map[string]any)["state"])

	// 2. The owner signs in for the first time, sees its catalog and creates a receptionist.
	owner := h.firstLogin(t, input.Owner.DocumentNumber, "ClaveDelDueño1")
	catalog := h.call(t, http.MethodGet, "/catalog", owner, nil)
	require.Equal(t, 200, catalog.status, catalog.body)
	gym := catalog.data()["gyms"].([]any)[0].(map[string]any)
	branch := gym["branches"].([]any)[0].(map[string]any)["id"].(string)

	receptionistDNI := uniqueDocument()
	hired := h.call(t, http.MethodPost, "/users", owner, map[string]any{
		"id": uuid.New(), "full_name": "Rosa Recepción", "document_number": receptionistDNI, "role": "receptionist", "branch_ids": []string{branch}})
	require.Equal(t, 201, hired.status, hired.body)
	t.Cleanup(func() { _, _ = h.pool.Exec(t.Context(), `DELETE FROM users WHERE document_number=$1`, receptionistDNI) })

	// 3. Reception enrolls a member, checks them in and sells a day pass.
	reception := h.firstLogin(t, receptionistDNI, "ClaveRecepcion1")
	memberDNI := uniqueDocument()
	sale := h.enroll(t, reception, map[string]any{"branch_id": branch, "document_type": "dni", "document_number": memberDNI,
		"full_name": "Marco Miembro", "phone": "988111222", "months": 3, "amount": "270", "method": "yape_plin"})
	require.Equal(t, 201, sale.status, sale.body)
	assert.Equal(t, "R-000001", sale.data()["receipt"].(map[string]any)["number"])

	entry := h.check(t, reception, uuidOf(t, branch), memberDNI)
	require.Equal(t, 200, entry.status, entry.body)
	assert.Equal(t, true, entry.data()["allowed"])

	pass := h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": branch, "method": "cash"})
	require.Equal(t, 201, pass.status, pass.body)
	assert.Equal(t, "D-000001", pass.data()["receipt"].(map[string]any)["number"])

	// 4. The owner reads the payments and the dashboard: the numbers add up.
	payments := h.call(t, http.MethodGet, "/payments", owner, nil)
	require.Equal(t, 200, payments.status, payments.body)
	totals := payments.body["meta"].(map[string]any)["totals"].(map[string]any)
	assert.Equal(t, "285.00", totals["amount"], "270 membership + 15 day pass")
	assert.Equal(t, 2.0, totals["count"])
	dashboard := h.call(t, http.MethodGet, "/dashboard", owner, nil)
	require.Equal(t, 200, dashboard.status, dashboard.body)
	assert.Equal(t, 1.0, dashboard.data()["members"].(map[string]any)["active"])
	assert.Equal(t, "285.00", dashboard.data()["income"].(map[string]any)["month"])
	assert.Equal(t, 1.0, dashboard.data()["access"].(map[string]any)["allowed_today"])

	// 5. The superadmin can look but not touch.
	view := h.inspect(t, "/payments", super, input.ID.String())
	assert.Equal(t, 200, view.status)
	assert.Equal(t, 403, h.enroll(t, super, map[string]any{"branch_id": branch}).status)
}

func uuidOf(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	require.NoError(t, err)
	return id
}
