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

func TestCatalogIsTheCurrentGymAndAccessFollowsTheToken(t *testing.T) {
	h := newHarness(t)
	first, second := h.newTenant(t), h.newTenant(t)
	for _, tc := range []struct {
		name       string
		document   string
		gym        uuid.UUID
		accessible int
	}{{"admin of gym one", first.Admin, first.GymOne, 2}, {"admin of gym two", first.AdminTwo, first.GymTwo, 1}, {"receptionist", first.Receptionist, first.GymOne, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			res := h.call(t, http.MethodGet, "/catalog", h.token(t, tc.document), nil)
			require.Equal(t, http.StatusOK, res.status, res.body)
			assert.NotContains(t, res.data(), "organization")
			gyms := res.data()["gyms"].([]any)
			require.Len(t, gyms, 1, "only the current gym: a sibling gym of the same group is never listed")
			assert.Equal(t, tc.gym.String(), gyms[0].(map[string]any)["id"])
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
	branch, sharing, found, err := module.Branches().BranchInfo(context.Background(), first.GymOne, first.BranchA)
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, sharing)
	assert.Equal(t, first.GymOne, branch.GymID)
	_, _, found, err = module.Branches().BranchInfo(context.Background(), second.GymOne, first.BranchA)
	require.NoError(t, err)
	assert.False(t, found)
	_, _, found, err = module.Branches().BranchInfo(context.Background(), first.GymTwo, first.BranchA)
	require.NoError(t, err)
	assert.False(t, found, "a sibling gym of the same group does not see it either")
}

func TestCompositeBranchTenancy(t *testing.T) {
	h := newHarness(t)
	first, second := h.newTenant(t), h.newTenant(t)
	_, err := h.pool.Exec(context.Background(), `INSERT INTO members(id,gym_id,home_branch_id,document_type,document_number,full_name,phone) VALUES($1,$2,$3,'dni',$4,'Invalid','911111111')`, uuid.New(), first.GymOne, second.BranchA, uniqueDocument())
	require.Error(t, err, "a member cannot be homed in a branch of another gym")
	_, err = h.pool.Exec(context.Background(), `INSERT INTO members(id,gym_id,home_branch_id,document_type,document_number,full_name,phone) VALUES($1,$2,$3,'dni',$4,'Invalid','911111111')`, uuid.New(), first.GymOne, first.BranchC, uniqueDocument())
	require.Error(t, err, "nor in a branch of a sibling gym of the same group")
	_, err = h.pool.Exec(context.Background(), `INSERT INTO user_branches(user_id,branch_id,gym_id) VALUES($1,$2,$3)`, first.ReceptionistID, second.BranchA, first.GymOne)
	require.Error(t, err)
}
