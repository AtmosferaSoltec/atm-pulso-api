package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

func (h harness) inspect(t *testing.T, path, token, organization string) response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1"+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Organization-Id", organization)
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	out := response{status: rec.Code, header: rec.Header()}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out.body))
	return out
}

func TestCatalogAndInspection(t *testing.T) {
	h := newHarness(t)
	first, second := h.newTenant(t), h.newTenant(t)
	for _, tc := range []struct {
		document   string
		accessible int
	}{{first.Owner, 3}, {first.Admin, 2}, {first.Receptionist, 2}} {
		token := h.token(t, tc.document, demoPassword)
		res := h.inspect(t, "/catalog", token, second.OrganizationID.String())
		require.Equal(t, http.StatusOK, res.status, res.body)
		org, ok := res.data()["organization"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, first.OrganizationID.String(), org["id"], "other roles cannot override their organization")
		gyms, ok := res.data()["gyms"].([]any)
		require.True(t, ok)
		require.Len(t, gyms, 2)
		count := 0
		for _, rawGym := range gyms {
			gym, ok := rawGym.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "15.00", gym["day_pass_price"])
			assert.Equal(t, true, gym["day_pass_price_is_default"])
			branches, ok := gym["branches"].([]any)
			require.True(t, ok)
			for _, rawBranch := range branches {
				branch, ok := rawBranch.(map[string]any)
				require.True(t, ok)
				if branch["accessible"] == true {
					count++
				}
			}
		}
		assert.Equal(t, tc.accessible, count)
	}
	superToken := h.token(t, h.newSuperadmin(t), demoPassword)
	for _, tc := range []struct {
		org    string
		status int
	}{{"", 400}, {"invalid", 400}, {uuid.NewString(), 404}, {first.OrganizationID.String(), 200}} {
		assert.Equal(t, tc.status, h.inspect(t, "/catalog", superToken, tc.org).status)
	}
	module, err := accounts.New(accounts.Deps{Pool: h.pool, Clock: clock.System{}, JWTSecret: jwtSecret})
	require.NoError(t, err)
	branch, sharing, found, err := module.Branches().BranchInfo(context.Background(), first.OrganizationID, first.BranchA)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, sharing)
	assert.Equal(t, first.GymOne, branch.GymID)
	_, _, found, err = module.Branches().BranchInfo(context.Background(), second.OrganizationID, first.BranchA)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestCompositeBranchTenancy(t *testing.T) {
	h := newHarness(t)
	first, second := h.newTenant(t), h.newTenant(t)
	_, err := h.pool.Exec(context.Background(), `INSERT INTO branches(id,gym_id,organization_id,name) VALUES($1,$2,$3,'Invalid')`, uuid.New(), first.GymOne, second.OrganizationID)
	require.Error(t, err)
	_, err = h.pool.Exec(context.Background(), `INSERT INTO user_branches(user_id,branch_id,organization_id) VALUES($1,$2,$3)`, first.ReceptionistID, second.BranchA, first.OrganizationID)
	require.Error(t, err)
}
