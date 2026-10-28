package security

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/accounts/app"
)

// issuer identifies tokens made by this API.
const issuer = "atm-pulso-api"

// ErrInvalidToken is returned for any token that must not be trusted.
var ErrInvalidToken = errors.New("invalid token")

// claims are the registered JWT claims plus the user's password version. Nothing else
// travels in the token: role and scope are read from the database on every request.
type claims struct {
	jwt.RegisteredClaims
	PasswordVersion int `json:"pv"`
}

// JWT issues and verifies access tokens (HS256). There is no refresh token and no
// token store: a token carries the user id, the password version and its dates, and
// the user is loaded from the database on every request.
type JWT struct {
	secret []byte
	ttl    time.Duration
}

// NewJWT creates the issuer/verifier. ttl is how long a login lasts.
func NewJWT(secret string, ttl time.Duration) JWT {
	return JWT{secret: []byte(secret), ttl: ttl}
}

// Issue signs a token for userID that expires ttl after now.
func (j JWT) Issue(userID uuid.UUID, passwordVersion int, now time.Time) (app.Token, error) {
	expiresAt := now.Add(j.ttl)
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		PasswordVersion: passwordVersion,
	}).SignedString(j.secret)
	if err != nil {
		return app.Token{}, fmt.Errorf("sign access token: %w", err)
	}
	return app.Token{Value: signed, ExpiresAt: expiresAt}, nil
}

// Verify checks signature, algorithm, issuer and expiry, and returns who the token is for
// and the password version it was issued under.
func (j JWT) Verify(token string, now time.Time) (app.TokenClaims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	var parsedClaims claims
	parsed, err := parser.ParseWithClaims(token, &parsedClaims, func(*jwt.Token) (any, error) { return j.secret, nil })
	if err != nil || !parsed.Valid {
		return app.TokenClaims{}, ErrInvalidToken
	}
	userID, err := uuid.Parse(parsedClaims.Subject)
	if err != nil || parsedClaims.PasswordVersion < 1 {
		return app.TokenClaims{}, ErrInvalidToken
	}
	return app.TokenClaims{UserID: userID, PasswordVersion: parsedClaims.PasswordVersion}, nil
}
