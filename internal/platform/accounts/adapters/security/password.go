// Package security has the cryptographic adapters of the accounts module: password
// hashing (argon2id) and access tokens (JWT HS256).
package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Parameters follow OWASP's minimum argon2id recommendation (19 MiB, 2 passes, 1
// lane): strong enough, and light on the small container the API runs in.
const (
	memoryKiB   uint32 = 19 * 1024
	iterations  uint32 = 2
	parallelism uint8  = 1
	saltLength         = 16
	keyLength   uint32 = 32
)

// Argon2idHasher hashes passwords as "$argon2id$v=19$m=...,t=...,p=...$salt$key".
type Argon2idHasher struct{}

// Shared across all hasher instances, including simultaneous Hash and Verify calls.
var hashSlots = make(chan struct{}, 4)

// Hash returns the encoded argon2id hash of password with a fresh random salt.
func (Argon2idHasher) Hash(password string) (string, error) {
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, iterations, memoryKiB, parallelism, keyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memoryKiB, iterations, parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify checks password against an encoded hash in constant time. A hash made with
// other parameters still verifies: the parameters are read from the hash itself.
func (Argon2idHasher) Verify(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}
	var memory, passes uint32
	var lanes uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &lanes); err != nil {
		return false, errors.New("invalid argon2 parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errors.New("invalid argon2 salt")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errors.New("invalid argon2 key")
	}
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()
	got := argon2.IDKey([]byte(password), salt, passes, memory, lanes, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
