package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims are what atm-auth-api puts in an access token (see its internal/platform/token.go).
// Name, DNI and TenantRUC only identify the person and the company; authorization reads
// Role, System, Tenant and AppRole.
type Claims struct {
	jwt.RegisteredClaims
	Role   string `json:"role"`
	System string `json:"system"`
	Tenant string `json:"tenant"`
	// Client is the auth customer of the company; ClientName its name. Only a hint to place a new gym in a group.
	Client      string `json:"client"`
	ClientName  string `json:"client_name"`
	AppRole     string `json:"app_role"`
	ManageUsers bool   `json:"manage_users"`
	Name        string `json:"name"`
	DNI         string `json:"dni"`
	TenantRUC   string `json:"tenant_ruc"`
}

// The values of Claims.Role.
const (
	RoleClaimUser       = "user"
	RoleClaimSuperadmin = "superadmin"
)

// TokenVerifier checks an access token. An error wraps ErrUnauthorized: not a token to trust.
// Whether a good token opens Pulso is decided by the Guard.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (Claims, error)
}

const (
	jwksTTL            = 10 * time.Minute
	jwksRefetchGap     = 10 * time.Second
	jwksRequestTimeout = 5 * time.Second
	maxJWKSBytes       = 64 << 10
)

// VerifierConfig is how to trust atm-auth-api.
type VerifierConfig struct {
	// JWKSURL is where atm-auth-api publishes its public key.
	JWKSURL string
	Issuer  string
	Client  *http.Client
	Now     func() time.Time
}

// JWKSVerifier validates RS256 tokens against the keys of atm-auth-api. The keys are cached
// for ten minutes and fetched again at once when a token names a key we do not know (a
// rotation), at most every ten seconds.
type JWKSVerifier struct {
	cfg VerifierConfig

	mu          sync.Mutex
	keys        map[string]*rsa.PublicKey
	fetchedAt   time.Time
	lastAttempt time.Time
}

// NewJWKSVerifier returns a verifier. Nothing is fetched until the first token.
func NewJWKSVerifier(cfg VerifierConfig) *JWKSVerifier {
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: jwksRequestTimeout}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &JWKSVerifier{cfg: cfg}
}

// Verify checks signature (RS256 only), issuer and expiry.
func (v *JWKSVerifier) Verify(ctx context.Context, raw string) (Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return v.key(ctx, kid)
	},
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.cfg.Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(v.cfg.Now),
		jwt.WithLeeway(5*time.Second),
	)
	if err != nil || claims.Subject == "" {
		return Claims{}, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	return claims, nil
}

func (v *JWKSVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.cfg.Now()
	key, known := v.keys[kid]
	fresh := now.Sub(v.fetchedAt) < jwksTTL
	if known && fresh {
		return key, nil
	}
	if now.Sub(v.lastAttempt) >= jwksRefetchGap {
		v.lastAttempt = now
		if keys, err := v.fetch(ctx); err == nil {
			v.keys, v.fetchedAt = keys, now
			key, known = keys[kid]
		} else if !known {
			return nil, err
		}
	}
	if !known {
		return nil, errors.New("unknown signing key")
	}
	return key, nil
}

func (v *JWKSVerifier) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	ctx, cancel := context.WithTimeout(ctx, jwksRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.cfg.JWKSURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := v.cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch jwks: status %d", res.StatusCode)
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxJWKSBytes)).Decode(&set); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(strings.TrimRight(k.N, "="))
		e, errE := base64.RawURLEncoding.DecodeString(strings.TrimRight(k.E, "="))
		if errN != nil || errE != nil {
			continue
		}
		key := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
		if key.N.BitLen() >= 2048 && key.E > 1 {
			keys[k.Kid] = key
		}
	}
	if len(keys) == 0 {
		return nil, errors.New("jwks has no usable key")
	}
	return keys, nil
}
