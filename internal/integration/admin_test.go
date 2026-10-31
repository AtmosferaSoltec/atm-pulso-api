package integration_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
)

// gymOfCompany finds the gym of a company (and its group) in the answer of GET /admin/groups.
func gymOfCompany(t *testing.T, groups response, authTenant uuid.UUID) (group, gym map[string]any) {
	t.Helper()
	for _, rawGroup := range groups.body["data"].([]any) {
		group := rawGroup.(map[string]any)
		for _, rawGym := range group["gyms"].([]any) {
			gym := rawGym.(map[string]any)
			if gym["auth_tenant_id"] == authTenant.String() {
				return group, gym
			}
		}
	}
	return nil, nil
}

func TestSuperadminSeesGroupsFromAtmAuthApi(t *testing.T) {
	h := newHarness(t)
	super := h.superToken(t)
	client, one, two := uuid.New(), uuid.New(), uuid.New()
	h.cleanupClient(t, client)
	h.auth.SetTenant(authapi.Tenant{ID: one.String(), RUC: "20111111111", DisplayName: "Sede Norte Gym", ClientID: client.String(), ClientName: "Cadena Fitness"})
	h.auth.SetTenant(authapi.Tenant{ID: two.String(), RUC: "20222222222", LegalName: "SUR SAC", ClientID: client.String(), ClientName: "Cadena Fitness"})
	h.auth.SetTenant(authapi.Tenant{ID: uuid.NewString(), RUC: "20333333333", DisplayName: "Sin cliente"})
	noRUC := uuid.New()
	h.auth.SetTenant(authapi.Tenant{ID: noRUC.String(), LegalName: "Gimnasio Sin RUC", ClientID: client.String(), ClientName: "Cadena Fitness"})

	res := h.call(t, http.MethodGet, "/admin/groups", super, nil)
	require.Equal(t, http.StatusOK, res.status, res.body)
	group, gymOne := gymOfCompany(t, res, one)
	require.NotNil(t, group, "the company with Pulso enabled appears")
	assert.Equal(t, "Cadena Fitness", group["name"], "a new gym starts in a group named after its client")
	assert.Equal(t, client.String(), gymOne["auth_client_id"])
	assert.Equal(t, group["id"], gymOne["group_id"])
	assert.NotContains(t, group, "auth_client_id", "a group is not tied to an auth client")
	assert.Len(t, group["gyms"], 3, "every company of the client starts in the same group")
	assert.Equal(t, "#17677b", gymOne["brand_color"])
	_, withoutRUC := gymOfCompany(t, res, noRUC)
	assert.Equal(t, "Gimnasio Sin RUC", withoutRUC["name"], "a company without RUC is still a gym")
	assert.Nil(t, withoutRUC["ruc"])
	assert.Equal(t, "Sede Norte Gym", gymOne["name"])
	assert.Equal(t, "20111111111", gymOne["ruc"])
	branches := gymOne["branches"].([]any)
	require.Len(t, branches, 1)
	assert.Equal(t, "Sede principal", branches[0].(map[string]any)["name"], "every gym starts with one branch")
	_, gymTwo := gymOfCompany(t, res, two)
	assert.Equal(t, "SUR SAC", gymTwo["name"])

	again := h.call(t, http.MethodGet, "/admin/groups", super, nil)
	_, sameGym := gymOfCompany(t, again, one)
	assert.Equal(t, gymOne["id"], sameGym["id"], "syncing again changes nothing")
	assert.Len(t, sameGym["branches"], 1)

	h.auth.Err = authapi.ErrUnavailable
	offline := h.call(t, http.MethodGet, "/admin/groups", super, nil)
	h.auth.Err = nil
	assert.Equal(t, http.StatusOK, offline.status, "without atm-auth-api the stored groups are still listed")
	group, _ = gymOfCompany(t, offline, one)
	assert.NotNil(t, group)
}

func TestSuperadminSetsTheGymColour(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	super := h.superToken(t)
	path := "/admin/gyms/" + tn.GymOne.String()

	res := h.call(t, http.MethodPatch, path, super, map[string]any{"brand_color": "#abcdef"})
	require.Equal(t, http.StatusOK, res.status, res.body)
	assert.Equal(t, "#abcdef", res.data()["brand_color"])
	assert.Len(t, res.data()["branches"], 2)
	me := h.call(t, http.MethodGet, "/me", h.token(t, tn.Admin), nil)
	assert.Equal(t, "#abcdef", me.data()["gym"].(map[string]any)["brand_color"], "the gym shows the colour the console set")

	for _, body := range []map[string]any{{}, {"brand_color": nil}, {"brand_color": "#ABCDEF"}, {"brand_color": "blue"}} {
		assert.Equal(t, http.StatusBadRequest, h.call(t, http.MethodPatch, path, super, body).status, body)
	}
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodPatch, "/admin/gyms/"+uuid.NewString(), super, map[string]any{"brand_color": "#abcdef"}).status)
	assert.Equal(t, http.StatusForbidden, h.call(t, http.MethodPatch, path, h.token(t, tn.Admin), map[string]any{"brand_color": "#123456"}).status)
}

func TestOnlyTheSuperadminReachesTheAdminRoutes(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	for name, token := range map[string]string{"nobody": "", "admin": h.token(t, tn.Admin), "receptionist": h.token(t, tn.Receptionist)} {
		for _, call := range []struct{ method, path string }{
			{http.MethodGet, "/admin/groups"},
			{http.MethodPost, "/admin/groups"},
			{http.MethodPatch, "/admin/groups/" + tn.GroupID.String()},
			{http.MethodDelete, "/admin/groups/" + tn.GroupID.String()},
			{http.MethodPatch, "/admin/gyms/" + tn.GymOne.String()},
			{http.MethodPost, "/admin/gyms/" + tn.GymOne.String() + "/branches"},
			{http.MethodPatch, "/admin/branches/" + tn.BranchA.String()},
			{http.MethodDelete, "/admin/branches/" + tn.BranchA.String()},
		} {
			res := h.call(t, call.method, call.path, token, map[string]any{"name": "Sede Pirata"})
			want := http.StatusForbidden
			if token == "" {
				want = http.StatusUnauthorized
			}
			assert.Equal(t, want, res.status, name+" "+call.method+" "+call.path)
		}
	}
	var branches int
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM branches WHERE gym_id = $1`, tn.GymOne).Scan(&branches))
	assert.Equal(t, 2, branches, "a gym admin never creates, edits or removes branches")
	assert.Equal(t, http.StatusForbidden, h.call(t, http.MethodGet, "/me", h.superToken(t), nil).status, "and the superadmin token is refused on people routes")
}

func TestSuperadminManagesBranches(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	super := h.superToken(t)
	admin := h.token(t, tn.Admin)

	created := h.call(t, http.MethodPost, "/admin/gyms/"+tn.GymOne.String()+"/branches", super, map[string]any{"name": "  Sede   Nueva ", "address": " Av. Lima 123 "})
	require.Equal(t, http.StatusCreated, created.status, created.body)
	branch := created.data()["id"].(string)
	assert.Equal(t, "Sede Nueva", created.data()["name"])
	assert.Equal(t, "Av. Lima 123", created.data()["address"])

	catalog := h.call(t, http.MethodGet, "/catalog", admin, nil)
	count := 0
	for _, rawGym := range catalog.data()["gyms"].([]any) {
		for _, rawBranch := range rawGym.(map[string]any)["branches"].([]any) {
			b := rawBranch.(map[string]any)
			if b["id"] == branch {
				count++
				assert.Equal(t, true, b["accessible"], "the admin reaches the new branch at once")
			}
		}
	}
	assert.Equal(t, 1, count)

	for _, body := range []map[string]any{{}, {"name": ""}, {"name": "   "}, {"name": string(make([]byte, 81))}} {
		res := h.call(t, http.MethodPost, "/admin/gyms/"+tn.GymOne.String()+"/branches", super, body)
		assert.Equal(t, http.StatusBadRequest, res.status, body)
	}
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodPost, "/admin/gyms/"+uuid.NewString()+"/branches", super, map[string]any{"name": "Sede X"}).status)

	renamed := h.call(t, http.MethodPatch, "/admin/branches/"+branch, super, map[string]any{"name": "Sede Renombrada", "address": nil})
	require.Equal(t, http.StatusOK, renamed.status, renamed.body)
	assert.Equal(t, "Sede Renombrada", renamed.data()["name"])
	assert.Nil(t, renamed.data()["address"])
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodPatch, "/admin/branches/"+uuid.NewString(), super, map[string]any{"name": "Sede X"}).status)
	assert.Equal(t, http.StatusBadRequest, h.call(t, http.MethodPatch, "/admin/branches/"+branch, super, map[string]any{"name": nil}).status)

	t.Run("a branch with history or people cannot be removed", func(t *testing.T) {
		assign := h.call(t, http.MethodPatch, "/users/"+tn.ReceptionistID.String(), admin, map[string]any{"branch_ids": []string{branch}})
		require.Equal(t, http.StatusOK, assign.status, assign.body)
		res := h.call(t, http.MethodDelete, "/admin/branches/"+branch, super, nil)
		assert.Equal(t, http.StatusConflict, res.status)
		assert.Equal(t, "BRANCH_IN_USE", res.errorCode())

		sold := h.enroll(t, admin, enrollBody(tn.BranchA, uniqueDocument()))
		require.Equal(t, http.StatusCreated, sold.status, sold.body)
		assert.Equal(t, "BRANCH_IN_USE", h.call(t, http.MethodDelete, "/admin/branches/"+tn.BranchA.String(), super, nil).errorCode(), "sales are history")
	})

	t.Run("an unused branch is removed, the last one of a gym never", func(t *testing.T) {
		spare := h.call(t, http.MethodPost, "/admin/gyms/"+tn.GymOne.String()+"/branches", super, map[string]any{"name": "Sede Sobrante"})
		require.Equal(t, http.StatusCreated, spare.status)
		assert.Equal(t, http.StatusNoContent, h.call(t, http.MethodDelete, "/admin/branches/"+spare.data()["id"].(string), super, nil).status)
		assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodDelete, "/admin/branches/"+spare.data()["id"].(string), super, nil).status)
		assert.Equal(t, "BRANCH_IN_USE", h.call(t, http.MethodDelete, "/admin/branches/"+tn.BranchC.String(), super, nil).errorCode(), "GymTwo has only this branch")
	})
}

func TestSuperadminManagesGroups(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	super := h.superToken(t)
	admin := h.token(t, tn.Admin)

	created := h.call(t, http.MethodPost, "/admin/groups", super, map[string]any{"name": "  Tres   Marcas "})
	require.Equal(t, http.StatusCreated, created.status, created.body)
	group := created.data()["id"].(string)
	t.Cleanup(func() { _, _ = h.pool.Exec(context.Background(), `DELETE FROM groups WHERE id = $1`, group) })
	assert.Equal(t, "Tres Marcas", created.data()["name"])
	assert.Len(t, created.data()["gyms"], 0)
	for _, body := range []map[string]any{{}, {"name": ""}, {"name": "   "}, {"name": string(make([]byte, 81))}} {
		assert.Equal(t, http.StatusBadRequest, h.call(t, http.MethodPost, "/admin/groups", super, body).status, body)
	}

	renamed := h.call(t, http.MethodPatch, "/admin/groups/"+group, super, map[string]any{"name": "Marcas de Ana"})
	require.Equal(t, http.StatusOK, renamed.status, renamed.body)
	assert.Equal(t, "Marcas de Ana", renamed.data()["name"])
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodPatch, "/admin/groups/"+uuid.NewString(), super, map[string]any{"name": "X"}).status)

	t.Run("moving a gym changes its group and nothing else", func(t *testing.T) {
		moved := h.call(t, http.MethodPatch, "/admin/gyms/"+tn.GymOne.String(), super, map[string]any{"group_id": group})
		require.Equal(t, http.StatusOK, moved.status, moved.body)
		assert.Equal(t, group, moved.data()["group_id"])
		assert.Equal(t, "#17677b", moved.data()["brand_color"])
		assert.Len(t, moved.data()["branches"], 2)
		me := h.call(t, http.MethodGet, "/me", admin, nil)
		assert.Equal(t, http.StatusOK, me.status, "the gym admin keeps working and never sees the group")
		sold := h.enroll(t, admin, enrollBody(tn.BranchA, uniqueDocument()))
		assert.Equal(t, http.StatusCreated, sold.status, sold.body)
		assert.Equal(t, "R-000001", sold.data()["receipt"].(map[string]any)["number"], "regrouping does not touch the series")

		assert.Equal(t, "SCOPE_NOT_ALLOWED", h.call(t, http.MethodPatch, "/admin/gyms/"+tn.GymOne.String(), super, map[string]any{"group_id": uuid.NewString()}).errorCode())
		assert.Equal(t, http.StatusBadRequest, h.call(t, http.MethodPatch, "/admin/gyms/"+tn.GymOne.String(), super, map[string]any{"group_id": nil}).status)
	})

	t.Run("a group with gyms cannot be removed", func(t *testing.T) {
		res := h.call(t, http.MethodDelete, "/admin/groups/"+group, super, nil)
		assert.Equal(t, http.StatusConflict, res.status)
		assert.Equal(t, "GROUP_IN_USE", res.errorCode())
		back := h.call(t, http.MethodPatch, "/admin/gyms/"+tn.GymOne.String(), super, map[string]any{"group_id": tn.GroupID.String()})
		require.Equal(t, http.StatusOK, back.status, back.body)
		assert.Equal(t, http.StatusNoContent, h.call(t, http.MethodDelete, "/admin/groups/"+group, super, nil).status)
		assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodDelete, "/admin/groups/"+group, super, nil).status)
	})
}
