package integration_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
)

func (h harness) clientInput(t *testing.T) app.NewClient {
	t.Helper()
	id := uuid.New()
	t.Cleanup(func() {
		_, err := h.pool.Exec(context.Background(), `DELETE FROM organizations WHERE id=$1`, id)
		assert.NoError(t, err)
	})
	return app.NewClient{ID: id, Owner: app.NewOwner{DocumentNumber: uniqueDocument(), FirstNames: "Test", LastNames: "Owner", Email: "test@example.com", Phone: "999111222"}, Gyms: []app.NewGym{{Name: "Test Gym", TaxID: "20123456789", Branches: []app.NewBranch{{Name: "Main"}}}}}
}
func TestCreateClientAndExtraAgreements(t *testing.T) {
	h := newHarness(t)
	token := h.token(t, h.newSuperadmin(t), demoPassword)
	input := h.clientInput(t)
	created := h.call(t, http.MethodPost, "/clients", token, input)
	require.Equal(t, http.StatusCreated, created.status, created.body)
	assert.Equal(t, input.ID.String(), created.data()["id"])
	assert.Equal(t, "Test Owner", created.data()["name"])
	repeat := h.call(t, http.MethodPost, "/clients", token, input)
	require.Equal(t, http.StatusCreated, repeat.status, repeat.body)
	assert.Equal(t, created.data(), repeat.data())
	login := h.login(t, input.Owner.DocumentNumber, input.Owner.DocumentNumber)
	require.Equal(t, http.StatusOK, login.status)
	owner, ok := login.data()["user"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, owner["must_change_password"])
	extra := h.clientInput(t)
	extra.Gyms = append(extra.Gyms, app.NewGym{Name: "Second Gym", Branches: []app.NewBranch{{Name: "Branch 1"}, {Name: "Branch 2"}}})
	denied := h.call(t, http.MethodPost, "/clients", token, extra)
	require.Equal(t, http.StatusConflict, denied.status)
	assert.Equal(t, "PLAN_LIMIT_EXCEEDED", denied.errorCode())
	extra.AgreedExtra = true
	accepted := h.call(t, http.MethodPost, "/clients", token, extra)
	require.Equal(t, http.StatusCreated, accepted.status, accepted.body)
	var agreements int
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM plan_agreements WHERE organization_id=$1`, extra.ID).Scan(&agreements))
	assert.Equal(t, 2, agreements)
	duplicate := h.clientInput(t)
	duplicate.Owner.DocumentNumber = input.Owner.DocumentNumber
	assert.Equal(t, "USER_DOCUMENT_TAKEN", h.call(t, http.MethodPost, "/clients", token, duplicate).errorCode())
	var exists bool
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM organizations WHERE id=$1)`, duplicate.ID).Scan(&exists))
	assert.False(t, exists)
}
func TestClientCreationIsAtomic(t *testing.T) {
	h := newHarness(t)
	token := h.token(t, h.newSuperadmin(t), demoPassword)
	input := h.clientInput(t)
	input.AgreedExtra = true
	input.Gyms[0].ID = uuid.New()
	second := input.Gyms[0]
	input.Gyms = append(input.Gyms, second)
	result := h.call(t, http.MethodPost, "/clients", token, input)
	require.Equal(t, http.StatusConflict, result.status, result.body)
	var count int
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM organizations WHERE id=$1`, input.ID).Scan(&count))
	assert.Zero(t, count)
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE document_number=$1`, input.Owner.DocumentNumber).Scan(&count))
	assert.Zero(t, count)
}
func TestPlatformRolesAndConcurrentCreate(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	super := h.token(t, h.newSuperadmin(t), demoPassword)
	input := h.clientInput(t)
	for _, document := range []string{tn.Owner, tn.Admin, tn.Receptionist} {
		token := h.token(t, document, demoPassword)
		for _, path := range []string{"/clients", "/clients/summary"} {
			assert.Equal(t, http.StatusForbidden, h.call(t, http.MethodGet, path, token, nil).status)
		}
		assert.Equal(t, http.StatusForbidden, h.call(t, http.MethodPost, "/clients", token, input).status)
		assert.Equal(t, http.StatusForbidden, h.call(t, http.MethodPost, "/clients/"+tn.OrganizationID.String()+"/gyms", token, input.Gyms[0]).status)
		assert.Equal(t, http.StatusForbidden, h.call(t, http.MethodPost, "/gyms/"+tn.GymOne.String()+"/branches", token, app.NewBranch{Name: "New"}).status)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			result := h.call(t, http.MethodPost, "/clients", super, input)
			assert.Equal(t, http.StatusCreated, result.status, result.body)
		})
	}
	wg.Wait()
	var count int
	require.NoError(t, h.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE organization_id=$1`, input.ID).Scan(&count))
	assert.Equal(t, 1, count)
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/clients?search=20123456789&page_size=1", super, nil).status)
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/clients/summary", super, nil).status)
}
func TestAddGymsAndBranches(t *testing.T) {
	h := newHarness(t)
	token := h.token(t, h.newSuperadmin(t), demoPassword)
	tn := h.newTenant(t)
	gym := app.NewGym{ID: uuid.New(), Name: "Extra", Branches: []app.NewBranch{{Name: "New"}}}
	path := "/clients/" + tn.OrganizationID.String() + "/gyms"
	assert.Equal(t, "PLAN_LIMIT_EXCEEDED", h.call(t, http.MethodPost, path, token, gym).errorCode())
	gym.AgreedExtra = true
	created := h.call(t, http.MethodPost, path, token, gym)
	require.Equal(t, http.StatusCreated, created.status, created.body)
	assert.Equal(t, created.data(), h.call(t, http.MethodPost, path, token, gym).data())
	branch := app.NewBranch{ID: uuid.New(), Name: "Extra branch"}
	path = "/gyms/" + gym.ID.String() + "/branches"
	assert.Equal(t, "PLAN_LIMIT_EXCEEDED", h.call(t, http.MethodPost, path, token, branch).errorCode())
	branch.AgreedExtra = true
	created = h.call(t, http.MethodPost, path, token, branch)
	require.Equal(t, http.StatusCreated, created.status, created.body)
	// Accessibility is calculated by the catalog; platform create responses are repeatable too.
	repeat := h.call(t, http.MethodPost, path, token, branch)
	require.Equal(t, http.StatusCreated, repeat.status)
	assert.Equal(t, created.data()["id"], repeat.data()["id"])
	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodPost, "/gyms/"+uuid.NewString()+"/branches", token, branch).status)
}
