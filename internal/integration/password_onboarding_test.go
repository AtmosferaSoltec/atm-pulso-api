package integration_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/adapters/security"
)

func TestRequiredPasswordChange(t *testing.T) {
	h := newHarness(t)
	document, id := h.freshUser(t)
	oldToken := h.token(t, document, demoPassword)
	hash, err := (security.Argon2idHasher{}).Hash(document)
	require.NoError(t, err)
	_, err = h.pool.Exec(context.Background(), `UPDATE users SET password_hash=$2, must_change_password=true, password_version=password_version+1 WHERE id=$1`, id, hash)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodGet, "/me", oldToken, nil).status)
	login := h.login(t, document, document)
	require.Equal(t, http.StatusOK, login.status)
	user, ok := login.data()["user"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, user["must_change_password"])
	token, ok := login.data()["access_token"].(string)
	require.True(t, ok)
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/me", token, nil).status)
	for _, request := range []struct{ method, path string }{
		{http.MethodPatch, "/me"},
		{http.MethodGet, "/documents/persons/12345678"},
		{http.MethodGet, "/documents/companies/20123456789"},
	} {
		res := h.call(t, request.method, request.path, token, nil)
		assert.Equal(t, http.StatusForbidden, res.status, request.path)
		assert.Equal(t, "PASSWORD_CHANGE_REQUIRED", res.errorCode(), request.path)
	}
	unchanged := h.call(t, http.MethodPost, "/me/password", token, map[string]string{"current_password": document, "new_password": document})
	assert.Equal(t, http.StatusBadRequest, unchanged.status)
	wrong := h.call(t, http.MethodPost, "/me/password", token, map[string]string{"current_password": "wrong-password", "new_password": "NewPassword123!"})
	assert.Equal(t, "CURRENT_PASSWORD_INCORRECT", wrong.errorCode())
	changed := h.call(t, http.MethodPost, "/me/password", token, map[string]string{"current_password": document, "new_password": "NewPassword123!"})
	require.Equal(t, http.StatusOK, changed.status, changed.body)
	assert.Equal(t, map[string]any{"login_required": true}, changed.data())
	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodGet, "/me", token, nil).status)
	assert.Equal(t, http.StatusUnauthorized, h.login(t, document, document).status)
	fresh := h.login(t, document, "NewPassword123!")
	require.Equal(t, http.StatusOK, fresh.status)
	freshUser, ok := fresh.data()["user"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, freshUser["must_change_password"])
	freshToken, ok := fresh.data()["access_token"].(string)
	require.True(t, ok)
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/documents/persons/12345678", freshToken, nil).status)
}
