package testsupport

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// TestIssuer is the `iss` of the tokens Tokens mints.
const TestIssuer = "https://auth.test"

// TokenIssuer plays atm-auth-api: it signs RS256 tokens with a throwaway key and publishes
// the matching JWKS over HTTP, so the code under test verifies them the way it does in production.
type TokenIssuer struct {
	key     *rsa.PrivateKey
	kid     string
	JWKSURL string
}

var (
	issuerOnce sync.Once
	issuer     *TokenIssuer
)

// Tokens returns the shared issuer of the test process (one key, one JWKS server).
func Tokens(t *testing.T) *TokenIssuer {
	t.Helper()
	issuerOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		issuer = &TokenIssuer{key: key, kid: "test-key"}
		mux := http.NewServeMux()
		mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
				"kty": "RSA", "kid": issuer.kid, "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
			}}})
		})
		issuer.JWKSURL = httptest.NewServer(mux).URL + "/.well-known/jwks.json"
	})
	return issuer
}

// Mint signs claims. Missing `iss`, `iat` and `exp` get defaults (valid for an hour).
func (i *TokenIssuer) Mint(t *testing.T, claims map[string]any) string {
	t.Helper()
	return i.MintWith(t, i.key, i.kid, claims)
}

// MintWith signs with another key or key id, to test tokens that must be refused.
func (i *TokenIssuer) MintWith(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	now := time.Now()
	full := jwt.MapClaims{"iss": TestIssuer, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()}
	for k, v := range claims {
		full[k] = v
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, full)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}
