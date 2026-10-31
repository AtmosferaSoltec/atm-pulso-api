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

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

func limaToday() time.Time { return clock.LimaDate(time.Now()) }

func iso(t time.Time) string { return t.Format("2006-01-02") }

// enrollBody is a valid enrollment at a branch; tests override what they check.
func enrollBody(branch uuid.UUID, document string) map[string]any {
	return map[string]any{
		"branch_id": branch, "document_type": "dni", "document_number": document, "full_name": "Ana Prueba",
		"phone": "999111222", "months": 3, "amount": "270", "method": "cash",
	}
}

func (h harness) enroll(t *testing.T, token string, body map[string]any) response {
	t.Helper()
	return h.call(t, http.MethodPost, "/members", token, body)
}

func TestEnrollmentCreatesMemberPeriodReceiptAndPayment(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	reception := h.token(t, tn.Receptionist)
	document := uniqueDocument()

	res := h.enroll(t, reception, enrollBody(tn.BranchA, document))
	require.Equal(t, http.StatusCreated, res.status, res.body)
	data := res.data()
	member := data["member"].(map[string]any)
	membership := data["membership"].(map[string]any)
	receipt := data["receipt"].(map[string]any)
	assert.Equal(t, "R-000001", receipt["number"])
	assert.Equal(t, "active", member["status"])
	assert.Equal(t, "3 meses", member["plan_label"])
	assert.Equal(t, "270.00", membership["amount"])
	assert.Equal(t, iso(limaToday()), membership["starts_on"])
	assert.Equal(t, iso(limaToday().AddDate(0, 3, 0)), membership["ends_on"])
	assert.Equal(t, "branch", membership["access_scope"])
	assert.Len(t, membership["access_branches"], 1)
	home := member["home_branch"].(map[string]any)
	assert.Equal(t, tn.BranchA.String(), home["id"])

	var members, memberships, receipts, payments int
	require.NoError(t, h.pool.QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM members WHERE gym_id=$1), (SELECT count(*) FROM memberships WHERE gym_id=$1),
		(SELECT count(*) FROM receipts WHERE gym_id=$1), (SELECT count(*) FROM payments WHERE gym_id=$1)`, tn.GymOne).
		Scan(&members, &memberships, &receipts, &payments))
	assert.Equal(t, []int{1, 1, 1, 1}, []int{members, memberships, receipts, payments})

	// The printable receipt.
	printable := h.call(t, http.MethodGet, "/receipts/"+receipt["id"].(string), reception, nil)
	require.Equal(t, 200, printable.status, printable.body)
	assert.Equal(t, "R-000001", printable.data()["number"])
	assert.Equal(t, "Gym Uno", printable.data()["gym"].(map[string]any)["name"])
	assert.Equal(t, "270.00", printable.data()["payment"].(map[string]any)["amount"])
	list := h.call(t, http.MethodGet, "/members/"+member["id"].(string)+"/receipts", reception, nil)
	require.Equal(t, 200, list.status)
	assert.Len(t, list.body["data"], 1)
}

func TestEnrollmentValidationDuplicatesAndIdempotency(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	admin := h.token(t, tn.Admin)
	document := uniqueDocument()

	bad := enrollBody(tn.BranchA, "123")
	bad["phone"], bad["months"], bad["amount"], bad["method"] = "123", 0, "-5", "bitcoin"
	res := h.enroll(t, admin, bad)
	assert.Equal(t, 400, res.status)
	assert.Equal(t, "VALIDATION_FAILED", res.errorCode())
	ce := enrollBody(tn.BranchA, "12345678")
	ce["document_type"] = "ce"
	assert.Equal(t, 400, h.enroll(t, admin, ce).status, "a CE has 9 to 12 digits")
	ce["document_number"] = "123456789"
	assert.Equal(t, 201, h.enroll(t, admin, ce).status)

	id := uuid.New()
	body := enrollBody(tn.BranchA, document)
	body["id"] = id
	first := h.enroll(t, admin, body)
	require.Equal(t, 201, first.status, first.body)
	again := h.enroll(t, admin, body)
	assert.Equal(t, 200, again.status, "repeating the same id returns the original sale")
	assert.Equal(t, first.data()["receipt"], again.data()["receipt"])

	dup := h.enroll(t, admin, enrollBody(tn.BranchB, document))
	assert.Equal(t, 409, dup.status)
	assert.Equal(t, "MEMBER_DOCUMENT_TAKEN", dup.errorCode())
	details := dup.body["error"].(map[string]any)["details"].(map[string]any)
	assert.Equal(t, id.String(), details["member_id"])

	// The same document in another gym is a different person.
	assert.Equal(t, 201, h.enroll(t, h.token(t, other.Admin), enrollBody(other.BranchA, document)).status)

	// The failed duplicate did not burn a receipt number: the next sale is R-000003 (CE, first, next).
	next := h.enroll(t, admin, enrollBody(tn.BranchB, uniqueDocument()))
	assert.Equal(t, "R-000003", next.data()["receipt"].(map[string]any)["number"])
}

func TestEnrollmentRolesAndTenancy(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	reception := h.token(t, tn.Receptionist)
	admin := h.token(t, tn.Admin)

	res := h.enroll(t, reception, enrollBody(tn.BranchC, uniqueDocument()))
	assert.Equal(t, 400, res.status, "a branch of a sibling gym of the same group is foreign")
	assert.Equal(t, "SCOPE_NOT_ALLOWED", res.errorCode())
	assert.Equal(t, "SCOPE_NOT_ALLOWED", h.enroll(t, admin, enrollBody(tn.BranchC, uniqueDocument())).errorCode(), "an admin only reaches its gym")
	unassigned := h.enroll(t, reception, enrollBody(h.extraBranch(t, tn), uniqueDocument()))
	assert.Equal(t, 403, unassigned.status)
	assert.Equal(t, "BRANCH_NOT_ACCESSIBLE", unassigned.errorCode(), "reception works only in assigned branches")
	foreign := h.enroll(t, reception, enrollBody(other.BranchA, uniqueDocument()))
	assert.Equal(t, 400, foreign.status)
	assert.Equal(t, "SCOPE_NOT_ALLOWED", foreign.errorCode())

	sale := h.enroll(t, reception, enrollBody(tn.BranchA, uniqueDocument()))
	require.Equal(t, 201, sale.status)
	memberID := sale.data()["member"].(map[string]any)["id"].(string)
	receiptID := sale.data()["receipt"].(map[string]any)["id"].(string)
	otherAdmin := h.token(t, other.Admin)
	for _, path := range []string{"/members/" + memberID, "/members/" + memberID + "/receipts", "/receipts/" + receiptID} {
		assert.Equal(t, 404, h.call(t, http.MethodGet, path, otherAdmin, nil).status, path)
	}
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/members/"+memberID, otherAdmin, map[string]string{"full_name": "Hacker Name"}).status)
	renewal := h.call(t, http.MethodPost, "/members/"+memberID+"/renewals", otherAdmin, map[string]any{"branch_id": other.BranchA, "months": 1, "amount": "100", "method": "cash"})
	assert.Equal(t, 404, renewal.status)

	assert.Equal(t, 401, h.call(t, http.MethodGet, "/members", "", nil).status)
}

func TestAccessBranchesOfAMembership(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin := h.token(t, tn.Admin)

	body := enrollBody(tn.BranchA, uniqueDocument())
	body["access_scope"] = "gym"
	res := h.enroll(t, admin, body)
	require.Equal(t, 201, res.status, res.body)
	assert.Len(t, res.data()["membership"].(map[string]any)["access_branches"], 2, "gym scope opens A and B")

	body = enrollBody(tn.BranchA, uniqueDocument())
	body["access_scope"] = "gym"
	body["access_branch_ids"] = []uuid.UUID{tn.BranchA, tn.BranchB}
	res = h.enroll(t, admin, body)
	require.Equal(t, 201, res.status, res.body)
	assert.Len(t, res.data()["membership"].(map[string]any)["access_branches"], 2)

	body = enrollBody(tn.BranchA, uniqueDocument())
	body["access_scope"] = "network"
	res = h.enroll(t, admin, body)
	assert.Equal(t, 400, res.status, "the network scope no longer exists")
	assert.Equal(t, "VALIDATION_FAILED", res.errorCode())

	for name, ids := range map[string][]uuid.UUID{"home missing": {tn.BranchB, tn.BranchC}, "outside the scope": {tn.BranchA, tn.BranchC}} {
		body = enrollBody(tn.BranchA, uniqueDocument())
		body["access_scope"] = "gym"
		body["access_branch_ids"] = ids
		res = h.enroll(t, admin, body)
		assert.Equal(t, 400, res.status, name)
		assert.Equal(t, "ACCESS_BRANCHES_INVALID", res.errorCode(), name)
	}
}

func TestRenewalDatesAndHomeBranch(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin := h.token(t, tn.Admin)
	today := limaToday()

	// Active member (ends in the future): the renewal starts at the current end.
	active := h.enroll(t, admin, enrollBody(tn.BranchA, uniqueDocument()))
	activeID := active.data()["member"].(map[string]any)["id"].(string)
	currentEnd := active.data()["membership"].(map[string]any)["ends_on"].(string)
	renewed := h.call(t, http.MethodPost, "/members/"+activeID+"/renewals", admin,
		map[string]any{"branch_id": tn.BranchB, "months": 1, "amount": "100.50", "method": "yape_plin"})
	require.Equal(t, 201, renewed.status, renewed.body)
	assert.Equal(t, currentEnd, renewed.data()["membership"].(map[string]any)["starts_on"])
	assert.Equal(t, "R-000002", renewed.data()["receipt"].(map[string]any)["number"])
	assert.Equal(t, tn.BranchA.String(), renewed.data()["member"].(map[string]any)["home_branch"].(map[string]any)["id"], "the home branch does not change")

	// Expired member: the renewal starts today.
	old := enrollBody(tn.BranchA, uniqueDocument())
	old["starts_on"], old["months"] = iso(today.AddDate(-1, 0, 0)), 1
	expired := h.enroll(t, admin, old)
	require.Equal(t, 201, expired.status, expired.body)
	assert.Equal(t, "expired", expired.data()["member"].(map[string]any)["status"])
	expiredID := expired.data()["member"].(map[string]any)["id"].(string)
	back := h.call(t, http.MethodPost, "/members/"+expiredID+"/renewals", admin, map[string]any{"branch_id": tn.BranchA, "months": 2, "amount": "190", "method": "card"})
	require.Equal(t, 201, back.status, back.body)
	assert.Equal(t, iso(today), back.data()["membership"].(map[string]any)["starts_on"])
	assert.Equal(t, "active", back.data()["member"].(map[string]any)["status"])

	// Idempotent renewal.
	periodID := uuid.New()
	req := map[string]any{"id": periodID, "branch_id": tn.BranchA, "months": 1, "amount": "100", "method": "cash"}
	assert.Equal(t, 201, h.call(t, http.MethodPost, "/members/"+expiredID+"/renewals", admin, req).status)
	assert.Equal(t, 200, h.call(t, http.MethodPost, "/members/"+expiredID+"/renewals", admin, req).status)
	detail := h.call(t, http.MethodGet, "/members/"+expiredID, admin, nil)
	assert.Len(t, detail.data()["memberships"], 3)
}

func TestMemberListStatusCountsAndFilters(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin := h.token(t, tn.Admin)
	today := limaToday()
	add := func(name, branch string, start time.Time, months int) {
		t.Helper()
		body := enrollBody(tn.BranchA, uniqueDocument())
		body["full_name"], body["starts_on"], body["months"] = name, iso(start), months
		if branch == "b" {
			body["branch_id"] = tn.BranchB
		}
		require.Equal(t, 201, h.enroll(t, admin, body).status)
	}
	add("Activa Uno", "a", today.AddDate(0, -1, 0), 3)
	add("Por Vencer", "a", today.AddDate(0, -1, 0), 1)
	add("Vencida Uno", "b", today.AddDate(0, -6, 0), 1)
	add("Futura Uno", "a", today.AddDate(0, 0, 10), 1)

	all := h.call(t, http.MethodGet, "/members", admin, nil)
	require.Equal(t, 200, all.status, all.body)
	counts := all.body["meta"].(map[string]any)["counts"].(map[string]any)
	assert.Equal(t, map[string]any{"all": 4.0, "active": 1.0, "expiring": 1.0, "expired": 1.0, "scheduled": 1.0}, counts)
	assert.Equal(t, 4.0, all.body["meta"].(map[string]any)["total"])

	expiring := h.call(t, http.MethodGet, "/members?status=expiring", admin, nil)
	assert.Len(t, expiring.body["data"], 1)
	assert.Equal(t, "Por Vencer", expiring.body["data"].([]any)[0].(map[string]any)["full_name"])
	assert.Equal(t, 4.0, expiring.body["meta"].(map[string]any)["counts"].(map[string]any)["all"], "counts ignore the status filter")

	search := h.call(t, http.MethodGet, "/members?q=vencida", admin, nil)
	assert.Len(t, search.body["data"], 1)
	assert.Equal(t, 1.0, search.body["meta"].(map[string]any)["counts"].(map[string]any)["all"], "counts respect the search")
	assert.Len(t, h.call(t, http.MethodGet, "/members?home_branch_id="+tn.BranchB.String(), admin, nil).body["data"], 1)
	assert.Len(t, h.call(t, http.MethodGet, "/members?q=%25", admin, nil).body["data"], 0, "wildcards are escaped")
	assert.Equal(t, 400, h.call(t, http.MethodGet, "/members?status=nope", admin, nil).status)
	paged := h.call(t, http.MethodGet, "/members?page=2&page_size=3", admin, nil)
	assert.Len(t, paged.body["data"], 1)

	// The directory is shared: reception sees members enrolled in branches it does not work in.
	reception := h.token(t, tn.Receptionist)
	assert.Equal(t, 4.0, h.call(t, http.MethodGet, "/members", reception, nil).body["meta"].(map[string]any)["total"])
}

func TestMemberEditChangesOnlyNameAndPhone(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	reception := h.token(t, tn.Receptionist)
	document := uniqueDocument()
	sale := h.enroll(t, reception, enrollBody(tn.BranchA, document))
	id := sale.data()["member"].(map[string]any)["id"].(string)

	res := h.call(t, http.MethodPatch, "/members/"+id, reception, map[string]any{"full_name": "  María  Nueva ", "phone": "988777666", "document_number": "00000000"})
	require.Equal(t, 200, res.status, res.body)
	assert.Equal(t, "María Nueva", res.data()["full_name"])
	assert.Equal(t, "988777666", res.data()["phone"])
	assert.Equal(t, document, res.data()["document_number"], "the document never changes")
	assert.Equal(t, 400, h.call(t, http.MethodPatch, "/members/"+id, reception, map[string]any{"phone": "12"}).status)
}

func TestReceiptNumbersAreGaplessUnderConcurrency(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin := h.token(t, tn.Admin)
	const sales = 20
	numbers := make([]string, sales)
	var wg sync.WaitGroup
	for i := range sales {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := h.enroll(t, admin, enrollBody(tn.BranchA, uniqueDocument()))
			if res.status == 201 {
				numbers[i] = res.data()["receipt"].(map[string]any)["number"].(string)
			}
		}()
	}
	wg.Wait()
	want := make([]string, sales)
	for i := range sales {
		want[i] = fmt.Sprintf("R-%06d", i+1)
	}
	sort.Strings(numbers)
	assert.Equal(t, want, numbers)
}
