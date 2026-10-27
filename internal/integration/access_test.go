package integration_test

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (h harness) check(t *testing.T, token string, branch uuid.UUID, document string) response {
	t.Helper()
	return h.call(t, http.MethodPost, "/access/checks", token, map[string]any{"branch_id": branch, "document_number": document})
}

func (h harness) memberWith(t *testing.T, token string, branch uuid.UUID, start time.Time, months int, extra map[string]any) string {
	t.Helper()
	document := uniqueDocument()
	body := enrollBody(branch, document)
	body["starts_on"], body["months"] = iso(start), months
	for k, v := range extra {
		body[k] = v
	}
	require.Equal(t, 201, h.enroll(t, token, body).status)
	return document
}

func TestAccessDecisionsAreServerSideAndLogged(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	owner := h.token(t, tn.Owner, demoPassword)
	reception := h.token(t, tn.Receptionist, demoPassword)
	today := limaToday()

	valid := h.memberWith(t, owner, tn.BranchA, today.AddDate(0, -1, 0), 3, nil)
	expiring := h.memberWith(t, owner, tn.BranchA, today.AddDate(0, -1, 0), 1, nil)
	expired := h.memberWith(t, owner, tn.BranchA, today.AddDate(0, -6, 0), 1, nil)
	future := h.memberWith(t, owner, tn.BranchA, today.AddDate(0, 0, 10), 1, nil)

	res := h.check(t, reception, tn.BranchA, valid)
	require.Equal(t, 200, res.status, res.body)
	assert.Equal(t, true, res.data()["allowed"])
	assert.Equal(t, "MEMBERSHIP_VALID", res.data()["reason"])
	assert.Equal(t, valid, res.data()["member"].(map[string]any)["document_number"])
	assert.Nil(t, res.data()["expiring_in_days"])

	soon := h.check(t, reception, tn.BranchA, expiring)
	assert.Equal(t, true, soon.data()["allowed"])
	assert.NotNil(t, soon.data()["expiring_in_days"], "an allowed entry that ends within 7 days carries a warning")

	for document, reason := range map[string]string{expired: "MEMBERSHIP_EXPIRED", future: "MEMBERSHIP_NOT_STARTED", uniqueDocument(): "NOT_REGISTERED"} {
		denied := h.check(t, reception, tn.BranchA, document)
		assert.Equal(t, 200, denied.status, "a denied check is still a successful request")
		assert.Equal(t, false, denied.data()["allowed"], reason)
		assert.Equal(t, reason, denied.data()["reason"])
	}
	assert.NotNil(t, h.check(t, reception, tn.BranchA, expired).data()["ends_on"])

	// Every attempt is logged: 2 allowed + 3 denied + the extra expired repeat = 6 entries.
	var entries int
	require.NoError(t, h.pool.QueryRow(t.Context(), `SELECT count(*) FROM access_entries WHERE organization_id=$1`, tn.OrganizationID).Scan(&entries))
	assert.Equal(t, 6, entries)
}

func TestAccessBranchRulesAndSharing(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	owner := h.token(t, tn.Owner, demoPassword)
	reception := h.token(t, tn.Receptionist, demoPassword)
	today := limaToday()

	// Gym scope opens A and B; the default scope opens only the home branch.
	gym := h.memberWith(t, owner, tn.BranchA, today, 3, map[string]any{"access_scope": "gym"})
	onlyHome := h.memberWith(t, owner, tn.BranchA, today, 3, nil)
	assert.Equal(t, true, h.check(t, reception, tn.BranchB, gym).data()["allowed"])
	denied := h.check(t, reception, tn.BranchB, onlyHome)
	assert.Equal(t, false, denied.data()["allowed"])
	assert.Equal(t, "BRANCH_NOT_ALLOWED", denied.data()["reason"])

	// Turning branch sharing off in the member's gym blocks every branch but the home one.
	_, err := h.pool.Exec(t.Context(), `UPDATE gyms SET branch_sharing=false WHERE id=$1`, tn.GymOne)
	require.NoError(t, err)
	assert.Equal(t, "BRANCH_NOT_ALLOWED", h.check(t, reception, tn.BranchB, gym).data()["reason"])
	assert.Equal(t, true, h.check(t, reception, tn.BranchA, gym).data()["allowed"])

	// Reception can only check in its branches; a foreign branch is a bad reference.
	assert.Equal(t, "BRANCH_NOT_ACCESSIBLE", h.check(t, reception, tn.BranchC, gym).errorCode())
	other := h.newTenant(t)
	assert.Equal(t, "SCOPE_NOT_ALLOWED", h.check(t, reception, other.BranchA, gym).errorCode())
	super := h.token(t, h.newSuperadmin(t), demoPassword)
	assert.Equal(t, 403, h.check(t, super, tn.BranchA, gym).status, "the superadmin only consults")

	// By member id; an unknown id is a 404.
	list := h.call(t, http.MethodGet, "/members?q="+gym, owner, nil)
	id := list.body["data"].([]any)[0].(map[string]any)["id"].(string)
	byID := h.call(t, http.MethodPost, "/access/checks", reception, map[string]any{"branch_id": tn.BranchA, "member_id": id})
	assert.Equal(t, 200, byID.status, byID.body)
	assert.Equal(t, 404, h.call(t, http.MethodPost, "/access/checks", reception, map[string]any{"branch_id": tn.BranchA, "member_id": uuid.New()}).status)
	assert.Equal(t, 400, h.call(t, http.MethodPost, "/access/checks", reception, map[string]any{"branch_id": tn.BranchA}).status)

	// Idempotent by id: no second entry.
	checkID := uuid.New()
	body := map[string]any{"id": checkID, "branch_id": tn.BranchA, "document_number": gym}
	first := h.call(t, http.MethodPost, "/access/checks", reception, body)
	again := h.call(t, http.MethodPost, "/access/checks", reception, body)
	assert.Equal(t, first.data()["event_id"], again.data()["event_id"])
	var n int
	require.NoError(t, h.pool.QueryRow(t.Context(), `SELECT count(*) FROM access_entries WHERE id=$1`, checkID).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestAccessHistoryAndSummaryRespectReach(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	owner := h.token(t, tn.Owner, demoPassword)
	reception := h.token(t, tn.Receptionist, demoPassword)
	admin := h.token(t, tn.Admin, demoPassword)
	today := limaToday()
	member := h.memberWith(t, owner, tn.BranchA, today, 3, map[string]any{"access_scope": "network"})
	h.check(t, owner, tn.BranchA, member)
	h.check(t, owner, tn.BranchC, member)
	h.check(t, owner, tn.BranchA, uniqueDocument())

	count := func(token, query string) int {
		res := h.call(t, http.MethodGet, "/access/events"+query, token, nil)
		require.Equal(t, 200, res.status, res.body)
		return len(res.body["data"].([]any))
	}
	assert.Equal(t, 3, count(owner, ""))
	assert.Equal(t, 2, count(admin, ""), "an admin sees its gym only")
	assert.Equal(t, 2, count(reception, ""), "reception sees its branches only")
	assert.Equal(t, 1, count(owner, "?result=denied"))
	assert.Equal(t, 2, count(owner, "?result=allowed"))
	assert.Equal(t, 1, count(owner, "?branch_id="+tn.BranchC.String()))
	assert.Equal(t, 2, count(owner, "?q="+member), "two entries of that member")
	assert.Equal(t, 3, count(owner, "?from="+iso(today)+"&to="+iso(today)))
	assert.Equal(t, 0, count(owner, "?to="+iso(today.AddDate(0, 0, -1))))
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/access/events?branch_id="+tn.BranchC.String(), reception, nil).status)
	assert.Equal(t, 0, count(h.token(t, other.Owner, demoPassword), ""), "another organization sees nothing")

	summary := h.call(t, http.MethodGet, "/access/summary", owner, nil)
	require.Equal(t, 200, summary.status, summary.body)
	assert.Equal(t, 2.0, summary.data()["allowed"])
	assert.Equal(t, 1.0, summary.data()["denied"])
	assert.Equal(t, 0.0, summary.data()["day_passes"])
}

func TestDayPassSaleIsAtomicAndNumberedSeparately(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	owner := h.token(t, tn.Owner, demoPassword)
	reception := h.token(t, tn.Receptionist, demoPassword)
	super := h.token(t, h.newSuperadmin(t), demoPassword)

	// A membership sale uses the R series; the day pass starts its own D series.
	h.memberWith(t, owner, tn.BranchA, limaToday(), 1, nil)
	res := h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": tn.BranchA, "method": "cash"})
	require.Equal(t, http.StatusCreated, res.status, res.body)
	assert.Equal(t, "D-000001", res.data()["receipt"].(map[string]any)["number"])
	assert.Equal(t, "15.00", res.data()["amount"], "the default price")

	_, err := h.pool.Exec(t.Context(), `UPDATE gyms SET day_pass_price=20 WHERE id=$1`, tn.GymOne)
	require.NoError(t, err)
	id := uuid.New()
	body := map[string]any{"id": id, "branch_id": tn.BranchA, "method": "yape_plin", "visitor": map[string]any{"document_type": "dni", "document_number": uniqueDocument(), "full_name": "Visita Uno"}}
	paid := h.call(t, http.MethodPost, "/day-passes", reception, body)
	require.Equal(t, 201, paid.status, paid.body)
	assert.Equal(t, "20.00", paid.data()["amount"], "the gym's price")
	assert.Equal(t, "D-000002", paid.data()["receipt"].(map[string]any)["number"])
	assert.Equal(t, 200, h.call(t, http.MethodPost, "/day-passes", reception, body).status, "repeating the same id sells nothing twice")

	// Payment and the allowed entry were created with it.
	var payments, entries int
	require.NoError(t, h.pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM payments WHERE organization_id=$1 AND kind='day_pass'),
		(SELECT count(*) FROM access_entries WHERE organization_id=$1 AND reason_code='DAY_PASS')`, tn.OrganizationID).Scan(&payments, &entries))
	assert.Equal(t, 2, payments)
	assert.Equal(t, 2, entries)

	// The price belongs to the gym: another amount is refused, the same one accepted.
	bad := h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": tn.BranchA, "method": "cash", "amount": "1"})
	assert.Equal(t, 400, bad.status)
	same := h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": tn.BranchA, "method": "cash", "amount": "20"})
	assert.Equal(t, 201, same.status, same.body)
	assert.Equal(t, 400, h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": tn.BranchA, "method": "bitcoin"}).status)
	assert.Equal(t, 400, h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": tn.BranchA, "method": "cash", "visitor": map[string]any{"phone": "12"}}).status)

	assert.Equal(t, "BRANCH_NOT_ACCESSIBLE", h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": tn.BranchC, "method": "cash"}).errorCode())
	assert.Equal(t, "SCOPE_NOT_ALLOWED", h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": other.BranchA, "method": "cash"}).errorCode())
	assert.Equal(t, 403, h.call(t, http.MethodPost, "/day-passes", super, map[string]any{"branch_id": tn.BranchA, "method": "cash"}).status)

	// The list, the detail and the printable receipt (with the visitor) respect the tenant.
	list := h.call(t, http.MethodGet, "/day-passes", owner, nil)
	assert.Equal(t, 3.0, list.body["meta"].(map[string]any)["total"])
	assert.Equal(t, 0.0, h.call(t, http.MethodGet, "/day-passes", h.token(t, other.Owner, demoPassword), nil).body["meta"].(map[string]any)["total"])
	assert.Equal(t, 200, h.call(t, http.MethodGet, "/day-passes/"+id.String(), reception, nil).status)
	assert.Equal(t, 404, h.call(t, http.MethodGet, "/day-passes/"+id.String(), h.token(t, other.Owner, demoPassword), nil).status)
	receipt := h.call(t, http.MethodGet, "/receipts/"+paid.data()["receipt"].(map[string]any)["id"].(string), reception, nil)
	require.Equal(t, 200, receipt.status, receipt.body)
	assert.Equal(t, "day_pass", receipt.data()["kind"])
	assert.Equal(t, "Visita Uno", receipt.data()["member"].(map[string]any)["full_name"])
	assert.Nil(t, receipt.data()["membership"])

	summary := h.call(t, http.MethodGet, "/access/summary", owner, nil)
	assert.Equal(t, 3.0, summary.data()["day_passes"])
}

func TestDayPassNumbersAreGaplessUnderConcurrency(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	reception := h.token(t, tn.Receptionist, demoPassword)
	const sales = 15
	numbers := make([]string, sales)
	var wg sync.WaitGroup
	for i := range sales {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": tn.BranchA, "method": "cash"})
			if res.status == 201 {
				numbers[i] = res.data()["receipt"].(map[string]any)["number"].(string)
			}
		}()
	}
	wg.Wait()
	want := make([]string, sales)
	for i := range sales {
		want[i] = fmt.Sprintf("D-%06d", i+1)
	}
	sort.Strings(numbers)
	assert.Equal(t, want, numbers)
}
