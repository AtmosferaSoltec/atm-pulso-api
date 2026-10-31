package integration_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
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

// person is someone atm-auth-api would let into a gym: tokens are minted from it.
type person struct {
	AuthUserID uuid.UUID
	Document   string
	Name       string
	Role       string
	ClientID   uuid.UUID
	ClientName string
	TenantID   uuid.UUID
}

// tenant is one group with two gyms (auth companies of one client), three branches and a few people.
type tenant struct {
	GroupID                uuid.UUID
	ClientID               uuid.UUID
	GroupName              string
	GymOne, GymTwo         uuid.UUID
	TenantOne, TenantTwo   uuid.UUID
	GymOneName, GymTwoName string
	// BranchA and BranchB belong to GymOne; BranchC to GymTwo.
	BranchA, BranchB, BranchC uuid.UUID
	// Documents of the people: Admin runs GymOne, AdminTwo runs GymTwo and Receptionist works
	// in BranchA and BranchB of GymOne.
	Admin, AdminTwo, Receptionist             string
	AdminID, AdminTwoID, ReceptionistID       uuid.UUID
	AdminAuth, AdminTwoAuth, ReceptionistAuth uuid.UUID
}

// newTenant inserts a client and removes it (users, gyms and branches cascade) when the test ends.
func (h harness) newTenant(t *testing.T) tenant {
	t.Helper()
	ctx := context.Background()
	tn := tenant{
		GroupID:   ids.New(),
		ClientID:  uuid.New(),
		GroupName: "Grupo " + uniqueDocument(),
		GymOne:    ids.New(), GymTwo: ids.New(),
		TenantOne: uuid.New(), TenantTwo: uuid.New(),
		GymOneName: "Gym Uno", GymTwoName: "Gym Dos",
		BranchA: ids.New(), BranchB: ids.New(), BranchC: ids.New(),
		Admin: uniqueDocument(), AdminTwo: uniqueDocument(), Receptionist: uniqueDocument(),
		AdminID: ids.New(), AdminTwoID: ids.New(), ReceptionistID: ids.New(),
		AdminAuth: uuid.New(), AdminTwoAuth: uuid.New(), ReceptionistAuth: uuid.New(),
	}

	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := h.pool.Exec(ctx, sql, args...)
		require.NoError(t, err)
	}
	exec(`INSERT INTO groups (id, name) VALUES ($1, $2)`, tn.GroupID, tn.GroupName)
	t.Cleanup(func() {
		// Gyms first (a group with gyms cannot be removed); everything else cascades from the gym.
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM gyms WHERE group_id = $1`, tn.GroupID)
		_, _ = h.pool.Exec(context.Background(), `DELETE FROM groups WHERE id = $1`, tn.GroupID)
	})

	exec(`INSERT INTO gyms (id, group_id, name, auth_tenant_id, auth_client_id) VALUES ($1, $3, $4, $6, $8), ($2, $3, $5, $7, $8)`,
		tn.GymOne, tn.GymTwo, tn.GroupID, tn.GymOneName, tn.GymTwoName, tn.TenantOne, tn.TenantTwo, tn.ClientID)
	exec(`INSERT INTO branches (id, gym_id, name) VALUES ($1, $4, 'Sede A'), ($2, $4, 'Sede B'), ($3, $5, 'Sede C')`,
		tn.BranchA, tn.BranchB, tn.BranchC, tn.GymOne, tn.GymTwo)
	user := func(id, authID uuid.UUID, gym uuid.UUID, document, name, role string) {
		exec(`INSERT INTO users (id, gym_id, auth_user_id, document_number, full_name, role) VALUES ($1, $2, $3, $4, $5, $6)`,
			id, gym, authID, document, name, role)
	}
	user(tn.AdminID, tn.AdminAuth, tn.GymOne, tn.Admin, "Admin de Prueba", "admin")
	user(tn.AdminTwoID, tn.AdminTwoAuth, tn.GymTwo, tn.AdminTwo, "Admin Dos de Prueba", "admin")
	user(tn.ReceptionistID, tn.ReceptionistAuth, tn.GymOne, tn.Receptionist, "Recepción de Prueba", "receptionist")
	exec(`INSERT INTO user_branches (user_id, branch_id, gym_id) VALUES ($1, $2, $4), ($1, $3, $4)`, tn.ReceptionistID, tn.BranchA, tn.BranchB, tn.GymOne)

	for _, m := range []struct {
		tenant         uuid.UUID
		auth           uuid.UUID
		document, name string
		role           string
	}{
		{tn.TenantOne, tn.AdminAuth, tn.Admin, "Admin de Prueba", "admin"},
		{tn.TenantTwo, tn.AdminTwoAuth, tn.AdminTwo, "Admin Dos de Prueba", "admin"},
		{tn.TenantOne, tn.ReceptionistAuth, tn.Receptionist, "Recepción de Prueba", "receptionist"},
	} {
		h.auth.SetMember(m.tenant.String(), authapi.Member{UserID: m.auth.String(), DNI: m.document, FullName: m.name, Role: m.role, Status: "active"})
	}
	h.people.Store(tn.Admin, person{AuthUserID: tn.AdminAuth, Document: tn.Admin, Name: "Admin de Prueba", Role: "admin", ClientID: tn.ClientID, ClientName: tn.GroupName, TenantID: tn.TenantOne})
	h.people.Store(tn.AdminTwo, person{AuthUserID: tn.AdminTwoAuth, Document: tn.AdminTwo, Name: "Admin Dos de Prueba", Role: "admin", ClientID: tn.ClientID, ClientName: tn.GroupName, TenantID: tn.TenantTwo})
	h.people.Store(tn.Receptionist, person{AuthUserID: tn.ReceptionistAuth, Document: tn.Receptionist, Name: "Recepción de Prueba", Role: "receptionist", ClientID: tn.ClientID, ClientName: tn.GroupName, TenantID: tn.TenantOne})
	return tn
}

// extraBranch adds a third branch to GymOne that the receptionist is not assigned to.
func (h harness) extraBranch(t *testing.T, tn tenant) uuid.UUID {
	t.Helper()
	id := ids.New()
	_, err := h.pool.Exec(context.Background(), `INSERT INTO branches (id, gym_id, name) VALUES ($1, $2, 'Sede D')`, id, tn.GymOne)
	require.NoError(t, err)
	return id
}

// cleanupClient removes, when the test ends, the gyms the API recorded for an auth client and the
// groups it created for them.
func (h harness) cleanupClient(t *testing.T, client uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		var groups []uuid.UUID
		rows, err := h.pool.Query(ctx, `SELECT DISTINCT group_id FROM gyms WHERE auth_client_id = $1`, client)
		if err == nil {
			for rows.Next() {
				var id uuid.UUID
				if rows.Scan(&id) == nil {
					groups = append(groups, id)
				}
			}
			rows.Close()
		}
		_, _ = h.pool.Exec(ctx, `DELETE FROM gyms WHERE auth_client_id = $1`, client)
		for _, id := range groups {
			_, _ = h.pool.Exec(ctx, `DELETE FROM groups WHERE id = $1`, id)
		}
	})
}

// freshAdmin inserts an active admin of its own, to change it without disturbing others.
func (h harness) freshAdmin(t *testing.T) (document string, id uuid.UUID) {
	t.Helper()
	tn := h.newTenant(t)
	return tn.Admin, tn.AdminID
}
