package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (h harness) createPlan(t *testing.T, token string, body map[string]any) response {
	t.Helper()
	return h.call(t, http.MethodPost, "/membership-plans", token, body)
}

// planID creates a plan as the admin and returns its id.
func (h harness) planID(t *testing.T, admin, name string, prices ...map[string]any) string {
	t.Helper()
	res := h.createPlan(t, admin, map[string]any{"name": name, "prices": prices})
	require.Equal(t, 201, res.status, res.body)
	return res.data()["id"].(string)
}

func price(months int, amount string) map[string]any {
	return map[string]any{"months": months, "amount": amount}
}

func TestMembershipPlansRolesValidationAndIsolation(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	admin, reception, adminTwo := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, tn.AdminTwo)
	otherAdmin := h.token(t, other.Admin)

	// Role matrix: only the admin of the gym writes; reception reads; the superadmin's token is refused everywhere.
	body := map[string]any{"name": "Baile", "description": "  Clases   de baile ", "prices": []any{price(3, "240.00"), price(1, "90.00")}}
	assert.Equal(t, 403, h.createPlan(t, reception, body).status)
	assert.Equal(t, 403, h.createPlan(t, h.superToken(t), body).status)
	assert.Equal(t, 401, h.createPlan(t, "", body).status)
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/membership-plans", h.superToken(t), nil).status)

	created := h.createPlan(t, admin, body)
	require.Equal(t, 201, created.status, created.body)
	plan := created.data()
	id := plan["id"].(string)
	assert.Equal(t, "Baile", plan["name"])
	assert.Equal(t, "Clases de baile", plan["description"], "spaces are collapsed")
	assert.Equal(t, true, plan["is_active"])
	assert.Equal(t, []any{
		map[string]any{"months": float64(1), "amount": "90.00"}, map[string]any{"months": float64(3), "amount": "240.00"},
	}, plan["prices"], "prices come ordered by months")

	// Validation, field by field.
	for name, bad := range map[string]map[string]any{
		"name empty":      {"name": "  "},
		"name too long":   {"name": string(make([]byte, 61))},
		"description":     {"name": "X", "description": string(make([]rune, 201))},
		"months 0":        {"name": "X", "prices": []any{price(0, "10.00")}},
		"months 25":       {"name": "X", "prices": []any{price(25, "10.00")}},
		"amount format":   {"name": "X", "prices": []any{price(1, "10")}},
		"amount negative": {"name": "X", "prices": []any{price(1, "-1.00")}},
		"amount too big":  {"name": "X", "prices": []any{price(1, "1000000.01")}},
		"repeated months": {"name": "X", "prices": []any{price(1, "10.00"), price(1, "12.00")}},
		"position":        {"name": "X", "position": -3},
	} {
		res := h.createPlan(t, admin, bad)
		assert.Equal(t, 400, res.status, name)
		assert.Equal(t, "VALIDATION_FAILED", res.errorCode(), name)
	}
	taken := h.createPlan(t, admin, map[string]any{"name": " baile "})
	assert.Equal(t, 409, taken.status)
	assert.Equal(t, "PLAN_NAME_TAKEN", taken.errorCode(), "names are unique per gym, ignoring case and spacing")
	assert.Equal(t, 201, h.createPlan(t, otherAdmin, map[string]any{"name": "Baile"}).status, "another gym may use the same name")
	assert.Equal(t, 201, h.createPlan(t, adminTwo, map[string]any{"name": "Baile"}).status, "even a sibling gym of the same group")

	// Idempotent create by id.
	again := uuid.NewString()
	assert.Equal(t, 201, h.createPlan(t, admin, map[string]any{"id": again, "name": "Todo el gym"}).status)
	assert.Equal(t, 409, h.createPlan(t, admin, map[string]any{"id": again, "name": "Otro nombre"}).status)

	// Patch: reception forbidden, only the fields sent change, prices replace the list.
	path := "/membership-plans/" + id
	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, reception, map[string]any{"name": "X"}).status)
	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, h.superToken(t), map[string]any{"name": "X"}).status)
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/membership-plans/"+uuid.NewString(), admin, map[string]any{"name": "X"}).status)
	assert.Equal(t, 404, h.call(t, http.MethodPatch, path, otherAdmin, map[string]any{"name": "Robado"}).status, "a plan of another gym does not exist")
	assert.Equal(t, 404, h.call(t, http.MethodPatch, path, adminTwo, map[string]any{"name": "Robado"}).status, "nor one of a sibling gym")
	for _, bad := range []map[string]any{{"name": nil}, {"name": ""}, {"is_active": nil}, {"prices": nil}, {"prices": []any{price(1, "1")}}, {"position": -1}} {
		assert.Equal(t, 400, h.call(t, http.MethodPatch, path, admin, bad).status, "%v", bad)
	}
	edited := h.call(t, http.MethodPatch, path, admin, map[string]any{"prices": []any{price(12, "800.00")}, "description": nil})
	require.Equal(t, 200, edited.status, edited.body)
	assert.Equal(t, "Baile", edited.data()["name"], "a field not sent stays")
	assert.Nil(t, edited.data()["description"])
	assert.Equal(t, []any{map[string]any{"months": float64(12), "amount": "800.00"}}, edited.data()["prices"])
	clashing := h.call(t, http.MethodPatch, "/membership-plans/"+again, admin, map[string]any{"name": "BAILE"})
	assert.Equal(t, 409, clashing.status)

	// Deactivating hides the plan from reception but not from the admin.
	off := h.call(t, http.MethodPatch, path, admin, map[string]any{"is_active": false})
	require.Equal(t, 200, off.status, off.body)
	names := func(token string) []string {
		res := h.call(t, http.MethodGet, "/membership-plans", token, nil)
		require.Equal(t, 200, res.status, res.body)
		var out []string
		for _, raw := range res.body["data"].([]any) {
			out = append(out, raw.(map[string]any)["name"].(string))
		}
		return out
	}
	assert.Equal(t, []string{"Baile", "Todo el gym"}, names(admin))
	assert.Equal(t, []string{"Todo el gym"}, names(reception), "reception only sees what can be sold")
	assert.Equal(t, []string{"Baile"}, names(otherAdmin))
	// Ordering by position.
	require.Equal(t, 200, h.call(t, http.MethodPatch, "/membership-plans/"+again, admin, map[string]any{"position": 0}).status)
	require.Equal(t, 200, h.call(t, http.MethodPatch, path, admin, map[string]any{"position": 5}).status)
	assert.Equal(t, []string{"Todo el gym", "Baile"}, names(admin))
}

func TestSellingChoosesThePlan(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	admin, reception := h.token(t, tn.Admin), h.token(t, tn.Receptionist)
	otherAdmin := h.token(t, other.Admin)

	// A gym that never configured plans gets the default "Membresía" on its first sale.
	first := h.enroll(t, reception, enrollBody(tn.BranchA, uniqueDocument()))
	require.Equal(t, 201, first.status, first.body)
	assert.Equal(t, "Membresía", first.data()["membership"].(map[string]any)["plan_name"])
	list := h.call(t, http.MethodGet, "/membership-plans", admin, nil)
	require.Len(t, list.body["data"], 1)
	defaultID := list.body["data"].([]any)[0].(map[string]any)["id"].(string)
	assert.Equal(t, defaultID, first.data()["membership"].(map[string]any)["plan_id"])
	secondDoc := uniqueDocument()
	second := h.enroll(t, reception, enrollBody(tn.BranchA, secondDoc))
	assert.Equal(t, 201, second.status)
	assert.Equal(t, defaultID, second.data()["membership"].(map[string]any)["plan_id"], "the one active plan is used, no second default")
	assert.Len(t, h.call(t, http.MethodGet, "/membership-plans", admin, nil).body["data"], 1)

	// With two active plans the sale must say which.
	baile := h.planID(t, admin, "Baile", price(1, "90.00"))
	foreign := h.planID(t, otherAdmin, "De otro gym")
	missing := h.enroll(t, reception, enrollBody(tn.BranchA, uniqueDocument()))
	assert.Equal(t, 400, missing.status)
	assert.Equal(t, "VALIDATION_FAILED", missing.errorCode())
	assert.Equal(t, "PLAN_REQUIRED", planFieldCode(missing, "plan_id"))
	for name, planID := range map[string]any{"another gym's plan": foreign, "unknown plan": uuid.NewString()} {
		body := enrollBody(tn.BranchA, uniqueDocument())
		body["plan_id"] = planID
		res := h.enroll(t, reception, body)
		assert.Equal(t, 400, res.status, name)
		assert.Equal(t, "PLAN_INVALID", planFieldCode(res, "plan_id"), name)
	}
	var members int
	require.NoError(t, h.pool.QueryRow(t.Context(), `SELECT count(*) FROM members WHERE gym_id=$1`, tn.GymOne).Scan(&members))
	assert.Equal(t, 2, members, "a refused sale stores nothing")

	// A sale with a chosen plan: any amount is accepted (the price is only a suggestion).
	body := enrollBody(tn.BranchA, uniqueDocument())
	body["plan_id"], body["months"], body["amount"] = baile, 1, "75.50"
	sold := h.enroll(t, reception, body)
	require.Equal(t, 201, sold.status, sold.body)
	membership := sold.data()["membership"].(map[string]any)
	assert.Equal(t, baile, membership["plan_id"])
	assert.Equal(t, "Baile", membership["plan_name"])
	assert.Equal(t, "75.50", membership["amount"])
	assert.Equal(t, []any{"Baile"}, sold.data()["member"].(map[string]any)["plan_names"])
	receiptID := sold.data()["receipt"].(map[string]any)["id"].(string)

	// The period keeps the plan's name as sold, even if the plan is renamed or deactivated afterwards.
	require.Equal(t, 200, h.call(t, http.MethodPatch, "/membership-plans/"+baile, admin, map[string]any{"name": "Zumba", "is_active": false}).status)
	printable := h.call(t, http.MethodGet, "/receipts/"+receiptID, reception, nil)
	require.Equal(t, 200, printable.status, printable.body)
	assert.Equal(t, "Baile", printable.data()["membership"].(map[string]any)["plan_name"])
	memberID := sold.data()["member"].(map[string]any)["id"].(string)
	receipts := h.call(t, http.MethodGet, "/members/"+memberID+"/receipts", reception, nil)
	assert.Equal(t, "Baile", receipts.body["data"].([]any)[0].(map[string]any)["plan_name"])
	detail := h.call(t, http.MethodGet, "/members/"+memberID, reception, nil)
	assert.Equal(t, "Baile", detail.data()["memberships"].([]any)[0].(map[string]any)["plan_name"])
	payments := h.call(t, http.MethodGet, "/payments?from="+iso(limaToday())+"&to="+iso(limaToday()), admin, nil)
	require.Equal(t, 200, payments.status, payments.body)
	var seen []any
	for _, raw := range payments.body["data"].([]any) {
		seen = append(seen, raw.(map[string]any)["plan_name"])
	}
	assert.Contains(t, seen, "Baile")

	// A deactivated plan cannot be sold; with only "Membresía" left active it is used again.
	renew := map[string]any{"branch_id": tn.BranchA, "months": 1, "amount": "90", "method": "cash", "plan_id": baile}
	res := h.call(t, http.MethodPost, "/members/"+memberID+"/renewals", reception, renew)
	assert.Equal(t, 400, res.status)
	assert.Equal(t, "PLAN_INVALID", planFieldCode(res, "plan_id"))
	delete(renew, "plan_id")
	res = h.call(t, http.MethodPost, "/members/"+memberID+"/renewals", reception, renew)
	require.Equal(t, 201, res.status, res.body)
	assert.Equal(t, "Membresía", res.data()["membership"].(map[string]any)["plan_name"])

	// Deactivating every plan leaves nothing to sell, and the sale says so instead of inventing a plan.
	require.Equal(t, 200, h.call(t, http.MethodPatch, "/membership-plans/"+defaultID, admin, map[string]any{"is_active": false}).status)
	none := h.enroll(t, reception, enrollBody(tn.BranchA, uniqueDocument()))
	assert.Equal(t, 400, none.status)
	assert.Equal(t, "PLAN_REQUIRED", planFieldCode(none, "plan_id"))

	// Another gym's data never mixed in.
	assert.Len(t, h.call(t, http.MethodGet, "/membership-plans", otherAdmin, nil).body["data"], 1)
}

func planFieldCode(res response, field string) string {
	e, _ := res.body["error"].(map[string]any)
	details, _ := e["details"].([]any)
	for _, raw := range details {
		d, _ := raw.(map[string]any)
		if d["field"] == field {
			code, _ := d["code"].(string)
			return code
		}
	}
	return ""
}

func parseDay(t *testing.T, value any) time.Time {
	t.Helper()
	day, err := time.Parse("2006-01-02", value.(string))
	require.NoError(t, err)
	return day
}

func TestSeveralPlansAtOnce(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin := h.token(t, tn.Admin)
	today := limaToday()
	baile := h.planID(t, admin, "Baile")
	todo := h.planID(t, admin, "Todo el gym")

	// "Baile" opens only branch A and ends soon; "Todo el gym" opens the whole gym and ends later.
	document := uniqueDocument()
	b := enrollBody(tn.BranchA, document)
	b["plan_id"], b["months"], b["starts_on"], b["access_scope"] = baile, 1, iso(today.AddDate(0, 0, -25)), "branch"
	first := h.enroll(t, admin, b)
	require.Equal(t, 201, first.status, first.body)
	memberID := first.data()["member"].(map[string]any)["id"].(string)
	baileEnd := parseDay(t, first.data()["membership"].(map[string]any)["ends_on"])
	baileDays := int(baileEnd.Sub(today).Hours() / 24)
	require.GreaterOrEqual(t, baileDays, 0)
	require.LessOrEqual(t, baileDays, 7, "the test relies on Baile ending within a week")
	assert.Equal(t, "expiring", first.data()["member"].(map[string]any)["status"], "with only Baile the member is about to expire")

	// Renew with ANOTHER plan: it starts today (not after Baile) because a renewal continues the same plan.
	renewTodo := h.call(t, http.MethodPost, "/members/"+memberID+"/renewals", admin, map[string]any{
		"branch_id": tn.BranchA, "plan_id": todo, "months": 3, "amount": "300", "method": "cash", "access_scope": "gym"})
	require.Equal(t, 201, renewTodo.status, renewTodo.body)
	todoPeriod := renewTodo.data()["membership"].(map[string]any)
	assert.Equal(t, iso(today), todoPeriod["starts_on"])
	todoEnd := parseDay(t, todoPeriod["ends_on"])
	member := renewTodo.data()["member"].(map[string]any)
	assert.Equal(t, "active", member["status"], "the plan that ends later decides: the member is no longer about to expire")
	assert.Equal(t, float64(int(todoEnd.Sub(today).Hours()/24)), member["days_remaining"])
	assert.ElementsMatch(t, []any{"Baile", "Todo el gym"}, member["plan_names"])

	detail := h.call(t, http.MethodGet, "/members/"+memberID, admin, nil)
	require.Equal(t, 200, detail.status)
	periods := detail.data()["memberships"].([]any)
	require.Len(t, periods, 2, "the file lists the periods of every plan")
	plansSeen := map[any]bool{}
	for _, raw := range periods {
		plansSeen[raw.(map[string]any)["plan_name"]] = true
	}
	assert.Equal(t, map[any]bool{"Baile": true, "Todo el gym": true}, plansSeen)

	// Access: branch B is opened only by "Todo el gym"; branch A by both. The end shown is the later one.
	atB := h.check(t, admin, tn.BranchB, document)
	assert.Equal(t, true, atB.data()["allowed"], atB.body)
	atA := h.check(t, admin, tn.BranchA, document)
	assert.Equal(t, true, atA.data()["allowed"])
	assert.Equal(t, iso(todoEnd), atA.data()["ends_on"])
	assert.Nil(t, atA.data()["expiring_in_days"])

	// The list filters by the same status.
	expiring := h.call(t, http.MethodGet, "/members?status=expiring", admin, nil)
	assert.Empty(t, expiring.body["data"])
	active := h.call(t, http.MethodGet, "/members?status=active", admin, nil)
	assert.Len(t, active.body["data"], 1)
	assert.Equal(t, float64(1), active.body["meta"].(map[string]any)["counts"].(map[string]any)["active"])

	// Renewing Baile continues Baile: it starts where the last Baile period ends, in the future.
	renewBaile := h.call(t, http.MethodPost, "/members/"+memberID+"/renewals", admin, map[string]any{
		"branch_id": tn.BranchA, "plan_id": baile, "months": 1, "amount": "90", "method": "cash"})
	require.Equal(t, 201, renewBaile.status, renewBaile.body)
	assert.Equal(t, iso(baileEnd), renewBaile.data()["membership"].(map[string]any)["starts_on"])
	assert.Equal(t, "active", renewBaile.data()["member"].(map[string]any)["status"])

	// A member holding only a branch-limited plan today is refused elsewhere, even though another plan exists in the future.
	narrow := uniqueDocument()
	n := enrollBody(tn.BranchA, narrow)
	n["plan_id"], n["months"], n["access_scope"] = baile, 1, "branch"
	require.Equal(t, 201, h.enroll(t, admin, n).status)
	denied := h.check(t, admin, tn.BranchB, narrow)
	assert.Equal(t, false, denied.data()["allowed"])
	assert.Equal(t, "BRANCH_NOT_ALLOWED", denied.data()["reason"])
}

func TestStatusWithPlansCoverageRules(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin := h.token(t, tn.Admin)
	today := limaToday()
	a := h.planID(t, admin, "Plan A")
	b := h.planID(t, admin, "Plan B")
	sell := func(document, plan string, start time.Time, months int) response {
		body := enrollBody(tn.BranchA, document)
		body["plan_id"], body["months"], body["starts_on"] = plan, months, iso(start)
		return h.enroll(t, admin, body)
	}
	statusOf := func(document string) (string, float64) {
		res := h.call(t, http.MethodGet, "/members?q="+document, admin, nil)
		require.Equal(t, 200, res.status, res.body)
		row := res.body["data"].([]any)[0].(map[string]any)
		return row["status"].(string), row["days_remaining"].(float64)
	}
	addPeriod := func(document string, plan string, start time.Time, months int) {
		res := h.call(t, http.MethodGet, "/members?q="+document, admin, nil)
		id := res.body["data"].([]any)[0].(map[string]any)["id"].(string)
		renew := h.call(t, http.MethodPost, "/members/"+id+"/renewals", admin, map[string]any{
			"branch_id": tn.BranchA, "plan_id": plan, "months": months, "amount": "10", "method": "cash", "starts_on": iso(start)})
		require.Equal(t, 201, renew.status, renew.body)
	}

	// Two plans: one expired, one current.
	d1 := uniqueDocument()
	require.Equal(t, 201, sell(d1, a, today.AddDate(0, -6, 0), 1).status)
	addPeriod(d1, b, today.AddDate(0, 0, -5), 3)
	status, days := statusOf(d1)
	assert.Equal(t, "active", status)
	assert.Greater(t, days, float64(60))

	// Both plans current, the one that ends later decides.
	d2 := uniqueDocument()
	require.Equal(t, 201, sell(d2, a, today.AddDate(0, 0, -3), 1).status) // ends in ~4 weeks
	addPeriod(d2, b, today.AddDate(0, 0, -20), 1)                         // ends in ~1.5 weeks
	status, days = statusOf(d2)
	assert.Equal(t, "active", status)
	assert.Greater(t, days, float64(7))

	// A gap between plans: the member is covered now but about to lose it; the later period does not rescue them.
	d3 := uniqueDocument()
	require.Equal(t, 201, sell(d3, a, today.AddDate(0, 0, -28), 1).status) // ends in 0..3 days
	addPeriod(d3, b, today.AddDate(0, 0, 20), 1)
	status, _ = statusOf(d3)
	assert.Equal(t, "expiring", status)

	// A period of another plan that starts right where the first ends continues the coverage.
	d4 := uniqueDocument()
	first := sell(d4, a, today.AddDate(0, 0, -27), 1)
	require.Equal(t, 201, first.status)
	end := parseDay(t, first.data()["membership"].(map[string]any)["ends_on"])
	addPeriod(d4, b, end.AddDate(0, 0, 1), 2)
	status, _ = statusOf(d4)
	assert.Equal(t, "active", status)

	// Only a future period: scheduled; only past ones: expired with negative days.
	d5, d6 := uniqueDocument(), uniqueDocument()
	require.Equal(t, 201, sell(d5, a, today.AddDate(0, 0, 10), 1).status)
	require.Equal(t, 201, sell(d6, b, today.AddDate(0, -4, 0), 1).status)
	status, _ = statusOf(d5)
	assert.Equal(t, "scheduled", status)
	status, days = statusOf(d6)
	assert.Equal(t, "expired", status)
	assert.Less(t, days, float64(0))

	// The dashboard counts the same way.
	dash := h.call(t, http.MethodGet, "/dashboard", admin, nil)
	require.Equal(t, 200, dash.status, dash.body)
	members := dash.data()["members"].(map[string]any)
	assert.Equal(t, float64(3), members["active"])
	assert.Equal(t, float64(1), members["expiring"])
	assert.Equal(t, float64(1), members["scheduled"])
	assert.Equal(t, float64(1), members["expired"])
}
