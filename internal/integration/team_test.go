package integration_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/domain"
)

func TestTeamCreationAndScope(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	owner, admin := h.token(t, tn.Owner, demoPassword), h.token(t, tn.Admin, demoPassword)
	input := app.NewUser{ID: uuid.New(), FullName: "New Admin", DocumentNumber: uniqueDocument(), Role: domain.RoleAdmin, GymIDs: []uuid.UUID{tn.GymOne}}
	assert.Equal(t, "ROLE_NOT_ASSIGNABLE", h.call(t, http.MethodPost, "/users", admin, input).errorCode())
	created := h.call(t, http.MethodPost, "/users", owner, input)
	require.Equal(t, http.StatusCreated, created.status, created.body)
	assert.Equal(t, created.data(), h.call(t, http.MethodPost, "/users", owner, input).data())
	token := h.token(t, input.DocumentNumber, input.DocumentNumber)
	assert.Equal(t, "PASSWORD_CHANGE_REQUIRED", h.call(t, http.MethodGet, "/catalog", token, nil).errorCode())
	receptionist := app.NewUser{ID: uuid.New(), FullName: "New Reception", DocumentNumber: uniqueDocument(), Role: domain.RoleReceptionist, BranchIDs: []uuid.UUID{other.BranchA}}
	result := h.call(t, http.MethodPost, "/users", owner, receptionist)
	assert.Equal(t, 400, result.status)
	assert.Equal(t, "SCOPE_NOT_ALLOWED", result.errorCode())
	receptionist.BranchIDs = []uuid.UUID{tn.BranchC}
	assert.Equal(t, 403, h.call(t, http.MethodPost, "/users", admin, receptionist).status)
	receptionist.BranchIDs = []uuid.UUID{tn.BranchA}
	result = h.call(t, http.MethodPost, "/users", admin, receptionist)
	require.Equal(t, 201, result.status, result.body)
	receptionist.ID = uuid.New()
	assert.Equal(t, "USER_DOCUMENT_TAKEN", h.call(t, http.MethodPost, "/users", owner, receptionist).errorCode())
	for _, document := range []string{tn.Owner, tn.Admin} {
		token := h.token(t, document, demoPassword)
		list := h.call(t, http.MethodGet, "/users", token, nil)
		require.Equal(t, 200, list.status)
		users, ok := list.body["data"].([]any)
		require.True(t, ok)
		for _, raw := range users {
			user, ok := raw.(map[string]any)
			require.True(t, ok)
			assert.NotContains(t, user, "password_hash")
			if document == tn.Admin {
				assert.NotEqual(t, "owner", user["role"])
				if user["role"] == "admin" {
					assert.Equal(t, tn.AdminID.String(), user["id"])
				}
			}
		}
	}
}

// One owner, several gyms: the owner decides which gyms each admin manages, an admin sees
// and reaches only those, and an admin can never create another admin.
func TestAdminManagesSeveralGyms(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	owner := h.token(t, tn.Owner, demoPassword)

	both := app.NewUser{ID: uuid.New(), FullName: "Admin Dos Gyms", DocumentNumber: uniqueDocument(), Role: domain.RoleAdmin, GymIDs: []uuid.UUID{tn.GymOne, tn.GymTwo, tn.GymTwo}}
	created := h.call(t, http.MethodPost, "/users", owner, both)
	require.Equal(t, http.StatusCreated, created.status, created.body)
	assert.Len(t, created.data()["gyms"], 2, "a repeated gym is stored once")

	invalid := both
	invalid.ID, invalid.DocumentNumber, invalid.GymIDs = uuid.New(), uniqueDocument(), nil
	assert.Equal(t, "GYM_REQUIRED", fieldCode(h.call(t, http.MethodPost, "/users", owner, invalid)), "an admin needs at least one gym")
	invalid.GymIDs = []uuid.UUID{tn.GymOne, other.GymOne}
	assert.Equal(t, "SCOPE_NOT_ALLOWED", h.call(t, http.MethodPost, "/users", owner, invalid).errorCode(), "a gym of another organization is refused")
	reception := app.NewUser{ID: uuid.New(), FullName: "Recepcion", DocumentNumber: uniqueDocument(), Role: domain.RoleReceptionist, BranchIDs: []uuid.UUID{tn.BranchA}, GymIDs: []uuid.UUID{tn.GymOne}}
	assert.Equal(t, "SCOPE_INVALID", fieldCode(h.call(t, http.MethodPost, "/users", owner, reception)), "only admins carry gyms")

	token := h.token(t, both.DocumentNumber, both.DocumentNumber)
	require.Equal(t, 200, h.call(t, http.MethodPost, "/me/password", token, map[string]string{"current_password": both.DocumentNumber, "new_password": "ChangedPassword123!"}).status)
	admin := h.token(t, both.DocumentNumber, "ChangedPassword123!")

	me := h.call(t, http.MethodGet, "/me", admin, nil)
	require.Equal(t, 200, me.status, me.body)
	assert.Len(t, me.data()["gyms"], 2)

	catalog := h.call(t, http.MethodGet, "/catalog", admin, nil)
	require.Equal(t, 200, catalog.status, catalog.body)
	for _, raw := range catalog.data()["gyms"].([]any) {
		for _, b := range raw.(map[string]any)["branches"].([]any) {
			assert.Equal(t, true, b.(map[string]any)["accessible"], "every branch of both gyms is reachable")
		}
	}

	// An admin never creates another admin, but creates reception for any of its gyms.
	assert.Equal(t, "ROLE_NOT_ASSIGNABLE", h.call(t, http.MethodPost, "/users", admin, app.NewUser{ID: uuid.New(), FullName: "Otro Admin", DocumentNumber: uniqueDocument(), Role: domain.RoleAdmin, GymIDs: []uuid.UUID{tn.GymOne}}).errorCode())
	for _, branch := range []uuid.UUID{tn.BranchA, tn.BranchC} {
		result := h.call(t, http.MethodPost, "/users", admin, app.NewUser{ID: uuid.New(), FullName: "Recepcion", DocumentNumber: uniqueDocument(), Role: domain.RoleReceptionist, BranchIDs: []uuid.UUID{branch}})
		assert.Equal(t, 201, result.status, result.body)
	}

	// The owner narrows the admin to one gym: the change takes effect on the next request.
	narrowed := h.call(t, http.MethodPatch, "/users/"+both.ID.String(), owner, map[string]any{"gym_ids": []uuid.UUID{tn.GymTwo}})
	require.Equal(t, 200, narrowed.status, narrowed.body)
	assert.Len(t, narrowed.data()["gyms"], 1)
	assert.Equal(t, 403, h.call(t, http.MethodPost, "/users", admin, app.NewUser{ID: uuid.New(), FullName: "Recepcion", DocumentNumber: uniqueDocument(), Role: domain.RoleReceptionist, BranchIDs: []uuid.UUID{tn.BranchA}}).status)
	assert.Equal(t, 200, h.call(t, http.MethodPatch, "/gyms/"+tn.GymTwo.String(), admin, map[string]any{"branch_sharing": true}).status)
	assert.Equal(t, 403, h.call(t, http.MethodPatch, "/gyms/"+tn.GymOne.String(), admin, map[string]any{"branch_sharing": true}).status)
}
func TestTeamMutationIsolationAndPasswordReset(t *testing.T) {
	h := newHarness(t)
	tn, other := h.newTenant(t), h.newTenant(t)
	owner := h.token(t, tn.Owner, demoPassword)
	admin := h.token(t, tn.Admin, demoPassword)
	reception := h.token(t, tn.Receptionist, demoPassword)
	super := h.token(t, h.newSuperadmin(t), demoPassword)
	assert.Equal(t, 200, h.inspect(t, "/users", super, tn.OrganizationID.String()).status)
	assert.Equal(t, 403, h.call(t, http.MethodGet, "/users", reception, nil).status)
	for _, suffix := range []string{"/activate", "/deactivate", "/password"} {
		assert.Equal(t, 404, h.call(t, http.MethodPost, "/users/"+other.ReceptionistID.String()+suffix, owner, nil).status)
		assert.Equal(t, "CANNOT_MODIFY_SELF", h.call(t, http.MethodPost, "/users/"+tn.AdminID.String()+suffix, admin, nil).errorCode())
		assert.Equal(t, "CANNOT_MODIFY_SELF", h.call(t, http.MethodPost, "/users/"+tn.OwnerID.String()+suffix, owner, nil).errorCode())
		assert.Equal(t, "CANNOT_MODIFY_OWNER", h.call(t, http.MethodPost, "/users/"+tn.OwnerID.String()+suffix, admin, nil).errorCode())
		assert.Equal(t, 403, h.call(t, http.MethodPost, "/users/"+tn.ReceptionistID.String()+suffix, super, nil).status)
		assert.Equal(t, 403, h.call(t, http.MethodPost, "/users/"+tn.AdminID.String()+suffix, reception, nil).status)
	}
	assert.Equal(t, 404, h.call(t, http.MethodPatch, "/users/"+other.ReceptionistID.String(), owner, map[string]string{"full_name": "Other name"}).status)
	path := "/users/" + tn.ReceptionistID.String()
	require.Equal(t, 200, h.call(t, http.MethodPost, path+"/password", admin, nil).status)
	assert.Equal(t, 401, h.call(t, http.MethodGet, "/me", reception, nil).status)
	temporary := h.token(t, tn.Receptionist, tn.Receptionist)
	newDNI := uniqueDocument()
	corrected := h.call(t, http.MethodPatch, path, admin, map[string]string{"document_number": newDNI})
	require.Equal(t, 200, corrected.status, corrected.body)
	assert.Equal(t, 401, h.call(t, http.MethodGet, "/me", temporary, nil).status)
	assert.Equal(t, 401, h.login(t, tn.Receptionist, tn.Receptionist).status)
	correctedToken := h.token(t, newDNI, newDNI)
	changed := h.call(t, http.MethodPost, "/me/password", correctedToken, map[string]string{"current_password": newDNI, "new_password": "ChangedPassword123!"})
	require.Equal(t, 200, changed.status)
	assert.Equal(t, map[string]any{"login_required": true}, changed.data())
	active := h.token(t, newDNI, "ChangedPassword123!")
	secondDNI := uniqueDocument()
	require.Equal(t, 200, h.call(t, http.MethodPatch, path, admin, map[string]string{"document_number": secondDNI}).status)
	assert.Equal(t, 401, h.call(t, http.MethodGet, "/me", active, nil).status)
	chosen := h.token(t, secondDNI, "ChangedPassword123!")
	require.Equal(t, 200, h.call(t, http.MethodPost, path+"/deactivate", admin, nil).status)
	assert.Equal(t, 401, h.call(t, http.MethodGet, "/me", chosen, nil).status)
	require.Equal(t, 200, h.call(t, http.MethodPost, path+"/activate", admin, nil).status)
	assert.Equal(t, 401, h.call(t, http.MethodGet, "/me", chosen, nil).status)
	require.Equal(t, 200, h.login(t, secondDNI, "ChangedPassword123!").status)
}
func TestPlatformOwnerCorrectionAndReset(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	super := h.token(t, h.newSuperadmin(t), demoPassword)
	owner := h.token(t, tn.Owner, demoPassword)
	path := "/clients/" + tn.OrganizationID.String() + "/owner"
	assert.Equal(t, 403, h.call(t, http.MethodPatch, path, owner, map[string]string{"document_number": uniqueDocument()}).status)
	assert.Equal(t, 403, h.call(t, http.MethodPost, path+"/password", owner, nil).status)
	document := uniqueDocument()
	result := h.call(t, http.MethodPatch, path, super, map[string]string{"document_number": document})
	require.Equal(t, 200, result.status, result.body)
	assert.Equal(t, 401, h.call(t, http.MethodGet, "/me", owner, nil).status)
	token := h.token(t, document, demoPassword)
	require.Equal(t, 200, h.call(t, http.MethodPost, path+"/password", super, nil).status)
	assert.Equal(t, 401, h.call(t, http.MethodGet, "/me", token, nil).status)
	temporary := h.token(t, document, document)
	assert.Equal(t, "PASSWORD_CHANGE_REQUIRED", h.call(t, http.MethodGet, "/catalog", temporary, nil).errorCode())
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
