package integration_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The group only bundles gyms: members, sales and access belong to one gym, and the gyms of one
// group are strangers to each other (plan 07). GymOne and GymTwo of a tenant are exactly that.

func TestSameDocumentIsAnotherMemberInASiblingGym(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, adminTwo := h.token(t, tn.Admin), h.token(t, tn.AdminTwo)
	document := uniqueDocument()

	one := h.enroll(t, admin, enrollBody(tn.BranchA, document))
	require.Equal(t, 201, one.status, one.body)
	two := h.enroll(t, adminTwo, enrollBody(tn.BranchC, document))
	require.Equal(t, 201, two.status, "the document is unique per gym, not per group: %v", two.body)
	assert.NotEqual(t, one.data()["member"].(map[string]any)["id"], two.data()["member"].(map[string]any)["id"], "two unrelated records")

	dup := h.enroll(t, admin, enrollBody(tn.BranchB, document))
	assert.Equal(t, 409, dup.status)
	assert.Equal(t, "MEMBER_DOCUMENT_TAKEN", dup.errorCode(), "still unique inside the gym")
	dup = h.enroll(t, adminTwo, enrollBody(tn.BranchC, document))
	assert.Equal(t, "MEMBER_DOCUMENT_TAKEN", dup.errorCode())

	// The database says the same, independent of the application.
	_, err := h.pool.Exec(context.Background(), `INSERT INTO members (id, gym_id, home_branch_id, document_type, document_number, full_name, phone)
		VALUES ($1, $2, $3, 'dni', $4, 'Repetido', '911111111')`, uuid.New(), tn.GymOne, tn.BranchA, document)
	assert.Error(t, err)

	for _, token := range []string{admin, adminTwo} {
		list := h.call(t, http.MethodGet, "/members?q="+document, token, nil)
		require.Equal(t, 200, list.status)
		assert.Len(t, list.body["data"], 1, "each gym lists only its own record")
	}
}

func TestGymsOfTheSameGroupAreIsolated(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, adminTwo := h.token(t, tn.Admin), h.token(t, tn.AdminTwo)
	reception := h.token(t, tn.Receptionist)
	today := limaToday()

	document := uniqueDocument()
	sold := h.enroll(t, admin, enrollBody(tn.BranchA, document))
	require.Equal(t, 201, sold.status, sold.body)
	memberID := sold.data()["member"].(map[string]any)["id"].(string)
	receiptID := sold.data()["receipt"].(map[string]any)["id"].(string)
	pass := h.call(t, http.MethodPost, "/day-passes", reception, map[string]any{"branch_id": tn.BranchA, "method": "cash"})
	require.Equal(t, 201, pass.status, pass.body)
	passID := pass.data()["id"].(string)

	t.Run("a resource of the sibling gym addressed by id is a 404", func(t *testing.T) {
		for _, call := range []struct {
			method, path string
			body         any
		}{
			{http.MethodGet, "/members/" + memberID, nil},
			{http.MethodPatch, "/members/" + memberID, map[string]any{"phone": "955555555"}},
			{http.MethodPost, "/members/" + memberID + "/renewals", map[string]any{"branch_id": tn.BranchC, "months": 1, "amount": "10", "method": "cash"}},
			{http.MethodGet, "/members/" + memberID + "/receipts", nil},
			{http.MethodGet, "/receipts/" + receiptID, nil},
			{http.MethodGet, "/receipts/" + receiptID + "/vouchers", nil},
			{http.MethodGet, "/day-passes/" + passID, nil},
			{http.MethodPatch, "/users/" + tn.ReceptionistID.String(), map[string]any{"full_name": "Intruso"}},
		} {
			res := h.call(t, call.method, call.path, adminTwo, call.body)
			assert.Equal(t, 404, res.status, call.method+" "+call.path)
		}
		var phone string
		require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT phone FROM members WHERE id = $1`, memberID).Scan(&phone))
		assert.NotEqual(t, "955555555", phone, "nothing changed")
		var periods int
		require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM memberships WHERE member_id = $1`, memberID).Scan(&periods))
		assert.Equal(t, 1, periods)
	})

	t.Run("references in a body to the sibling gym are a 400", func(t *testing.T) {
		res := h.enroll(t, adminTwo, enrollBody(tn.BranchA, uniqueDocument()))
		assert.Equal(t, 400, res.status)
		assert.Equal(t, "SCOPE_NOT_ALLOWED", res.errorCode())
		res = h.call(t, http.MethodPost, "/day-passes", adminTwo, map[string]any{"branch_id": tn.BranchA, "method": "cash"})
		assert.Equal(t, "SCOPE_NOT_ALLOWED", res.errorCode())
		res = h.call(t, http.MethodPost, "/access/checks", adminTwo, map[string]any{"branch_id": tn.BranchA, "document_number": document})
		assert.Equal(t, "SCOPE_NOT_ALLOWED", res.errorCode())
		res = h.call(t, http.MethodPost, "/users", admin, map[string]any{"full_name": "Rosa Recepción", "document_number": uniqueDocument(),
			"role": "receptionist", "branch_ids": []string{tn.BranchC.String()}})
		assert.Equal(t, 400, res.status)
		assert.Equal(t, "SCOPE_NOT_ALLOWED", res.errorCode(), "reception cannot be given a branch of another gym")
		res = h.call(t, http.MethodPatch, "/gyms/"+tn.GymOne.String(), adminTwo, map[string]any{"branch_sharing": false})
		assert.Equal(t, 403, res.status, "the rules of the sibling gym are not theirs")
		for _, query := range []string{"?branch_id=" + tn.BranchA.String(), "?gym_id=" + tn.GymOne.String()} {
			assert.Contains(t, []int{400, 403}, h.call(t, http.MethodGet, "/payments"+query, adminTwo, nil).status, query)
		}
	})

	t.Run("the sibling gym sees none of it", func(t *testing.T) {
		member := h.call(t, http.MethodGet, "/members?q="+document, adminTwo, nil)
		assert.Len(t, member.body["data"], 0)
		payments := h.call(t, http.MethodGet, "/payments", adminTwo, nil)
		assert.Equal(t, 0.0, payments.body["meta"].(map[string]any)["total"])
		dayPasses := h.call(t, http.MethodGet, "/day-passes", adminTwo, nil)
		assert.Equal(t, 0.0, dayPasses.body["meta"].(map[string]any)["total"])
		dash := h.call(t, http.MethodGet, "/dashboard", adminTwo, nil)
		require.Equal(t, 200, dash.status, dash.body)
		assert.Equal(t, map[string]any{"active": 0.0, "expiring": 0.0, "expired": 0.0, "scheduled": 0.0}, dash.data()["members"])
		team := h.call(t, http.MethodGet, "/users", adminTwo, nil)
		require.Equal(t, 200, team.status, team.body)
		for _, raw := range team.body["data"].([]any) {
			assert.NotEqual(t, tn.ReceptionistID.String(), raw.(map[string]any)["id"], "the team is the gym's")
		}
	})

	t.Run("access control: a member of one gym is a stranger in the other", func(t *testing.T) {
		valid := h.check(t, admin, tn.BranchA, document)
		assert.Equal(t, true, valid.data()["allowed"])
		foreign := h.check(t, adminTwo, tn.BranchC, document)
		assert.Equal(t, false, foreign.data()["allowed"])
		assert.Equal(t, "NOT_REGISTERED", foreign.data()["reason"])
		byID := h.call(t, http.MethodPost, "/access/checks", adminTwo, map[string]any{"branch_id": tn.BranchC, "member_id": memberID})
		assert.Equal(t, 404, byID.status, "a member id of the sibling gym does not exist here")
		_ = today
	})

	t.Run("the database refuses cross-gym links", func(t *testing.T) {
		ctx := context.Background()
		_, err := h.pool.Exec(ctx, `INSERT INTO payments (id, gym_id, receipt_id, branch_id, kind, method, amount, paid_on, member_id, created_by)
			SELECT $1, $2, p.receipt_id, $3, 'membership', 'cash', 1, current_date, $4, $5 FROM payments p WHERE p.member_id = $4`,
			uuid.New(), tn.GymTwo, tn.BranchC, memberID, tn.AdminTwoID)
		assert.Error(t, err, "a payment of GymTwo cannot point to a member or receipt of GymOne")
		_, err = h.pool.Exec(ctx, `INSERT INTO access_entries (id, gym_id, branch_id, member_id, display_name, allowed, reason_code, kind, checked_by)
			VALUES ($1, $2, $3, $4, 'X', true, 'MEMBERSHIP_VALID', 'check', $5)`, uuid.New(), tn.GymTwo, tn.BranchC, memberID, tn.AdminTwoID)
		assert.Error(t, err, "nor an access entry")
		_, err = h.pool.Exec(ctx, `INSERT INTO memberships (id, gym_id, member_id, months, starts_on, ends_on, amount, access_scope)
			VALUES ($1, $2, $3, 1, current_date, current_date + 30, 1, 'network')`, uuid.New(), tn.GymOne, memberID)
		assert.Error(t, err, "the network scope no longer exists")
	})
}

func TestReceiptSeriesAreIndependentPerGym(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, adminTwo := h.token(t, tn.Admin), h.token(t, tn.AdminTwo)

	number := func(res response) string {
		require.Equal(t, 201, res.status, res.body)
		return res.data()["receipt"].(map[string]any)["number"].(string)
	}
	assert.Equal(t, "R-000001", number(h.enroll(t, admin, enrollBody(tn.BranchA, uniqueDocument()))))
	assert.Equal(t, "R-000002", number(h.enroll(t, admin, enrollBody(tn.BranchB, uniqueDocument()))), "branches of one gym share the series")
	assert.Equal(t, "R-000001", number(h.enroll(t, adminTwo, enrollBody(tn.BranchC, uniqueDocument()))), "a sibling gym starts its own")

	day := func(token string, branch uuid.UUID) string {
		res := h.call(t, http.MethodPost, "/day-passes", token, map[string]any{"branch_id": branch, "method": "cash"})
		require.Equal(t, 201, res.status, res.body)
		return res.data()["receipt"].(map[string]any)["number"].(string)
	}
	assert.Equal(t, "D-000001", day(admin, tn.BranchA))
	assert.Equal(t, "D-000001", day(adminTwo, tn.BranchC))
	assert.Equal(t, "D-000002", day(admin, tn.BranchB))

	var counters int
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM receipt_counters WHERE gym_id IN ($1, $2)`, tn.GymOne, tn.GymTwo).Scan(&counters))
	assert.Equal(t, 4, counters, "one counter per gym and kind")
}

func TestMemberRoleIsNotActiveYet(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	value, _ := h.people.Load(tn.Admin)
	claims := h.userClaims(value.(person))
	claims["app_role"] = "member"
	res := h.call(t, http.MethodGet, "/me", h.tokens.Mint(t, claims), nil)
	assert.Equal(t, 403, res.status)
	assert.Equal(t, "NO_ACCESS", res.errorCode(), "the future member role is still refused")

	var column string
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT is_nullable FROM information_schema.columns
		WHERE table_name = 'members' AND column_name = 'auth_user_id'`).Scan(&column))
	assert.Equal(t, "YES", column, "reserved for that role, nullable and unused")
}
