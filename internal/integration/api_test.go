// Package integration_test exercises the real router, the real modules and a real
// PostgreSQL together. It is skipped without TEST_DATABASE_URL (see AGENTS.md).
package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/authapi"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/identity"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/access"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/documents"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/members"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/reports"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers"
	vouchersdomain "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/vouchers/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/health"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/httpserver"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/middleware"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/testsupport"
)

const systemCode = "pulso"

// harness is the API assembled like cmd/api does, on a migrated database.
type harness struct {
	router   *gin.Engine
	pool     *pgxpool.Pool
	vouchers *memoryVouchers
	// auth plays atm-auth-api's service API (companies and members).
	auth *authapi.Fake
	// people are the fixtures a token can be minted for, by DNI.
	people *sync.Map
	tokens *testsupport.TokenIssuer
}

func newHarness(t *testing.T) harness {
	t.Helper()
	pool := testsupport.MigratedPool(t)

	var sink io.Writer = io.Discard
	if os.Getenv("TEST_LOG") != "" {
		sink = os.Stderr
	}
	quiet := slog.New(slog.NewTextHandler(sink, nil))
	router, api, err := httpserver.NewRouter(httpserver.Options{Logger: quiet, RateLimitPerMinute: 100_000, BodyLimits: map[string]int64{"POST " + httpserver.APIPrefix + "/receipts/:id/vouchers": vouchersdomain.MaxBytes + 1}})
	require.NoError(t, err)

	health.Register(api, pool)

	voucherStore := newMemoryVouchers()
	authFake := authapi.NewFake()
	tokens := testsupport.Tokens(t)
	accountsModule := accounts.New(accounts.Deps{Pool: pool, Clock: clock.System{}, Auth: authFake})
	guard := auth.NewGuard(auth.NewJWKSVerifier(auth.VerifierConfig{JWKSURL: tokens.JWKSURL, Issuer: testsupport.TestIssuer}), accountsModule.Resolver(), systemCode)
	protected := api.Group("", guard.User())
	accountsModule.Routes(protected)
	accountsModule.AdminRoutes(api.Group("/admin", middleware.NewRateLimiter(100_000).Middleware(), guard.Superadmin()))
	documents.New(identity.Fake{}).Routes(protected)
	members.New(members.Deps{Pool: pool, Clock: clock.System{}}).Routes(protected)
	access.New(access.Deps{Pool: pool, Clock: clock.System{}}).Routes(protected)
	vouchers.New(vouchers.Deps{Pool: pool, Store: voucherStore}).Routes(protected)
	reports.New(reports.Deps{Pool: pool, Clock: clock.System{}}).Routes(protected, middleware.NewRateLimiter(100_000).Middleware())
	return harness{router: router, pool: pool, vouchers: voucherStore, auth: authFake, people: &sync.Map{}, tokens: tokens}
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

// userClaims are the claims atm-auth-api puts in the token of a person for a company.
func (h harness) userClaims(p person) map[string]any {
	return map[string]any{
		"sub": p.AuthUserID.String(), "role": "user", "system": systemCode,
		"tenant": p.TenantID.String(), "client": p.ClientID.String(), "client_name": p.ClientName,
		"app_role": p.Role, "name": p.Name, "dni": p.Document, "tenant_ruc": "20123456789",
	}
}

// token mints the access token atm-auth-api would give the fixture person with that DNI.
func (h harness) token(t *testing.T, document string) string {
	t.Helper()
	value, ok := h.people.Load(document)
	require.True(t, ok, "no fixture person with DNI %s", document)
	return h.tokens.Mint(t, h.userClaims(value.(person)))
}

// superToken is the token of the platform administrator (no system, no company).
func (h harness) superToken(t *testing.T) string {
	t.Helper()
	return h.tokens.Mint(t, map[string]any{"sub": uuid.NewString(), "role": "superadmin"})
}

func TestDocumentLookups(t *testing.T) {
	h := newHarness(t)
	token := h.token(t, h.newTenant(t).Receptionist) // reception may look documents up too

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
