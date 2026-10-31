package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

// setBodyTracking switches the body_tracking module of a gym on or off (the superadmin's PATCH, like console-web).
func (h harness) setBodyTracking(t *testing.T, gym uuid.UUID, on bool) {
	t.Helper()
	features := []string{}
	if on {
		features = []string{"body_tracking"}
	}
	res := h.call(t, http.MethodPatch, "/admin/gyms/"+gym.String()+"/features", h.superToken(t), map[string]any{"features": features})
	require.Equal(t, 200, res.status, res.body)
}

func (h harness) bodyMember(t *testing.T, admin string, branch uuid.UUID) string {
	t.Helper()
	res := h.enroll(t, admin, enrollBody(branch, uniqueDocument()))
	require.Equal(t, 201, res.status, res.body)
	return memberIDOf(res)
}

func today() string { return clock.LimaDate(time.Now()).Format("2006-01-02") }

func daysAgo(n int) string { return clock.LimaDate(time.Now()).AddDate(0, 0, -n).Format("2006-01-02") }

func (h harness) auditRows(t *testing.T, member string) []string {
	t.Helper()
	rows, err := h.pool.Query(context.Background(), `SELECT action || ':' || resource || ':' || (resource_id IS NOT NULL)::text FROM body_access_log
		WHERE member_id = $1 ORDER BY id`, member)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	return out
}

func list(res response) []map[string]any {
	var out []map[string]any
	for _, raw := range res.body["data"].([]any) {
		out = append(out, raw.(map[string]any))
	}
	return out
}

func TestBodyTrackingOffAnswersFeatureDisabledEverywhere(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, reception := h.token(t, tn.Admin), h.token(t, tn.Receptionist)
	member := h.bodyMember(t, admin, tn.BranchA)
	rec := uuid.NewString()
	base := "/members/" + member
	calls := []struct{ method, path string }{
		{"GET", base + "/measurements"}, {"POST", base + "/measurements"}, {"PUT", base + "/measurements/" + rec}, {"DELETE", base + "/measurements/" + rec},
		{"GET", base + "/routines"}, {"POST", base + "/routines"}, {"PUT", base + "/routines/" + rec}, {"DELETE", base + "/routines/" + rec},
		{"GET", base + "/body-access-log"},
	}
	for _, token := range []string{admin, reception} {
		for _, call := range calls {
			res := h.call(t, call.method, call.path, token, map[string]any{"weight_kg": 80, "title": "x"})
			assert.Equal(t, 403, res.status, call.method+" "+call.path)
			assert.Equal(t, "FEATURE_DISABLED", res.errorCode(), call.method+" "+call.path)
		}
	}
	assert.Empty(t, h.auditRows(t, member), "a module that is off writes no audit rows")

	h.setBodyTracking(t, tn.GymOne, true)
	assert.Equal(t, 200, h.call(t, "GET", base+"/measurements", admin, nil).status)
	assert.Equal(t, 401, h.call(t, "GET", base+"/measurements", "", nil).status)
	assert.Equal(t, 403, h.call(t, "GET", base+"/measurements", h.superToken(t), nil).status)
	// The sibling gym of the same group stays off.
	assert.Equal(t, "FEATURE_DISABLED", h.call(t, "GET", base+"/measurements", h.token(t, tn.AdminTwo), nil).errorCode())
	h.setBodyTracking(t, tn.GymOne, false)
	assert.Equal(t, "FEATURE_DISABLED", h.call(t, "GET", base+"/measurements", admin, nil).errorCode(), "switching off is immediate")
}

func TestMeasurementsLifecycleAndValidation(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	h.setBodyTracking(t, tn.GymOne, true)
	admin, reception := h.token(t, tn.Admin), h.token(t, tn.Receptionist)
	member := h.bodyMember(t, admin, tn.BranchA)
	path := "/members/" + member + "/measurements"

	empty := h.call(t, "GET", path, reception, nil)
	require.Equal(t, 200, empty.status)
	assert.Empty(t, list(empty), "no measurements: an empty list")

	// Only the weight; the date defaults to today (Lima). Numbers and strings are both accepted.
	first := h.call(t, "POST", path, reception, map[string]any{"weight_kg": 95.5})
	require.Equal(t, 201, first.status, first.body)
	assert.Equal(t, today(), first.data()["measured_on"])
	assert.EqualValues(t, 95.5, first.data()["weight_kg"])
	assert.Nil(t, first.data()["height_cm"])
	assert.Equal(t, "Recepción de Prueba", first.data()["created_by_name"])
	older := h.call(t, "POST", path, admin, map[string]any{"measured_on": daysAgo(30), "weight_kg": "98", "height_cm": 172.5, "body_fat_pct": "24.8",
		"waist_cm": 101, "chest_cm": 110.2, "hip_cm": 105, "notes": "  Primera medición\r\ncon cinta  "})
	require.Equal(t, 201, older.status, older.body)
	assert.Equal(t, "Primera medición\ncon cinta", older.data()["notes"])
	assert.EqualValues(t, 24.8, older.data()["body_fat_pct"])
	h.call(t, "POST", path, admin, map[string]any{"measured_on": daysAgo(10), "waist_cm": 99.5}) // not even a weight: any one value

	got := list(h.call(t, "GET", path, admin, nil))
	require.Len(t, got, 3)
	assert.Equal(t, []string{today(), daysAgo(10), daysAgo(30)}, []string{got[0]["measured_on"].(string), got[1]["measured_on"].(string), got[2]["measured_on"].(string)}, "newest first")

	for name, bad := range map[string]map[string]any{
		"no value": {}, "only notes": {"notes": "hola"}, "only date": {"measured_on": today()},
		"future": {"measured_on": time.Now().AddDate(0, 0, 3).Format("2006-01-02"), "weight_kg": 80}, "old": {"measured_on": "1999-12-31", "weight_kg": 80},
		"date text": {"measured_on": "ayer", "weight_kg": 80}, "weight low": {"weight_kg": 19.9}, "weight high": {"weight_kg": 400.1},
		"weight decimals": {"weight_kg": "80.55"}, "weight text": {"weight_kg": "abc"}, "weight negative": {"weight_kg": -3}, "height": {"height_cm": 49},
		"fat": {"body_fat_pct": 71}, "fat low": {"body_fat_pct": 1.9}, "waist": {"waist_cm": 301}, "chest": {"chest_cm": 5}, "hip": {"hip_cm": 0},
		"notes long": {"weight_kg": 80, "notes": strings.Repeat("x", 501)},
	} {
		res := h.call(t, "POST", path, admin, bad)
		assert.Equal(t, 400, res.status, name)
		assert.Equal(t, "VALIDATION_FAILED", res.errorCode(), name)
	}
	assert.Equal(t, 400, h.call(t, "POST", path, admin, map[string]any{"weight_kg": true}).status)
	assert.Equal(t, 201, h.call(t, "POST", path, admin, map[string]any{"measured_on": today(), "weight_kg": 400}).status, "the limits are inclusive")

	// Idempotent by id.
	id := uuid.NewString()
	assert.Equal(t, 201, h.call(t, "POST", path, admin, map[string]any{"id": id, "weight_kg": 90}).status)
	again := h.call(t, "POST", path, admin, map[string]any{"id": id, "weight_kg": 91})
	assert.Equal(t, 200, again.status)
	assert.EqualValues(t, 90, again.data()["weight_kg"], "the retry answers the stored record")
	assert.Len(t, list(h.call(t, "GET", path, admin, nil)), 5)

	// Edit replaces the record; who and when change.
	recordID := first.data()["id"].(string)
	updated := h.call(t, "PUT", path+"/"+recordID, admin, map[string]any{"measured_on": daysAgo(1), "weight_kg": 94.9, "notes": "corregido"})
	require.Equal(t, 200, updated.status, updated.body)
	assert.EqualValues(t, 94.9, updated.data()["weight_kg"])
	assert.Equal(t, daysAgo(1), updated.data()["measured_on"])
	assert.Equal(t, "Admin de Prueba", updated.data()["updated_by_name"])
	assert.Equal(t, "Recepción de Prueba", updated.data()["created_by_name"])
	assert.Equal(t, 400, h.call(t, "PUT", path+"/"+recordID, admin, map[string]any{"notes": "sin valores"}).status)
	assert.Equal(t, "MEASUREMENT_NOT_FOUND", h.call(t, "PUT", path+"/"+uuid.NewString(), admin, map[string]any{"weight_kg": 80}).errorCode())
	assert.Equal(t, 404, h.call(t, "PUT", path+"/not-a-uuid", admin, map[string]any{"weight_kg": 80}).status)

	// Logical delete: it leaves the list, stays in the table with who and when; a second delete is a 404.
	assert.Equal(t, 204, h.call(t, "DELETE", path+"/"+recordID, reception, nil).status)
	assert.Equal(t, "MEASUREMENT_NOT_FOUND", h.call(t, "DELETE", path+"/"+recordID, reception, nil).errorCode())
	assert.Equal(t, "MEASUREMENT_NOT_FOUND", h.call(t, "PUT", path+"/"+recordID, admin, map[string]any{"weight_kg": 80}).errorCode())
	assert.Len(t, list(h.call(t, "GET", path, admin, nil)), 4)
	var deletedBy *uuid.UUID
	var deletedAt *time.Time
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT deleted_by, deleted_at FROM body_measurements WHERE id = $1`, recordID).Scan(&deletedBy, &deletedAt))
	require.NotNil(t, deletedBy)
	assert.Equal(t, tn.ReceptionistID, *deletedBy)
	assert.NotNil(t, deletedAt)
	assert.Equal(t, 409, h.call(t, "POST", path, admin, map[string]any{"id": recordID, "weight_kg": 80}).status, "a deleted id is not reused")
}

func TestRoutinesLifecycleAndValidation(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	h.setBodyTracking(t, tn.GymOne, true)
	admin, reception := h.token(t, tn.Admin), h.token(t, tn.Receptionist)
	member := h.bodyMember(t, admin, tn.BranchA)
	path := "/members/" + member + "/routines"

	assert.Empty(t, list(h.call(t, "GET", path, admin, nil)))
	old := h.call(t, "POST", path, admin, map[string]any{"started_on": daysAgo(60), "title": "  Fuerza   A ", "content": "Sentadilla 4x8\nPress banca 4x8", "is_current": false})
	require.Equal(t, 201, old.status, old.body)
	assert.Equal(t, "Fuerza A", old.data()["title"])
	assert.Equal(t, false, old.data()["is_current"])
	v1 := h.call(t, "POST", path, reception, map[string]any{"started_on": daysAgo(20), "title": "Hipertrofia", "content": "Pecho/espalda", "notes": "dolor de rodilla"})
	require.Equal(t, 201, v1.status, v1.body)
	assert.Equal(t, true, v1.data()["is_current"], "current by default")
	v2 := h.call(t, "POST", path, admin, map[string]any{"title": "Definición"})
	require.Equal(t, 201, v2.status, v2.body)
	assert.Equal(t, today(), v2.data()["started_on"])
	assert.Equal(t, "", v2.data()["content"])

	got := list(h.call(t, "GET", path, admin, nil))
	require.Len(t, got, 3)
	assert.Equal(t, []any{"Definición", "Hipertrofia", "Fuerza A"}, []any{got[0]["title"], got[1]["title"], got[2]["title"]}, "current first, then newest start")

	// Marking an old one current puts it first among the current ones by start date; unmarking sends it down.
	put := h.call(t, "PUT", path+"/"+old.data()["id"].(string), admin, map[string]any{"started_on": daysAgo(60), "title": "Fuerza A", "content": "x", "is_current": true})
	require.Equal(t, 200, put.status, put.body)
	h.call(t, "PUT", path+"/"+v2.data()["id"].(string), admin, map[string]any{"title": "Definición", "is_current": false})
	got = list(h.call(t, "GET", path, admin, nil))
	assert.Equal(t, []any{"Hipertrofia", "Fuerza A", "Definición"}, []any{got[0]["title"], got[1]["title"], got[2]["title"]})

	for name, bad := range map[string]map[string]any{
		"no title": {"content": "x"}, "blank title": {"title": "   "}, "long title": {"title": strings.Repeat("t", 81)},
		"long content": {"title": "A", "content": strings.Repeat("c", 5001)}, "long notes": {"title": "A", "notes": strings.Repeat("n", 501)},
		"future": {"title": "A", "started_on": time.Now().AddDate(0, 0, 2).Format("2006-01-02")}, "bad date": {"title": "A", "started_on": "31-12-2025"},
		"control char": {"title": "A", "content": "bad\x01"},
	} {
		res := h.call(t, "POST", path, admin, bad)
		assert.Equal(t, 400, res.status, name)
		assert.Equal(t, "VALIDATION_FAILED", res.errorCode(), name)
	}
	assert.Equal(t, 201, h.call(t, "POST", path, admin, map[string]any{"title": "Larga", "content": strings.Repeat("ñ", 5000)}).status, "5000 characters fit")

	id := uuid.NewString()
	assert.Equal(t, 201, h.call(t, "POST", path, admin, map[string]any{"id": id, "title": "Única"}).status)
	assert.Equal(t, 200, h.call(t, "POST", path, admin, map[string]any{"id": id, "title": "Única"}).status)

	assert.Equal(t, 204, h.call(t, "DELETE", path+"/"+id, reception, nil).status)
	assert.Equal(t, "ROUTINE_NOT_FOUND", h.call(t, "DELETE", path+"/"+id, reception, nil).errorCode())
	assert.Equal(t, "ROUTINE_NOT_FOUND", h.call(t, "PUT", path+"/"+id, admin, map[string]any{"title": "X"}).errorCode())
	var deleted bool
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT deleted_by IS NOT NULL AND deleted_at IS NOT NULL FROM body_routines WHERE id = $1`, id).Scan(&deleted))
	assert.True(t, deleted)
}

func TestBodyTrackingIsolationBetweenGymsMembersAndBranches(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	for _, gym := range []uuid.UUID{tn.GymOne, tn.GymTwo, other.GymOne} {
		h.setBodyTracking(t, gym, true)
	}
	admin, reception, adminTwo, otherAdmin := h.token(t, tn.Admin), h.token(t, tn.Receptionist), h.token(t, tn.AdminTwo), h.token(t, other.Admin)
	member := h.bodyMember(t, admin, tn.BranchA)
	mine := "/members/" + member
	rec := h.call(t, "POST", mine+"/measurements", admin, map[string]any{"weight_kg": 80, "notes": "PRIVADO-123"})
	require.Equal(t, 201, rec.status)
	routine := h.call(t, "POST", mine+"/routines", admin, map[string]any{"title": "Privada"})
	require.Equal(t, 201, routine.status)
	mID, rID := rec.data()["id"].(string), routine.data()["id"].(string)

	// A member of another gym (same group or another group) does not exist for me, in every operation.
	for _, token := range []string{adminTwo, otherAdmin} {
		for _, call := range []struct{ method, path string }{
			{"GET", mine + "/measurements"}, {"POST", mine + "/measurements"}, {"PUT", mine + "/measurements/" + mID}, {"DELETE", mine + "/measurements/" + mID},
			{"GET", mine + "/routines"}, {"POST", mine + "/routines"}, {"PUT", mine + "/routines/" + rID}, {"DELETE", mine + "/routines/" + rID}, {"GET", mine + "/body-access-log"},
		} {
			res := h.call(t, call.method, call.path, token, map[string]any{"weight_kg": 70, "title": "Hack"})
			assert.Equal(t, 404, res.status, call.method+" "+call.path)
			assert.Equal(t, "MEMBER_NOT_FOUND", res.errorCode(), call.method+" "+call.path)
		}
	}
	assert.Equal(t, 404, h.call(t, "GET", "/members/"+uuid.NewString()+"/measurements", admin, nil).status)

	// A record id of the same gym but another member is not found under this member.
	otherMember := h.bodyMember(t, admin, tn.BranchA)
	assert.Equal(t, "MEASUREMENT_NOT_FOUND", h.call(t, "PUT", "/members/"+otherMember+"/measurements/"+mID, admin, map[string]any{"weight_kg": 70}).errorCode())
	assert.Equal(t, "MEASUREMENT_NOT_FOUND", h.call(t, "DELETE", "/members/"+otherMember+"/measurements/"+mID, admin, nil).errorCode())
	assert.Equal(t, "ROUTINE_NOT_FOUND", h.call(t, "DELETE", "/members/"+otherMember+"/routines/"+rID, admin, nil).errorCode())
	assert.Empty(t, list(h.call(t, "GET", "/members/"+otherMember+"/measurements", admin, nil)))
	// A client id already used by another gym or member is a conflict, never an overwrite or a leak.
	cross := h.call(t, "POST", "/members/"+otherMember+"/measurements", admin, map[string]any{"id": mID, "weight_kg": 70})
	assert.Equal(t, 409, cross.status)
	assert.NotContains(t, cross.body, "PRIVADO")
	assert.Equal(t, 409, h.call(t, "POST", "/members/"+otherMember+"/routines", admin, map[string]any{"id": rID, "title": "Robo"}).status)
	// Nothing changed.
	assert.EqualValues(t, 80, list(h.call(t, "GET", mine+"/measurements", admin, nil))[0]["weight_kg"])

	// Branch: reception works in A and B; a member whose home branch is another one of the gym is out of reach.
	far := h.bodyMember(t, admin, h.extraBranch(t, tn))
	for _, call := range []struct{ method, path string }{
		{"GET", "/members/" + far + "/measurements"}, {"POST", "/members/" + far + "/routines"}, {"GET", "/members/" + far + "/routines"},
	} {
		res := h.call(t, call.method, call.path, reception, map[string]any{"title": "x"})
		assert.Equal(t, 403, res.status, call.path)
		assert.Equal(t, "BRANCH_NOT_ACCESSIBLE", res.errorCode(), call.path)
	}
	assert.Equal(t, 200, h.call(t, "GET", "/members/"+far+"/measurements", admin, nil).status, "the admin reaches every branch")
	branchB := h.bodyMember(t, admin, tn.BranchB)
	assert.Equal(t, 200, h.call(t, "GET", "/members/"+branchB+"/measurements", reception, nil).status)
	for _, row := range h.auditRows(t, far) {
		assert.True(t, strings.HasPrefix(row, "read:"), "only the admin's read is logged, the refused ones are not: %s", row)
	}
}

func TestBodyTrackingAuditLogRecordsWhoWithoutValues(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	h.setBodyTracking(t, tn.GymOne, true)
	admin, reception := h.token(t, tn.Admin), h.token(t, tn.Receptionist)
	member := h.bodyMember(t, admin, tn.BranchA)
	base := "/members/" + member

	m := h.call(t, "POST", base+"/measurements", reception, map[string]any{"weight_kg": 91.3, "notes": "NOTA-SECRETA"})
	require.Equal(t, 201, m.status)
	mid := m.data()["id"].(string)
	h.call(t, "GET", base+"/measurements", admin, nil)
	h.call(t, "PUT", base+"/measurements/"+mid, admin, map[string]any{"weight_kg": 90})
	h.call(t, "DELETE", base+"/measurements/"+mid, reception, nil)
	r := h.call(t, "POST", base+"/routines", admin, map[string]any{"title": "T", "content": "CONTENIDO-SECRETO"})
	h.call(t, "GET", base+"/routines", reception, nil)
	h.call(t, "PUT", base+"/routines/"+r.data()["id"].(string), admin, map[string]any{"title": "T2"})
	h.call(t, "DELETE", base+"/routines/"+r.data()["id"].(string), admin, nil)
	// Failed requests leave no row: validation, missing record, foreign member.
	h.call(t, "POST", base+"/measurements", admin, map[string]any{})
	h.call(t, "DELETE", base+"/measurements/"+uuid.NewString(), admin, nil)

	assert.Equal(t, []string{
		"create:measurements:true", "read:measurements:false", "update:measurements:true", "delete:measurements:true",
		"create:routines:true", "read:routines:false", "update:routines:true", "delete:routines:true",
	}, h.auditRows(t, member))

	// The log shows who, what and when, and no content.
	log := h.call(t, "GET", base+"/body-access-log", admin, nil)
	require.Equal(t, 200, log.status)
	entries := list(log)
	require.Len(t, entries, 8)
	assert.Equal(t, "delete", entries[0]["action"], "newest first")
	assert.Equal(t, "Recepción de Prueba", entries[7]["user_name"])
	assert.Equal(t, tn.ReceptionistID.String(), entries[7]["user_id"])
	raw, _ := json.Marshal(log.body)
	for _, secret := range []string{"NOTA-SECRETA", "CONTENIDO-SECRETO", "91.3", "weight"} {
		assert.NotContains(t, string(raw), secret)
	}
	assert.Equal(t, 403, h.call(t, "GET", base+"/body-access-log", reception, nil).status, "the log is for the admin")

	// The table has no column that could hold a value.
	rows, err := h.pool.Query(context.Background(), `SELECT column_name FROM information_schema.columns WHERE table_name = 'body_access_log' AND table_schema = current_schema()`)
	require.NoError(t, err)
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var c string
		require.NoError(t, rows.Scan(&c))
		columns = append(columns, c)
	}
	assert.ElementsMatch(t, []string{"id", "gym_id", "member_id", "user_id", "action", "resource", "resource_id", "occurred_at"}, columns)
}

func TestBodyDataDoesNotLeakIntoOtherEndpoints(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	h.setBodyTracking(t, tn.GymOne, true)
	admin, reception := h.token(t, tn.Admin), h.token(t, tn.Receptionist)
	member := h.bodyMember(t, admin, tn.BranchA)
	base := "/members/" + member
	require.Equal(t, 201, h.call(t, "POST", base+"/measurements", admin, map[string]any{"weight_kg": 123.4, "waist_cm": 87.6, "notes": "SENSIBLE-XYZ"}).status)
	require.Equal(t, 201, h.call(t, "POST", base+"/routines", admin, map[string]any{"title": "RUTINA-XYZ", "content": "SENSIBLE-XYZ"}).status)

	for _, token := range []string{admin, reception} {
		for _, path := range []string{"/members", "/members?search=Ana", base, base + "/receipts", "/payments", "/dashboard", "/me", "/catalog"} {
			res := h.call(t, "GET", path, token, nil)
			raw, _ := json.Marshal(res.body)
			for _, secret := range []string{"SENSIBLE-XYZ", "RUTINA-XYZ", "123.4", "87.6", "weight_kg", "measurement", "routine"} {
				assert.NotContains(t, string(raw), secret, path)
			}
		}
		csv := h.callRaw(t, "GET", "/payments/export", token)
		for _, secret := range []string{"SENSIBLE-XYZ", "RUTINA-XYZ", "123.4", "87.6"} {
			assert.NotContains(t, csv.Body.String(), secret)
		}
	}
	// With the module off the data stays stored (not erased) but nothing serves it.
	h.setBodyTracking(t, tn.GymOne, false)
	assert.Equal(t, 403, h.call(t, "GET", base+"/measurements", admin, nil).status)
	h.setBodyTracking(t, tn.GymOne, true)
	assert.Len(t, list(h.call(t, "GET", base+"/measurements", admin, nil)), 1)
}
