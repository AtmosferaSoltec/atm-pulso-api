package integration_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
)

// hired registers a person the fake atm-auth-api created, so a token can be minted for them.
func (h harness) hired(t *testing.T, tn tenant, tenantID uuid.UUID, created response) string {
	t.Helper()
	document := created.data()["document_number"].(string)
	members, err := h.auth.Members(context.Background(), tenantID.String())
	require.NoError(t, err)
	for _, m := range members {
		if m.DNI == document {
			h.people.Store(document, person{AuthUserID: uuid.MustParse(m.UserID), Document: document, Name: m.FullName, Role: m.Role,
				ClientID: tn.ClientID, ClientName: tn.GroupName, TenantID: tenantID})
			return document
		}
	}
	require.Failf(t, "not in atm-auth-api", "DNI %s", document)
	return ""
}

func TestTeamCreationAndScope(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	admin, reception := h.token(t, tn.Admin), h.token(t, tn.Receptionist)

	t.Run("only an admin manages the team", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, h.call(t, http.MethodGet, "/users", reception, nil).status)
		assert.Equal(t, http.StatusForbidden, h.call(t, http.MethodPost, "/users", reception, app.NewUser{FullName: "Alguien Nuevo", DocumentNumber: uniqueDocument(), Role: domain.RoleAdmin}).status)
	})

	t.Run("an admin creates another admin and a receptionist", func(t *testing.T) {
		newAdmin := app.NewUser{FullName: "Nuevo Admin", DocumentNumber: uniqueDocument(), Role: domain.RoleAdmin}
		created := h.call(t, http.MethodPost, "/users", admin, newAdmin)
		require.Equal(t, http.StatusCreated, created.status, created.body)
		assert.Equal(t, "admin", created.data()["role"])
		assert.Empty(t, created.data()["branches"], "an admin has no assigned branches")
		assert.Equal(t, false, created.data()["is_self"])

		newReception := app.NewUser{FullName: "Nueva Recepción", DocumentNumber: uniqueDocument(), Role: domain.RoleReceptionist, BranchIDs: []uuid.UUID{tn.BranchA, tn.BranchA}}
		created = h.call(t, http.MethodPost, "/users", admin, newReception)
		require.Equal(t, http.StatusCreated, created.status, created.body)
		assert.Len(t, created.data()["branches"], 1, "a repeated branch is stored once")
		assert.NotContains(t, created.data(), "password_hash")

		// atm-auth-api got the person with the role; the new receptionist can use Pulso at once.
		document := h.hired(t, tn, tn.TenantOne, created)
		me := h.call(t, http.MethodGet, "/me", h.token(t, document), nil)
		require.Equal(t, http.StatusOK, me.status, me.body)
		assert.Equal(t, "receptionist", me.data()["role"])
		assert.Len(t, me.data()["branches"], 1)
	})

	t.Run("the scope of each role is validated", func(t *testing.T) {
		base := app.NewUser{FullName: "Persona Prueba", DocumentNumber: uniqueDocument()}
		cases := []struct {
			name   string
			input  app.NewUser
			status int
			code   string
		}{
			{"unknown role", app.NewUser{FullName: base.FullName, DocumentNumber: base.DocumentNumber, Role: "owner"}, 403, "ROLE_NOT_ASSIGNABLE"},
			{"admin with branches", app.NewUser{FullName: base.FullName, DocumentNumber: base.DocumentNumber, Role: domain.RoleAdmin, BranchIDs: []uuid.UUID{tn.BranchA}}, 400, "SCOPE_INVALID"},
			{"receptionist without branches", app.NewUser{FullName: base.FullName, DocumentNumber: base.DocumentNumber, Role: domain.RoleReceptionist}, 400, "BRANCH_REQUIRED"},
			{"branch of another gym", app.NewUser{FullName: base.FullName, DocumentNumber: base.DocumentNumber, Role: domain.RoleReceptionist, BranchIDs: []uuid.UUID{tn.BranchC}}, 400, "SCOPE_NOT_ALLOWED"},
			{"branch of another client", app.NewUser{FullName: base.FullName, DocumentNumber: base.DocumentNumber, Role: domain.RoleReceptionist, BranchIDs: []uuid.UUID{other.BranchA}}, 400, "SCOPE_NOT_ALLOWED"},
			{"bad DNI", app.NewUser{FullName: base.FullName, DocumentNumber: "123", Role: domain.RoleAdmin}, 400, "VALIDATION_FAILED"},
			{"bad name", app.NewUser{FullName: "x", DocumentNumber: base.DocumentNumber, Role: domain.RoleAdmin}, 400, "VALIDATION_FAILED"},
		}
		for _, tc := range cases {
			res := h.call(t, http.MethodPost, "/users", admin, tc.input)
			assert.Equal(t, tc.status, res.status, tc.name)
			got := res.errorCode()
			if got == "VALIDATION_FAILED" && tc.code != got {
				got = fieldCode(res)
			}
			assert.Equal(t, tc.code, got, tc.name)
		}
		members, err := h.auth.Members(context.Background(), tn.TenantOne.String())
		require.NoError(t, err)
		for _, m := range members {
			assert.NotEqual(t, base.DocumentNumber, m.DNI, "a rejected request never reaches atm-auth-api")
		}
	})

	t.Run("atm-auth-api refusals become clear errors", func(t *testing.T) {
		h.auth.Err = authapi.ErrUnavailable
		res := h.call(t, http.MethodPost, "/users", admin, app.NewUser{FullName: "Persona Prueba", DocumentNumber: uniqueDocument(), Role: domain.RoleAdmin})
		assert.Equal(t, http.StatusServiceUnavailable, res.status)
		assert.Equal(t, "AUTH_UNAVAILABLE", res.errorCode())
		fallback := h.call(t, http.MethodGet, "/users", admin, nil)
		assert.Equal(t, http.StatusOK, fallback.status, "the list falls back to the people Pulso already knows")
		h.auth.Err = nil
	})
}

func TestTeamListMirrorsAtmAuthApi(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin := h.token(t, tn.Admin)

	names := func() map[string]map[string]any {
		list := h.call(t, http.MethodGet, "/users", admin, nil)
		require.Equal(t, http.StatusOK, list.status, list.body)
		byDocument := map[string]map[string]any{}
		for _, raw := range list.body["data"].([]any) {
			user := raw.(map[string]any)
			byDocument[user["document_number"].(string)] = user
		}
		return byDocument
	}
	listed := names()
	assert.Contains(t, listed, tn.Admin)
	assert.Contains(t, listed, tn.Receptionist)
	assert.NotContains(t, listed, tn.AdminTwo, "the other gym's team is not this team")
	assert.Equal(t, true, listed[tn.Admin]["is_self"])

	// Someone the console added is recorded the first time the list is read.
	added, newcomer := uniqueDocument(), uuid.New()
	h.auth.SetMember(tn.TenantOne.String(), authapi.Member{UserID: newcomer.String(), DNI: added, FullName: "Agregado Consola", Role: "receptionist", Status: "active"})
	listed = names()
	require.Contains(t, listed, added)
	assert.Equal(t, "receptionist", listed[added]["role"])
	assert.Empty(t, listed[added]["branches"], "they see nothing until an admin assigns branches")

	// A person disabled in atm-auth-api shows as inactive.
	h.auth.SetMember(tn.TenantOne.String(), authapi.Member{UserID: uuid.NewString(), DNI: uniqueDocument(), FullName: "Deshabilitado Consola", Role: "admin", Status: "disabled"})
	disabled := 0
	for _, user := range names() {
		if user["is_active"] == false {
			disabled++
		}
	}
	assert.Equal(t, 1, disabled)

	page := h.call(t, http.MethodGet, "/users?page=1&page_size=2", admin, nil)
	assert.Len(t, page.body["data"], 2)
	assert.Equal(t, 400, h.call(t, http.MethodGet, "/users?page_size=1000", admin, nil).status)
}

func TestTeamMutationIsolationAndPasswordReset(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	admin, adminTwo, reception := h.token(t, tn.Admin), h.token(t, tn.AdminTwo), h.token(t, tn.Receptionist)
	path := "/users/" + tn.ReceptionistID.String()

	for _, suffix := range []string{"/activate", "/deactivate", "/password"} {
		assert.Equal(t, 404, h.call(t, http.MethodPost, "/users/"+other.ReceptionistID.String()+suffix, admin, nil).status, "another client")
		assert.Equal(t, 404, h.call(t, http.MethodPost, path+suffix, adminTwo, nil).status, "another gym of the same client")
		assert.Equal(t, "CANNOT_MODIFY_SELF", h.call(t, http.MethodPost, "/users/"+tn.AdminID.String()+suffix, admin, nil).errorCode())
		assert.Equal(t, 403, h.call(t, http.MethodPost, "/users/"+tn.AdminID.String()+suffix, reception, nil).status)
	}
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/users/"+other.ReceptionistID.String(), admin, map[string]string{"full_name": "Otro Nombre"}).status)

	t.Run("reset password goes to atm-auth-api", func(t *testing.T) {
		require.Equal(t, 200, h.call(t, http.MethodPost, path+"/password", admin, nil).status)
		assert.Equal(t, [][2]string{{tn.TenantOne.String(), tn.ReceptionistAuth.String()}}, h.auth.Resets)
	})

	t.Run("deactivating blocks the person at once, activating restores them", func(t *testing.T) {
		require.Equal(t, 200, h.call(t, http.MethodPost, path+"/deactivate", admin, nil).status)
		blocked := h.call(t, http.MethodGet, "/me", reception, nil)
		assert.Equal(t, 403, blocked.status)
		assert.Equal(t, "USER_INACTIVE", blocked.errorCode())
		require.Equal(t, 200, h.call(t, http.MethodPost, path+"/activate", admin, nil).status)
		assert.Equal(t, 200, h.call(t, http.MethodGet, "/me", reception, nil).status)
	})

	t.Run("changing the role is applied in atm-auth-api and clears or requires branches", func(t *testing.T) {
		promoted := h.call(t, http.MethodPatch, path, admin, map[string]any{"role": "admin"})
		require.Equal(t, 200, promoted.status, promoted.body)
		assert.Equal(t, "admin", promoted.data()["role"])
		assert.Empty(t, promoted.data()["branches"])
		members, err := h.auth.Members(context.Background(), tn.TenantOne.String())
		require.NoError(t, err)
		for _, m := range members {
			if m.UserID == tn.ReceptionistAuth.String() {
				assert.Equal(t, "admin", m.Role)
			}
		}
		noBranch := h.call(t, http.MethodPatch, path, admin, map[string]any{"role": "receptionist"})
		assert.Equal(t, "BRANCH_REQUIRED", fieldCode(noBranch))
		back := h.call(t, http.MethodPatch, path, admin, map[string]any{"role": "receptionist", "branch_ids": []uuid.UUID{tn.BranchB}})
		require.Equal(t, 200, back.status, back.body)
		assert.Len(t, back.data()["branches"], 1)
		renamed := h.call(t, http.MethodPatch, path, admin, map[string]any{"full_name": "  Rosa   Nueva "})
		require.Equal(t, 200, renamed.status, renamed.body)
		assert.Equal(t, "Rosa Nueva", renamed.data()["full_name"])
		assert.Len(t, renamed.data()["branches"], 1, "a rename keeps the branches")
		assert.Equal(t, 403, h.call(t, http.MethodPatch, path, admin, map[string]any{"role": "owner"}).status)
	})
}

// fieldCode is the code of the first validation detail of an error response.
func fieldCode(r response) string {
	details, _ := r.body["error"].(map[string]any)["details"].([]any)
	if len(details) == 0 {
		return ""
	}
	code, _ := details[0].(map[string]any)["code"].(string)
	return code
}
