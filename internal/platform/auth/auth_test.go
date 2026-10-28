package auth

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestPrincipalScope(t *testing.T) {
	atlas, norte := uuid.New(), uuid.New()
	miraflores := BranchRef{ID: uuid.New(), GymID: atlas}
	barranco := BranchRef{ID: uuid.New(), GymID: atlas}
	olivos := BranchRef{ID: uuid.New(), GymID: norte}
	sur := uuid.New()
	chorrillos := BranchRef{ID: uuid.New(), GymID: sur}

	owner := Principal{Role: RoleOwner}
	admin := Principal{Role: RoleAdmin, GymIDs: []uuid.UUID{atlas}}
	multiAdmin := Principal{Role: RoleAdmin, GymIDs: []uuid.UUID{atlas, norte}}
	reception := Principal{Role: RoleReceptionist, BranchIDs: []uuid.UUID{miraflores.ID}}
	superadmin := Principal{Role: RoleSuperadmin}

	t.Run("branch access", func(t *testing.T) {
		for _, branch := range []BranchRef{miraflores, barranco, olivos} {
			assert.True(t, owner.CanAccessBranch(branch), "owner reaches every branch")
			assert.True(t, superadmin.CanAccessBranch(branch), "superadmin can consult every branch")
		}
		assert.True(t, admin.CanAccessBranch(miraflores))
		assert.True(t, admin.CanAccessBranch(barranco))
		assert.False(t, admin.CanAccessBranch(olivos), "admin stays inside their gym")
		assert.True(t, multiAdmin.CanAccessBranch(miraflores))
		assert.True(t, multiAdmin.CanAccessBranch(olivos), "an admin with several gyms reaches all of them")
		assert.False(t, multiAdmin.CanAccessBranch(chorrillos), "but not the gyms the owner did not assign")
		assert.False(t, Principal{Role: RoleAdmin}.CanAccessBranch(miraflores), "an admin without gyms reaches nothing")
		assert.True(t, reception.CanAccessBranch(miraflores))
		assert.False(t, reception.CanAccessBranch(barranco), "reception only has assigned branches")
		assert.False(t, Principal{Role: "unknown"}.CanAccessBranch(miraflores))
	})

	t.Run("gym management", func(t *testing.T) {
		assert.True(t, owner.CanManageGym(atlas))
		assert.True(t, owner.CanManageGym(norte))
		assert.True(t, admin.CanManageGym(atlas))
		assert.False(t, admin.CanManageGym(norte))
		assert.True(t, multiAdmin.CanManageGym(atlas) && multiAdmin.CanManageGym(norte))
		assert.False(t, multiAdmin.CanManageGym(sur))
		assert.False(t, reception.CanManageGym(atlas))
		assert.False(t, superadmin.CanManageGym(atlas), "superadmin only consults")
	})

	t.Run("team and operation", func(t *testing.T) {
		assert.True(t, owner.CanManageTeam())
		assert.True(t, admin.CanManageTeam())
		assert.False(t, reception.CanManageTeam())
		assert.False(t, superadmin.CanManageTeam())
		assert.False(t, superadmin.CanOperate())
		assert.True(t, owner.CanOperate() && admin.CanOperate() && reception.CanOperate())
	})
}

func TestBearerToken(t *testing.T) {
	cases := map[string]struct {
		header string
		token  string
		ok     bool
	}{
		"valid":            {"Bearer abc.def", "abc.def", true},
		"case-insensitive": {"bearer abc", "abc", true},
		"extra spaces":     {"Bearer   abc  ", "abc", true},
		"missing":          {"", "", false},
		"wrong scheme":     {"Basic abc", "", false},
		"no token":         {"Bearer ", "", false},
		"only scheme":      {"Bearer", "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			token, ok := bearerToken(tc.header)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.token, token)
		})
	}
}
