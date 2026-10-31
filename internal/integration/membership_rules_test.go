package integration_test

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (h harness) renewal(t *testing.T, token, memberID string, body map[string]any) response {
	t.Helper()
	return h.call(t, http.MethodPost, "/members/"+memberID+"/renewals", token, body)
}

func memberIDOf(res response) string { return res.data()["member"].(map[string]any)["id"].(string) }
func periodOf(res response) map[string]any {
	return res.data()["membership"].(map[string]any)
}

func TestSamePlanPurchaseIsARenewalAndNeverOverlaps(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin := h.token(t, tn.Admin)
	today := limaToday()
	baile := h.planID(t, admin, "Baile")
	todo := h.planID(t, admin, "Todo el gym")

	b := enrollBody(tn.BranchA, uniqueDocument())
	b["plan_id"], b["months"], b["starts_on"] = baile, 2, iso(today.AddDate(0, 0, -10))
	first := h.enroll(t, admin, b)
	require.Equal(t, 201, first.status, first.body)
	member := memberIDOf(first)
	firstEnd := periodOf(first)["ends_on"].(string)
	renew := func(extra map[string]any) response {
		body := map[string]any{"branch_id": tn.BranchA, "plan_id": baile, "months": 1, "amount": "90", "method": "cash"}
		for k, v := range extra {
			body[k] = v
		}
		return h.renewal(t, admin, member, body)
	}

	// Default start: the day the last period of the plan ends (the server computes it).
	second := renew(nil)
	require.Equal(t, 201, second.status, second.body)
	assert.Equal(t, firstEnd, periodOf(second)["starts_on"])
	secondEnd := periodOf(second)["ends_on"].(string)

	// ... and again: it queues behind the programmed one.
	third := renew(nil)
	require.Equal(t, 201, third.status, third.body)
	assert.Equal(t, secondEnd, periodOf(third)["starts_on"])

	// An explicit start before the end of the last period of the same plan is refused with a stable code.
	for _, start := range []time.Time{today, today.AddDate(0, 0, -5), parseDay(t, firstEnd).AddDate(0, 0, 3), parseDay(t, secondEnd).AddDate(0, 0, -1)} {
		res := renew(map[string]any{"starts_on": iso(start)})
		assert.Equal(t, 409, res.status, iso(start))
		assert.Equal(t, "MEMBERSHIP_OVERLAP", res.errorCode(), iso(start))
	}
	clash := renew(map[string]any{"starts_on": iso(today)})
	details := clash.body["error"].(map[string]any)["details"].(map[string]any)
	assert.NotEmpty(t, details["membership_id"])
	assert.Equal(t, iso(parseDay(t, periodOf(third)["ends_on"])), details["suggested_starts_on"], "it tells where the plan can start")

	// An explicit start AFTER the last period (a gap) is fine; so is the exact end date.
	thirdEnd := periodOf(third)["ends_on"].(string)
	gap := renew(map[string]any{"starts_on": iso(parseDay(t, thirdEnd).AddDate(0, 0, 5))})
	assert.Equal(t, 201, gap.status, gap.body)

	// A DIFFERENT plan may start today alongside the current Baile.
	other := h.renewal(t, admin, member, map[string]any{"branch_id": tn.BranchA, "plan_id": todo, "months": 1, "amount": "120", "method": "cash", "starts_on": iso(today)})
	assert.Equal(t, 201, other.status, other.body)
	assert.Equal(t, iso(today), periodOf(other)["starts_on"])

	// Idempotent retry of a created renewal is not an overlap.
	id := "00000000-0000-7000-8000-" + "0000000000a1"
	body := map[string]any{"id": id, "branch_id": tn.BranchA, "plan_id": todo, "months": 1, "amount": "120", "method": "cash"}
	assert.Equal(t, 201, h.renewal(t, admin, member, body).status)
	assert.Equal(t, 200, h.renewal(t, admin, member, body).status)

	// An expired plan renews from today.
	b2 := enrollBody(tn.BranchA, uniqueDocument())
	b2["plan_id"], b2["months"], b2["starts_on"] = baile, 1, iso(today.AddDate(0, -3, 0))
	old := h.enroll(t, admin, b2)
	require.Equal(t, 201, old.status)
	back := h.renewal(t, admin, memberIDOf(old), map[string]any{"branch_id": tn.BranchA, "plan_id": baile, "months": 1, "amount": "90", "method": "cash"})
	assert.Equal(t, iso(today), periodOf(back)["starts_on"])
}

func TestConcurrentRenewalsOfTheSamePlanNeverOverlap(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin := h.token(t, tn.Admin)
	first := h.enroll(t, admin, enrollBody(tn.BranchA, uniqueDocument()))
	require.Equal(t, 201, first.status)
	member := memberIDOf(first)
	start := iso(limaToday().AddDate(1, 0, 0))

	var wg sync.WaitGroup
	results := make(chan int, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- h.renewal(t, admin, member, map[string]any{"branch_id": tn.BranchA, "months": 1, "amount": "90", "method": "cash", "starts_on": start}).status
		}()
	}
	wg.Wait()
	close(results)
	created, conflicts := 0, 0
	for status := range results {
		switch status {
		case 201:
			created++
		case 409:
			conflicts++
		}
	}
	assert.Equal(t, 1, created, "only one of the simultaneous identical purchases fits")
	assert.Equal(t, 5, conflicts)
}

func TestAmendMembershipRulesRolesAndTraceability(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	admin, reception, otherAdmin := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, other.Admin)
	today := limaToday()

	sold := h.enroll(t, admin, func() map[string]any {
		b := enrollBody(tn.BranchA, uniqueDocument())
		b["months"], b["amount"], b["method"], b["starts_on"] = 3, "270", "cash", iso(today.AddDate(0, 0, -5))
		return b
	}())
	require.Equal(t, 201, sold.status, sold.body)
	member, period := memberIDOf(sold), periodOf(sold)["id"].(string)
	receipt := sold.data()["receipt"].(map[string]any)
	path := "/members/" + member + "/memberships/" + period

	// Validation.
	for name, bad := range map[string]map[string]any{
		"empty":      {},
		"months 0":   {"months": 0},
		"months 25":  {"months": 25},
		"amount":     {"amount": "-3"},
		"too big":    {"amount": "1000000.01"},
		"method":     {"method": "bitcoin"},
		"start":      {"starts_on": "10/10/2026"},
		"ends today": {"starts_on": iso(today.AddDate(-1, 0, 0))},
	} {
		res := h.call(t, http.MethodPatch, path, admin, bad)
		assert.Equal(t, 400, res.status, name)
		assert.Equal(t, "VALIDATION_FAILED", res.errorCode(), name)
	}

	// Roles and isolation.
	assert.Equal(t, 401, h.call(t, http.MethodPatch, path, "", map[string]any{"months": 2}).status)
	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, h.superToken(t), map[string]any{"months": 2}).status)
	assert.Equal(t, 404, h.call(t, http.MethodPatch, path, otherAdmin, map[string]any{"months": 2}).status, "another gym never sees it")
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/members/"+member+"/memberships/"+member, admin, map[string]any{"months": 2}).status)
	assert.Equal(t, "MEMBERSHIP_NOT_FOUND", h.call(t, http.MethodPatch, "/members/"+member+"/memberships/"+member, admin, map[string]any{"months": 2}).errorCode())

	// A correction: the server recalculates the end; the receipt number does not change; the payment follows.
	start := today.AddDate(0, 0, -2)
	res := h.call(t, http.MethodPatch, path, reception, map[string]any{"months": 6, "starts_on": iso(start), "amount": "500.5", "method": "yape_plin"})
	require.Equal(t, 200, res.status, res.body)
	got := periodOf(res)
	assert.Equal(t, float64(6), got["months"])
	assert.Equal(t, iso(start), got["starts_on"])
	assert.Equal(t, iso(start.AddDate(0, 6, 0)), got["ends_on"], "the end is recalculated by the server")
	assert.Equal(t, "500.50", got["amount"])
	assert.NotNil(t, got["amended_at"])
	assert.Equal(t, receipt["number"], res.data()["receipt"].(map[string]any)["number"], "same receipt, corrected in place")
	printed := h.call(t, http.MethodGet, "/receipts/"+receipt["id"].(string), admin, nil)
	assert.Equal(t, "500.50", printed.data()["payment"].(map[string]any)["amount"])
	assert.Equal(t, "yape_plin", printed.data()["payment"].(map[string]any)["method"])
	assert.Equal(t, iso(start.AddDate(0, 6, 0)), printed.data()["membership"].(map[string]any)["ends_on"])
	assert.Equal(t, "6 meses", printed.data()["membership"].(map[string]any)["plan_label"])
	list := h.call(t, http.MethodGet, "/members/"+member+"/receipts", admin, nil)
	require.Len(t, list.body["data"], 1, "no second receipt is created")

	// A no-op leaves no trace.
	same := h.call(t, http.MethodPatch, path, admin, map[string]any{"months": 6})
	assert.Equal(t, 200, same.status)

	// History: admin only, oldest first, with the author and the values before and after.
	assert.Equal(t, 403, h.call(t, http.MethodGet, path+"/amendments", reception, nil).status)
	history := h.call(t, http.MethodGet, path+"/amendments", admin, nil)
	require.Equal(t, 200, history.status, history.body)
	rows := history.body["data"].([]any)
	require.Len(t, rows, 1)
	row := rows[0].(map[string]any)
	assert.Equal(t, "Recepción de Prueba", row["changed_by"].(map[string]any)["full_name"])
	assert.Equal(t, "270.00", row["before"].(map[string]any)["amount"])
	assert.Equal(t, "cash", row["before"].(map[string]any)["method"])
	assert.Equal(t, "500.50", row["after"].(map[string]any)["amount"])
	assert.Equal(t, 404, h.call(t, http.MethodGet, path+"/amendments", otherAdmin, nil).status)

	// Branch access: the payment was collected in a branch the receptionist does not reach.
	hidden := h.extraBranch(t, tn)
	elsewhere := h.enroll(t, admin, func() map[string]any {
		b := enrollBody(hidden, uniqueDocument())
		return b
	}())
	require.Equal(t, 201, elsewhere.status, elsewhere.body)
	denied := h.call(t, http.MethodPatch, "/members/"+memberIDOf(elsewhere)+"/memberships/"+periodOf(elsewhere)["id"].(string), reception, map[string]any{"amount": "10"})
	assert.Equal(t, 403, denied.status)
	assert.Equal(t, "BRANCH_NOT_ACCESSIBLE", denied.errorCode())
	assert.Equal(t, 200, h.call(t, http.MethodPatch, "/members/"+memberIDOf(elsewhere)+"/memberships/"+periodOf(elsewhere)["id"].(string), admin, map[string]any{"amount": "10"}).status)
}

func TestAmendMembershipEndedAndOverlap(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin := h.token(t, tn.Admin)
	today := limaToday()
	baile := h.planID(t, admin, "Baile")
	todo := h.planID(t, admin, "Todo el gym")

	b := enrollBody(tn.BranchA, uniqueDocument())
	b["plan_id"], b["months"], b["starts_on"] = baile, 1, iso(today.AddDate(0, -3, 0))
	old := h.enroll(t, admin, b)
	require.Equal(t, 201, old.status)
	member := memberIDOf(old)
	ended := h.call(t, http.MethodPatch, "/members/"+member+"/memberships/"+periodOf(old)["id"].(string), admin, map[string]any{"amount": "50"})
	assert.Equal(t, 409, ended.status)
	assert.Equal(t, "MEMBERSHIP_ENDED", ended.errorCode(), "an ended period cannot be changed")

	// Two queued periods of Baile (current + next) and a Todo period.
	current := h.renewal(t, admin, member, map[string]any{"branch_id": tn.BranchA, "plan_id": baile, "months": 1, "amount": "90", "method": "cash"})
	next := h.renewal(t, admin, member, map[string]any{"branch_id": tn.BranchA, "plan_id": baile, "months": 1, "amount": "90", "method": "cash"})
	parallel := h.renewal(t, admin, member, map[string]any{"branch_id": tn.BranchA, "plan_id": todo, "months": 1, "amount": "120", "method": "cash"})
	require.Equal(t, 201, parallel.status, parallel.body)
	currentPath := "/members/" + member + "/memberships/" + periodOf(current)["id"].(string)
	nextPath := "/members/" + member + "/memberships/" + periodOf(next)["id"].(string)

	// Lengthening the current period would run into the next one of the same plan.
	res := h.call(t, http.MethodPatch, currentPath, admin, map[string]any{"months": 2})
	assert.Equal(t, 409, res.status, res.body)
	assert.Equal(t, "MEMBERSHIP_OVERLAP", res.errorCode())
	// Moving the next one back is refused too; moving it forward is fine.
	assert.Equal(t, 409, h.call(t, http.MethodPatch, nextPath, admin, map[string]any{"starts_on": iso(today)}).status)
	later := h.call(t, http.MethodPatch, nextPath, admin, map[string]any{"starts_on": iso(parseDay(t, periodOf(next)["starts_on"]).AddDate(0, 0, 4))})
	assert.Equal(t, 200, later.status, later.body)
	// Now the current one may grow by up to the gap... and a different plan never matters.
	assert.Equal(t, 200, h.call(t, http.MethodPatch, "/members/"+member+"/memberships/"+periodOf(parallel)["id"].(string), admin, map[string]any{"months": 3}).status)
	// Status stays derived: still covered.
	detail := h.call(t, http.MethodGet, "/members/"+member, admin, nil)
	assert.Contains(t, []any{"active", "expiring"}, detail.data()["status"])
}
