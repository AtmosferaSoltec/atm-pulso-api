package integration_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/adapters/security"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
)

// The tests build their own data and delete it afterwards: nothing is shared between tests
// and there is no seed in the application. New modules follow the same pattern: add a helper
// here (or in their own *_test.go) that inserts exactly what the test needs.

var documentCounter atomic.Int64

func init() { documentCounter.Store(time.Now().UnixNano() % 5_000_000) }

// uniqueDocument returns a DNI-shaped number (starting with 9) that no other test uses.
func uniqueDocument() string {
	return fmt.Sprintf("9%07d", documentCounter.Add(1)%10_000_000)
}

// tenant is one client with two gyms, three branches and one user per role.
type tenant struct {
	OrganizationID         uuid.UUID
	OrganizationName       string
	GymOne, GymTwo         uuid.UUID
	GymOneName, GymTwoName string
	// BranchA and BranchB belong to GymOne; BranchC to GymTwo.
	BranchA, BranchB, BranchC uuid.UUID
	// Documents of the users, all with the password demoPassword.
	Owner, Admin, Receptionist       string
	OwnerID, AdminID, ReceptionistID uuid.UUID
}

func (h harness) hash(t *testing.T) string {
	t.Helper()
	hash, err := security.Argon2idHasher{}.Hash(demoPassword)
	require.NoError(t, err)
	return hash
}

// newTenant inserts a client and removes it (users, gyms and branches cascade) when the test ends.
// The owner and the admin (of GymOne) have every branch of their reach; the receptionist works in
// BranchA and BranchB.
func (h harness) newTenant(t *testing.T) tenant {
	t.Helper()
	ctx := context.Background()
	hash := h.hash(t)
	tn := tenant{
		OrganizationID:   ids.New(),
		OrganizationName: "Cliente " + uniqueDocument(),
		GymOne:           ids.New(), GymTwo: ids.New(),
		GymOneName: "Gym Uno", GymTwoName: "Gym Dos",
		BranchA: ids.New(), BranchB: ids.New(), BranchC: ids.New(),
		Owner: uniqueDocument(), Admin: uniqueDocument(), Receptionist: uniqueDocument(),
		OwnerID: ids.New(), AdminID: ids.New(), ReceptionistID: ids.New(),
	}

	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := h.pool.Exec(ctx, sql, args...)
		require.NoError(t, err)
	}
	exec(`INSERT INTO organizations (id, name) VALUES ($1, $2)`, tn.OrganizationID, tn.OrganizationName)
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, tn.OrganizationID)
	})

	exec(`INSERT INTO gyms (id, organization_id, name) VALUES ($1, $3, $4), ($2, $3, $5)`, tn.GymOne, tn.GymTwo, tn.OrganizationID, tn.GymOneName, tn.GymTwoName)
	exec(`INSERT INTO branches (id, gym_id, name, organization_id) VALUES ($1, $4, 'Sede A', $6), ($2, $4, 'Sede B', $6), ($3, $5, 'Sede C', $6)`, tn.BranchA, tn.BranchB, tn.BranchC, tn.GymOne, tn.GymTwo, tn.OrganizationID)
	exec(`INSERT INTO users (id, organization_id, gym_id, document_number, full_name, role, password_hash) VALUES
		($1, $3, NULL, $4, 'Dueño de Prueba', 'owner', $7),
		($2, $3, $6, $5, 'Admin de Prueba', 'admin', $7)`, tn.OwnerID, tn.AdminID, tn.OrganizationID, tn.Owner, tn.Admin, tn.GymOne, hash)
	exec(`INSERT INTO users (id, organization_id, document_number, full_name, role, password_hash)
		VALUES ($1, $2, $3, 'Recepción de Prueba', 'receptionist', $4)`, tn.ReceptionistID, tn.OrganizationID, tn.Receptionist, hash)
	exec(`INSERT INTO user_branches (user_id, branch_id, organization_id) VALUES ($1, $2, $4), ($1, $3, $4)`, tn.ReceptionistID, tn.BranchA, tn.BranchB, tn.OrganizationID)
	return tn
}

// newSuperadmin inserts a superadmin (it belongs to no organization) and removes it afterwards.
func (h harness) newSuperadmin(t *testing.T) string {
	t.Helper()
	id, document := ids.New(), uniqueDocument()
	_, err := h.pool.Exec(context.Background(), `INSERT INTO users (id, document_number, full_name, role, password_hash)
		VALUES ($1, $2, 'Superadmin de Prueba', 'superadmin', $3)`, id, document, h.hash(t))
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = h.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id) })
	return document
}

// freshUser inserts an active owner of its own, to change it without disturbing others.
func (h harness) freshUser(t *testing.T) (document string, id uuid.UUID) {
	t.Helper()
	tn := h.newTenant(t)
	return tn.Owner, tn.OwnerID
}
