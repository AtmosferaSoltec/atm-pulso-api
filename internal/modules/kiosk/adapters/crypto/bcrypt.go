// Package crypto hashes the kiosk exit PIN with bcrypt (salted, slow).
package crypto

import "golang.org/x/crypto/bcrypt"

// Bcrypt implements app.Hasher.
type Bcrypt struct{ Cost int }

// Hash returns the bcrypt hash of pin.
func (b Bcrypt) Hash(pin string) (string, error) {
	cost := b.Cost
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	out, err := bcrypt.GenerateFromPassword([]byte(pin), cost)
	return string(out), err
}

// Matches says whether pin produced hash.
func (Bcrypt) Matches(hash, pin string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pin)) == nil
}
