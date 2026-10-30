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

	admin := Principal{Role: RoleAdmin, GymID: atlas}
	reception := Principal{Role: RoleReceptionist, GymID: atlas, BranchIDs: []uuid.UUID{miraflores.ID}}

	t.Run("branch access", func(t *testing.T) {
		assert.True(t, admin.CanAccessBranch(miraflores))
		assert.True(t, admin.CanAccessBranch(barranco))
		assert.False(t, admin.CanAccessBranch(olivos), "an admin stays inside the gym of their token")
		assert.True(t, reception.CanAccessBranch(miraflores))
		assert.False(t, reception.CanAccessBranch(barranco), "reception only has assigned branches")
		assert.False(t, Principal{Role: "unknown", GymID: atlas}.CanAccessBranch(miraflores))
	})

	t.Run("management", func(t *testing.T) {
		assert.True(t, admin.CanManageGym(atlas))
		assert.False(t, admin.CanManageGym(norte))
		assert.False(t, reception.CanManageGym(atlas))
		assert.True(t, admin.CanManageTeam())
		assert.False(t, reception.CanManageTeam())
	})
}
