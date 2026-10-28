package security

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasher(t *testing.T) {
	hasher := Argon2idHasher{}
	hash, err := hasher.Hash("Pulso2026!")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$"))

	ok, err := hasher.Verify("Pulso2026!", hash)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = hasher.Verify("pulso2026!", hash)
	require.NoError(t, err)
	assert.False(t, ok, "a different password must not verify")

	other, err := hasher.Hash("Pulso2026!")
	require.NoError(t, err)
	assert.NotEqual(t, hash, other, "each hash has its own salt")
}

func TestHasherRejectsMalformedHashes(t *testing.T) {
	hasher := Argon2idHasher{}
	for _, bad := range []string{
		"", "plain-text", "$argon2i$v=19$m=1,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=18$m=19456,t=2,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=x$c2FsdA$a2V5",
		"$argon2id$v=19$m=19456,t=2,p=1$!!$a2V5",
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$",
	} {
		ok, err := hasher.Verify("whatever", bad)
		assert.Error(t, err, "%q", bad)
		assert.False(t, ok)
	}
}

var (
	secret = strings.Repeat("s", 40)
	now    = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
)

func TestTokenRoundTrip(t *testing.T) {
	signer := NewJWT(secret, 8*time.Hour)
	userID := uuid.New()

	token, err := signer.Issue(userID, 3, now)
	require.NoError(t, err)
	assert.Equal(t, now.Add(8*time.Hour), token.ExpiresAt)

	claims, err := signer.Verify(token.Value, now.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, userID, claims.UserID)
	assert.Equal(t, 3, claims.PasswordVersion)
}

func TestTokenRejections(t *testing.T) {
	signer := NewJWT(secret, time.Hour)
	userID := uuid.New()
	valid, err := signer.Issue(userID, 1, now)
	require.NoError(t, err)

	t.Run("expired", func(t *testing.T) {
		_, err := signer.Verify(valid.Value, now.Add(time.Hour+time.Second))
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("signed with another secret", func(t *testing.T) {
		other, err := NewJWT(strings.Repeat("x", 40), time.Hour).Issue(userID, 1, now)
		require.NoError(t, err)
		_, err = signer.Verify(other.Value, now)
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("tampered", func(t *testing.T) {
		_, err := signer.Verify(valid.Value+"x", now)
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("garbage", func(t *testing.T) {
		_, err := signer.Verify("not-a-token", now)
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("unsigned (alg none)", func(t *testing.T) {
		unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims{RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: userID.String(),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		}, PasswordVersion: 1}).SignedString(jwt.UnsafeAllowNoneSignatureType)
		require.NoError(t, err)
		_, err = signer.Verify(unsigned, now)
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("another HMAC algorithm", func(t *testing.T) {
		hs512, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims{RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: userID.String(),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		}, PasswordVersion: 1}).SignedString([]byte(secret))
		require.NoError(t, err)
		_, err = signer.Verify(hs512, now)
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("another issuer", func(t *testing.T) {
		foreign, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "someone-else", Subject: userID.String(),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		}, PasswordVersion: 1}).SignedString([]byte(secret))
		require.NoError(t, err)
		_, err = signer.Verify(foreign, now)
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("subject that is not a user id", func(t *testing.T) {
		bad, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: "not-a-uuid",
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		}, PasswordVersion: 1}).SignedString([]byte(secret))
		require.NoError(t, err)
		_, err = signer.Verify(bad, now)
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("without a password version", func(t *testing.T) {
		legacy, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
			Issuer: issuer, Subject: userID.String(),
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		}).SignedString([]byte(secret))
		require.NoError(t, err)
		_, err = signer.Verify(legacy, now)
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
	t.Run("no expiry", func(t *testing.T) {
		eternal, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{RegisteredClaims: jwt.RegisteredClaims{
			Issuer: issuer, Subject: userID.String(), IssuedAt: jwt.NewNumericDate(now),
		}, PasswordVersion: 1}).SignedString([]byte(secret))
		require.NoError(t, err)
		_, err = signer.Verify(eternal, now)
		assert.ErrorIs(t, err, ErrInvalidToken)
	})
}
