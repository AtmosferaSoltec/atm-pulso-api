// Package integration_test exercises the real router, the real modules and a real
// PostgreSQL together. It is skipped without TEST_DATABASE_URL (see AGENTS.md).
package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/identity"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/access"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/adapters/security"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/documents"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/reports"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/health"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/httpserver"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/middleware"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/testsupport"
)

const (
	jwtSecret    = "integration-test-secret-integration-test-secret"
	demoPassword = "Pulso2026!"
)

// harness is the API assembled like cmd/api does, on a migrated database.
type harness struct {
	router *gin.Engine
	pool   *pgxpool.Pool
	logos  *memoryLogos
}

func newHarness(t *testing.T) harness {
	t.Helper()
	pool := testsupport.MigratedPool(t)

	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	router, api, err := httpserver.NewRouter(httpserver.Options{Logger: quiet, RateLimitPerMinute: 100_000})
	require.NoError(t, err)

	health.Register(api, pool)

	logos := newMemoryLogos()
	accountsModule, err := accounts.New(accounts.Deps{Pool: pool, Clock: clock.System{}, JWTSecret: jwtSecret, AccessTokenTTL: 8 * time.Hour, Logos: logos})
	require.NoError(t, err)
	limited := api.Group("", middleware.NewRateLimiter(100_000).Middleware())
	protected := api.Group("", auth.Require(accountsModule.Authenticator()))
	accountsModule.Routes(limited, protected)
	accountsModule.PublicRoutes(api)
	documents.New(identity.Fake{}).Routes(protected)
	scope := auth.OrganizationScope(accountsModule.Organizations())
	members.New(members.Deps{Pool: pool, Clock: clock.System{}}).Routes(protected, scope)
	access.New(access.Deps{Pool: pool, Clock: clock.System{}}).Routes(protected, scope)
	reports.New(reports.Deps{Pool: pool, Clock: clock.System{}}).Routes(protected, scope, middleware.NewRateLimiter(100_000).Middleware())
	return harness{router: router, pool: pool, logos: logos}
}

type response struct {
	status int
	header http.Header
	body   map[string]any
}

func (r response) data() map[string]any {
	data, _ := r.body["data"].(map[string]any)
	return data
}

func (r response) errorCode() string {
	e, _ := r.body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func (h harness) call(t *testing.T, method, path, token string, body any) response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, httpserver.APIPrefix+path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)

	out := response{status: rec.Code, header: rec.Header()}
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out.body), rec.Body.String())
	}
	return out
}

// callRaw returns the recorder for endpoints that do not answer JSON (the CSV download).
func (h harness) callRaw(t *testing.T, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, httpserver.APIPrefix+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func (h harness) login(t *testing.T, document, password string) response {
	t.Helper()
	return h.call(t, http.MethodPost, "/auth/login", "", map[string]string{"document_number": document, "password": password})
}

func (h harness) token(t *testing.T, document, password string) string {
	t.Helper()
	res := h.login(t, document, password)
	require.Equal(t, http.StatusOK, res.status, "%v", res.body)
	token, _ := res.data()["access_token"].(string)
	require.NotEmpty(t, token)
	return token
}

func TestLoginAndMeForEveryRole(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	superadmin := h.newSuperadmin(t)

	cases := []struct {
		name         string
		document     string
		role         string
		organization any
		gym          any
		branches     []string
	}{
		{"superadmin", superadmin, "superadmin", nil, nil, nil},
		{"owner", tn.Owner, "owner", tn.OrganizationName, nil, nil},
		{"admin", tn.Admin, "admin", tn.OrganizationName, tn.GymOneName, nil},
		{"receptionist", tn.Receptionist, "receptionist", tn.OrganizationName, nil, []string{"Sede A", "Sede B"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			login := h.login(t, tc.document, demoPassword)
			require.Equal(t, http.StatusOK, login.status, "%v", login.body)
			assert.Equal(t, "Bearer", login.data()["token_type"])
			assert.Greater(t, login.data()["expires_in"], float64(0))

			token, _ := login.data()["access_token"].(string)
			me := h.call(t, http.MethodGet, "/me", token, nil)
			require.Equal(t, http.StatusOK, me.status)
			user := me.data()
			assert.Equal(t, tc.document, user["document_number"])
			assert.Equal(t, "dni", user["document_type"])
			assert.Equal(t, tc.role, user["role"])
			assert.NotContains(t, user, "password_hash", "the hash never leaves the server")

			name := func(key string) any {
				if v, ok := user[key].(map[string]any); ok {
					return v["name"]
				}
				return nil
			}
			assert.Equal(t, tc.organization, name("organization"))
			assert.Equal(t, tc.gym, name("gym"))

			branches, _ := user["branches"].([]any)
			var names []string
			for _, b := range branches {
				names = append(names, b.(map[string]any)["name"].(string))
			}
			assert.Equal(t, tc.branches, names)
			// The sign-in answer carries the same user as /me.
			assert.Equal(t, user, login.data()["user"])
		})
	}
}

func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	h := newHarness(t)

	tn := h.newTenant(t)
	wrongPassword := h.login(t, tn.Owner, "not-the-password")
	unknownUser := h.login(t, uniqueDocument(), demoPassword)
	assert.Equal(t, http.StatusUnauthorized, wrongPassword.status)
	assert.Equal(t, http.StatusUnauthorized, unknownUser.status)
	assert.Equal(t, "INVALID_CREDENTIALS", wrongPassword.errorCode())
	assert.Equal(t, wrongPassword.body["error"], unknownUser.body["error"], "same code, message and details")

	malformed := h.login(t, "abc", "")
	assert.Equal(t, http.StatusBadRequest, malformed.status)
	assert.Equal(t, "VALIDATION_FAILED", malformed.errorCode())

	notJSON := h.call(t, http.MethodPost, "/auth/login", "", "not an object")
	assert.Equal(t, http.StatusBadRequest, notJSON.status)
}

func TestFiveFailuresLockTheAccountAndTellHowLong(t *testing.T) {
	h := newHarness(t)
	document, _ := h.freshUser(t)

	for range 5 {
		assert.Equal(t, http.StatusUnauthorized, h.login(t, document, "wrong-password").status)
	}
	locked := h.login(t, document, demoPassword)
	assert.Equal(t, http.StatusTooManyRequests, locked.status)
	assert.Equal(t, "TOO_MANY_ATTEMPTS", locked.errorCode())
	assert.NotEmpty(t, locked.header.Get("Retry-After"))
}

func TestPrivateRoutesNeedAValidToken(t *testing.T) {
	h := newHarness(t)

	for name, token := range map[string]string{
		"no token":     "",
		"garbage":      "not-a-token",
		"wrong secret": mustIssue(t, "another-secret-another-secret-another-secret", uuid.New(), 1, time.Hour),
		"expired":      mustIssue(t, jwtSecret, uuid.New(), 1, -time.Minute),
		"unknown user": mustIssue(t, jwtSecret, uuid.New(), 1, time.Hour),
	} {
		t.Run(name, func(t *testing.T) {
			for _, path := range []string{"/me", "/documents/persons/45678912"} {
				res := h.call(t, http.MethodGet, path, token, nil)
				assert.Equal(t, http.StatusUnauthorized, res.status, path)
				assert.Equal(t, "UNAUTHORIZED", res.errorCode())
			}
		})
	}

	t.Run("health stays public", func(t *testing.T) {
		// Registered by main through platform/health; the router itself must not guard the prefix.
		res := h.call(t, http.MethodGet, "/nope", "", nil)
		assert.Equal(t, http.StatusNotFound, res.status)
		assert.Equal(t, "NOT_FOUND", res.errorCode())
	})
}

func mustIssue(t *testing.T, secret string, userID uuid.UUID, version int, ttl time.Duration) string {
	t.Helper()
	token, err := security.NewJWT(secret, ttl).Issue(userID, version, time.Now())
	require.NoError(t, err)
	return token.Value
}

func TestDeactivatingAUserCutsAccessAtOnce(t *testing.T) {
	h := newHarness(t)
	document, id := h.freshUser(t)
	token := h.token(t, document, demoPassword)
	require.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/me", token, nil).status)

	_, err := h.pool.Exec(context.Background(), `UPDATE users SET is_active = false WHERE id = $1`, id)
	require.NoError(t, err)

	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodGet, "/me", token, nil).status, "no waiting for the token to expire")
	assert.Equal(t, http.StatusUnauthorized, h.login(t, document, demoPassword).status)
}

func TestEditingOwnProfile(t *testing.T) {
	h := newHarness(t)
	document, _ := h.freshUser(t)
	token := h.token(t, document, demoPassword)

	updated := h.call(t, http.MethodPatch, "/me", token, map[string]string{"full_name": "  Nuevo   Nombre  ", "phone": "987654321"})
	require.Equal(t, http.StatusOK, updated.status, "%v", updated.body)
	assert.Equal(t, "Nuevo Nombre", updated.data()["full_name"])
	assert.Equal(t, "987654321", updated.data()["phone"])
	assert.Equal(t, document, updated.data()["document_number"], "the DNI cannot be edited here")

	again := h.call(t, http.MethodGet, "/me", token, nil)
	assert.Equal(t, "Nuevo Nombre", again.data()["full_name"])

	cleared := h.call(t, http.MethodPatch, "/me", token, map[string]string{"full_name": "Nuevo Nombre", "phone": ""})
	require.Equal(t, http.StatusOK, cleared.status)
	assert.Nil(t, cleared.data()["phone"], "an empty phone is stored as null")

	invalid := h.call(t, http.MethodPatch, "/me", token, map[string]string{"full_name": "x", "phone": "123"})
	assert.Equal(t, http.StatusBadRequest, invalid.status)
	assert.Equal(t, "VALIDATION_FAILED", invalid.errorCode())
	details, _ := invalid.body["error"].(map[string]any)["details"].([]any)
	assert.Len(t, details, 2)
}

func TestChangingThePassword(t *testing.T) {
	h := newHarness(t)
	document, _ := h.freshUser(t)
	oldToken := h.token(t, document, demoPassword)

	wrong := h.call(t, http.MethodPost, "/me/password", oldToken, map[string]string{"current_password": "nope", "new_password": "NuevaClave123"})
	assert.Equal(t, http.StatusBadRequest, wrong.status, "400, so the web does not think the session ended")
	assert.Equal(t, "CURRENT_PASSWORD_INCORRECT", wrong.errorCode())

	weak := h.call(t, http.MethodPost, "/me/password", oldToken, map[string]string{"current_password": demoPassword, "new_password": "corta"})
	assert.Equal(t, http.StatusBadRequest, weak.status)
	assert.Equal(t, "VALIDATION_FAILED", weak.errorCode())

	changed := h.call(t, http.MethodPost, "/me/password", oldToken, map[string]string{"current_password": demoPassword, "new_password": "NuevaClave123"})
	require.Equal(t, http.StatusOK, changed.status, "%v", changed.body)
	newToken, _ := changed.data()["access_token"].(string)
	require.NotEmpty(t, newToken)

	// Immediately, with no waiting: the old token is dead and the new one works.
	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodGet, "/me", oldToken, nil).status)
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/me", newToken, nil).status)
	assert.Equal(t, http.StatusUnauthorized, h.login(t, document, demoPassword).status)
	assert.Equal(t, http.StatusOK, h.login(t, document, "NuevaClave123").status)
}

func TestDocumentLookups(t *testing.T) {
	h := newHarness(t)
	token := h.token(t, h.newTenant(t).Receptionist, demoPassword) // reception may look documents up too

	person := h.call(t, http.MethodGet, "/documents/persons/45678912", token, nil)
	require.Equal(t, http.StatusOK, person.status, "%v", person.body)
	assert.Equal(t, "45678912", person.data()["document_number"])
	assert.NotEmpty(t, person.data()["first_names"])
	assert.NotEmpty(t, person.data()["paternal_surname"])
	assert.True(t, strings.Contains(person.data()["full_name"].(string), person.data()["paternal_surname"].(string)))

	company := h.call(t, http.MethodGet, "/documents/companies/20123456789", token, nil)
	require.Equal(t, http.StatusOK, company.status)
	assert.Contains(t, company.data()["legal_name"], "S.A.C.")

	assert.Equal(t, http.StatusNotFound, h.call(t, http.MethodGet, "/documents/persons/00000000", token, nil).status)
	assert.Equal(t, "DOCUMENT_NOT_FOUND", h.call(t, http.MethodGet, "/documents/persons/00000000", token, nil).errorCode())

	for _, path := range []string{"/documents/persons/123", "/documents/persons/1234567a", "/documents/companies/12345678", "/documents/companies/abc"} {
		res := h.call(t, http.MethodGet, path, token, nil)
		assert.Equal(t, http.StatusBadRequest, res.status, path)
		assert.Equal(t, "VALIDATION_FAILED", res.errorCode(), path)
	}
}

// The database itself refuses data that would break the tenancy or role rules, even if
// a bug in the application tried to write it.
func TestDatabaseEnforcesTenancyAndRoleRules(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	hash := h.hash(t)
	tn := h.newTenant(t)
	other := h.newTenant(t)

	insert := func(document, role string, organization, gym any) error {
		_, err := h.pool.Exec(ctx, `INSERT INTO users (id, organization_id, gym_id, document_number, full_name, role, password_hash)
			VALUES ($1, $2, $3, $4, 'Persona', $5, $6)`, ids.New(), organization, gym, document, role, hash)
		return err
	}

	assert.Error(t, insert(uniqueDocument(), "admin", tn.OrganizationID, other.GymOne), "an admin cannot manage a gym of another organization")
	assert.Error(t, insert(uniqueDocument(), "owner", nil, nil), "an owner needs an organization")
	assert.Error(t, insert(uniqueDocument(), "superadmin", tn.OrganizationID, nil), "the superadmin belongs to no organization")
	assert.Error(t, insert(uniqueDocument(), "admin", tn.OrganizationID, nil), "an admin needs a gym")
	assert.Error(t, insert(uniqueDocument(), "cashier", tn.OrganizationID, nil), "unknown roles are refused")
	assert.Error(t, insert("1000000", "owner", tn.OrganizationID, nil), "a DNI has eight digits")
	assert.Error(t, insert(tn.Owner, "owner", tn.OrganizationID, nil), "the DNI is the login: it is unique")
	assert.NoError(t, insert(uniqueDocument(), "admin", tn.OrganizationID, tn.GymTwo), "an admin of its own organization's gym is fine")
}
