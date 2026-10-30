package integration_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

func TestCatalogIsSharedButAccessFollowsTheToken(t *testing.T) {
	h := newHarness(t)
	first, second := h.newTenant(t), h.newTenant(t)
	for _, tc := range []struct {
		name       string
		document   string
		accessible int
	}{{"admin of gym one", first.Admin, 2}, {"admin of gym two", first.AdminTwo, 1}, {"receptionist", first.Receptionist, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			res := h.call(t, http.MethodGet, "/catalog", h.token(t, tc.document), nil)
			require.Equal(t, http.StatusOK, res.status, res.body)
			org := res.data()["organization"].(map[string]any)
			assert.Equal(t, first.OrganizationID.String(), org["id"])
			gyms := res.data()["gyms"].([]any)
			require.Len(t, gyms, 2, "the directory shows every gym of the client")
			count := 0
			for _, rawGym := range gyms {
				gym := rawGym.(map[string]any)
				assert.Equal(t, "15.00", gym["day_pass_price"])
				assert.Equal(t, true, gym["day_pass_price_is_default"])
				assert.Nil(t, gym["logo_url"])
				for _, rawBranch := range gym["branches"].([]any) {
					if rawBranch.(map[string]any)["accessible"] == true {
						count++
					}
				}
			}
			assert.Equal(t, tc.accessible, count)
		})
	}

	module := accounts.New(accounts.Deps{Pool: h.pool, Clock: clock.System{}})
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
