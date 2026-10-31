package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
)

func TestMeForEveryRole(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)

	cases := []struct {
		name     string
		document string
		role     string
		gym      string
		branches []string
	}{
		{"admin of the first gym", tn.Admin, "admin", tn.GymOneName, []string{"Sede A", "Sede B"}},
		{"admin of the second gym", tn.AdminTwo, "admin", tn.GymTwoName, []string{"Sede C"}},
		{"receptionist", tn.Receptionist, "receptionist", tn.GymOneName, []string{"Sede A", "Sede B"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			me := h.call(t, http.MethodGet, "/me", h.token(t, tc.document), nil)
			require.Equal(t, http.StatusOK, me.status, "%v", me.body)
			user := me.data()
			assert.Equal(t, tc.document, user["document_number"])
			assert.Equal(t, tc.role, user["role"])
			assert.NotContains(t, user, "must_change_password")
			assert.NotContains(t, user, "organization", "the group is internal and never shown")
			assert.NotContains(t, user, "group")
			gym := user["gym"].(map[string]any)
			assert.Equal(t, tc.gym, gym["name"])
			assert.Contains(t, gym, "brand_color")
			assert.Nil(t, gym["logo_url"], "the logo will come from the company profile")
			var names []string
			for _, b := range user["branches"].([]any) {
				names = append(names, b.(map[string]any)["name"].(string))
			}
			assert.Equal(t, tc.branches, names)
		})
	}
}

func TestPrivateRoutesNeedAValidUserToken(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	value, _ := h.people.Load(tn.Admin)
	claims := h.userClaims(value.(person))
	with := func(changes map[string]any) string {
		merged := map[string]any{}
		for k, v := range claims {
			merged[k] = v
		}
		for k, v := range changes {
			merged[k] = v
		}
		return h.tokens.Mint(t, merged)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	type expectation struct {
		status int
		code   string
	}
	unauthorized := expectation{http.StatusUnauthorized, "UNAUTHORIZED"}
	noAccess := expectation{http.StatusForbidden, "NO_ACCESS"}
	for name, c := range map[string]struct {
		token string
		want  expectation
	}{
		"no token":             {"", unauthorized},
		"garbage":              {"not-a-token", unauthorized},
		"another key":          {h.tokens.MintWith(t, otherKey, "test-key", claims), unauthorized},
		"unknown key id":       {h.tokens.MintWith(t, otherKey, "rotated", claims), unauthorized},
		"expired":              {with(map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}), unauthorized},
		"another issuer":       {with(map[string]any{"iss": "https://evil.test"}), unauthorized},
		"another system":       {with(map[string]any{"system": "olympus"}), noAccess},
		"no company":           {with(map[string]any{"tenant": ""}), noAccess},
		"no client":            {with(map[string]any{"client": ""}), noAccess},
		"unknown role":         {with(map[string]any{"app_role": "cashier"}), noAccess},
		"owner is gone":        {with(map[string]any{"app_role": "owner"}), noAccess},
		"member is not active": {with(map[string]any{"app_role": "member"}), noAccess},
		"a service token":      {with(map[string]any{"role": "service", "sub": "system:pulso"}), noAccess},
		"superadmin on users":  {h.superToken(t), expectation{http.StatusForbidden, "FORBIDDEN"}},
	} {
		t.Run(name, func(t *testing.T) {
			for _, path := range []string{"/me", "/documents/persons/45678912"} {
				res := h.call(t, http.MethodGet, path, c.token, nil)
				assert.Equal(t, c.want.status, res.status, path)
				assert.Equal(t, c.want.code, res.errorCode(), path)
			}
		})
	}

	t.Run("unknown routes stay 404", func(t *testing.T) {
		res := h.call(t, http.MethodGet, "/nope", "", nil)
		assert.Equal(t, http.StatusNotFound, res.status)
		assert.Equal(t, "NOT_FOUND", res.errorCode())
	})
}

func TestFirstSightRecordsClientCompanyBranchAndPerson(t *testing.T) {
	h := newHarness(t)
	client, tenantID, authUser := uuid.New(), uuid.New(), uuid.New()
	document := uniqueDocument()
	h.cleanupClient(t, client)
	h.auth.SetTenant(authapi.Tenant{ID: tenantID.String(), RUC: "20555555555", DisplayName: "Titanes Gym", ClientID: client.String(), ClientName: "Familia Titanes"})
	token := h.tokens.Mint(t, map[string]any{
		"sub": authUser.String(), "role": "user", "system": systemCode, "tenant": tenantID.String(),
		"client": client.String(), "client_name": "Familia Titanes", "app_role": "admin",
		"name": "Rosa Titán", "dni": document, "tenant_ruc": "20555555555",
	})

	me := h.call(t, http.MethodGet, "/me", token, nil)
	require.Equal(t, http.StatusOK, me.status, "%v", me.body)
	assert.NotContains(t, me.data(), "organization")
	assert.Equal(t, "Titanes Gym", me.data()["gym"].(map[string]any)["name"], "the name of the company comes from atm-auth-api")
	branches := me.data()["branches"].([]any)
	require.Len(t, branches, 1)
	assert.Equal(t, "Sede principal", branches[0].(map[string]any)["name"])
	assert.Equal(t, "Rosa Titán", me.data()["full_name"])

	again := h.call(t, http.MethodGet, "/me", token, nil)
	assert.Equal(t, me.data()["id"], again.data()["id"], "the same person, not a new record")
	var groups, gyms, branchRows, users int
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT
		(SELECT count(DISTINCT group_id) FROM gyms WHERE auth_client_id = $1),
		(SELECT count(*) FROM gyms WHERE auth_tenant_id = $2),
		(SELECT count(*) FROM branches b JOIN gyms g ON g.id = b.gym_id WHERE g.auth_tenant_id = $2),
		(SELECT count(*) FROM users WHERE auth_user_id = $3)`, client, tenantID, authUser).Scan(&groups, &gyms, &branchRows, &users))
	assert.Equal(t, []int{1, 1, 1, 1}, []int{groups, gyms, branchRows, users})

	t.Run("a second company of the same client joins the same group", func(t *testing.T) {
		second := uuid.New()
		other := h.tokens.Mint(t, map[string]any{
			"sub": authUser.String(), "role": "user", "system": systemCode, "tenant": second.String(),
			"client": client.String(), "client_name": "Familia Titanes", "app_role": "admin",
			"name": "Rosa Titán", "dni": document, "tenant_ruc": "20666666666",
		})
		res := h.call(t, http.MethodGet, "/me", other, nil)
		require.Equal(t, http.StatusOK, res.status, "%v", res.body)
		var groupsOfClient int
		require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(DISTINCT group_id) FROM gyms WHERE auth_client_id = $1`, client).Scan(&groupsOfClient))
		assert.Equal(t, 1, groupsOfClient, "both companies are in one group")
		assert.NotEqual(t, me.data()["gym"], res.data()["gym"])
		assert.Equal(t, "20666666666", res.data()["gym"].(map[string]any)["name"], "without the company list the RUC names the gym")
	})

	t.Run("a company cannot move to another client", func(t *testing.T) {
		stolen := h.tokens.Mint(t, map[string]any{
			"sub": authUser.String(), "role": "user", "system": systemCode, "tenant": tenantID.String(),
			"client": uuid.NewString(), "client_name": "Intruso", "app_role": "admin", "name": "Rosa Titán", "dni": document,
		})
		defer func() {
			_, _ = h.pool.Exec(context.Background(), `DELETE FROM groups WHERE name = 'Intruso'`)
		}()
		res := h.call(t, http.MethodGet, "/me", stolen, nil)
		assert.Equal(t, http.StatusForbidden, res.status)
		assert.Equal(t, "NO_ACCESS", res.errorCode())
	})
}

func TestTheRoleOfTheTokenFollowsTheConsole(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	value, _ := h.people.Load(tn.Receptionist)
	claims := h.userClaims(value.(person))

	require.NoError(t, h.auth.ChangeRole(context.Background(), tn.TenantOne.String(), tn.ReceptionistAuth.String(), "admin"))
	claims["app_role"] = "admin"
	promoted := h.tokens.Mint(t, claims)
	me := h.call(t, http.MethodGet, "/me", promoted, nil)
	require.Equal(t, http.StatusOK, me.status)
	assert.Equal(t, "admin", me.data()["role"])
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/users", promoted, nil).status, "now they manage the team")
	var role string
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT role FROM users WHERE id = $1`, tn.ReceptionistID).Scan(&role))
	assert.Equal(t, "admin", role)
	var assigned int
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM user_branches WHERE user_id = $1`, tn.ReceptionistID).Scan(&assigned))
	assert.Zero(t, assigned, "an admin has no assigned branches")
}

func TestDeactivatingAUserCutsAccessAtOnce(t *testing.T) {
	h := newHarness(t)
	document, id := h.freshAdmin(t)
	token := h.token(t, document)
	require.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/me", token, nil).status)

	_, err := h.pool.Exec(context.Background(), `UPDATE users SET is_active = false WHERE id = $1`, id)
	require.NoError(t, err)

	res := h.call(t, http.MethodGet, "/me", token, nil)
	assert.Equal(t, http.StatusForbidden, res.status, "no waiting for the token to expire")
	assert.Equal(t, "USER_INACTIVE", res.errorCode())
}

func TestEditingOwnProfile(t *testing.T) {
	h := newHarness(t)
	document, _ := h.freshAdmin(t)
	token := h.token(t, document)

	updated := h.call(t, http.MethodPatch, "/me", token, map[string]string{"full_name": "  Nuevo   Nombre  ", "phone": "987654321"})
	require.Equal(t, http.StatusOK, updated.status, "%v", updated.body)
	assert.Equal(t, "Nuevo Nombre", updated.data()["full_name"])
	assert.Equal(t, "987654321", updated.data()["phone"])
	assert.Equal(t, document, updated.data()["document_number"], "the DNI cannot be edited here")

	again := h.call(t, http.MethodGet, "/me", token, nil)
	assert.Equal(t, "Nuevo Nombre", again.data()["full_name"], "the token's name does not overwrite what the person edited")

	cleared := h.call(t, http.MethodPatch, "/me", token, map[string]string{"full_name": "Nuevo Nombre", "phone": ""})
	require.Equal(t, http.StatusOK, cleared.status)
	assert.Nil(t, cleared.data()["phone"], "an empty phone is stored as null")

	invalid := h.call(t, http.MethodPatch, "/me", token, map[string]string{"full_name": "x", "phone": "123"})
	assert.Equal(t, http.StatusBadRequest, invalid.status)
	assert.Equal(t, "VALIDATION_FAILED", invalid.errorCode())
	details, _ := invalid.body["error"].(map[string]any)["details"].([]any)
	assert.Len(t, details, 2)
}

// The database itself refuses data that would break the tenancy or role rules, even if
// a bug in the application tried to write it.
func TestDatabaseEnforcesTenancyAndRoleRules(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tn := h.newTenant(t)
	other := h.newTenant(t)

	insert := func(document, role string, gym any) error {
		_, err := h.pool.Exec(ctx, `INSERT INTO users (id, gym_id, auth_user_id, document_number, full_name, role)
			VALUES ($1, $2, $3, $4, 'Persona', $5)`, ids.New(), gym, uuid.New(), document, role)
		return err
	}

	assert.Error(t, insert(uniqueDocument(), "admin", nil), "a person needs a gym")
	assert.Error(t, insert(uniqueDocument(), "owner", tn.GymOne), "owner is not a role anymore")
	assert.Error(t, insert(uniqueDocument(), "member", tn.GymOne), "the member role is not active yet")
	assert.Error(t, insert(uniqueDocument(), "superadmin", tn.GymOne), "nor is superadmin")
	assert.Error(t, insert("1000000", "admin", tn.GymOne), "a DNI has eight digits")
	assert.Error(t, insert(tn.Admin, "admin", tn.GymOne), "the DNI is unique inside a gym")
	assert.Error(t, insert(uniqueDocument(), "admin", uuid.New()), "an unknown gym is refused")
	assert.NoError(t, insert(tn.Admin, "admin", tn.GymTwo), "the same person can work for a second gym")

	link := func(userID, branchID, gym uuid.UUID) error {
		_, err := h.pool.Exec(ctx, `INSERT INTO user_branches (user_id, branch_id, gym_id) VALUES ($1, $2, $3)`, userID, branchID, gym)
		return err
	}
	assert.Error(t, link(tn.ReceptionistID, other.BranchA, tn.GymOne), "reception cannot work a branch of another gym")
	assert.Error(t, link(tn.ReceptionistID, tn.BranchC, tn.GymOne), "nor a branch of a sibling gym of the same group")
	assert.Error(t, link(tn.ReceptionistID, other.BranchA, other.GymOne), "nor borrow another gym's id")
	assert.Error(t, link(tn.ReceptionistID, tn.BranchA, tn.GymOne), "the same branch is not linked twice")
}
