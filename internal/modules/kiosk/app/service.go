// Package app holds the use cases of the kiosk module: the exit PIN of the access tablet.
package app

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/kiosk/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/clock"
)

// Repository is the persistence port.
type Repository interface {
	// PINHash returns the stored hash; domain.ErrPINNotSet when the gym has none.
	PINHash(ctx context.Context, gymID uuid.UUID) (string, error)
	SetPINHash(ctx context.Context, gymID, actorID uuid.UUID, hash string) error
}

// Hasher hashes and checks PINs (a slow, salted hash).
type Hasher interface {
	Hash(pin string) (string, error)
	Matches(hash, pin string) bool
}

// Settings is what the web may know about the kiosk configuration: never the PIN or its hash.
type Settings struct {
	PINSet bool `json:"pin_set"`
}

// Service implements the use cases.
type Service struct {
	Repo   Repository
	Hasher Hasher
	Clock  clock.Clock

	mu       sync.Mutex
	failures map[uuid.UUID]*failure
}

type failure struct {
	count       int
	lockedUntil time.Time
}

// Settings tells whether the gym has an exit PIN (admin and reception: the kiosk needs it before starting).
func (s *Service) Settings(ctx context.Context, p auth.Principal) (Settings, error) {
	if _, err := s.Repo.PINHash(ctx, p.GymID); err != nil {
		if err == domain.ErrPINNotSet {
			return Settings{}, nil
		}
		return Settings{}, err
	}
	return Settings{PINSet: true}, nil
}

// SetPIN stores the hash of a new exit PIN. Admin only.
func (s *Service) SetPIN(ctx context.Context, p auth.Principal, pin string) error {
	if p.Role != auth.RoleAdmin {
		return domain.ErrForbidden
	}
	if !domain.ValidPIN(pin) {
		return &domain.ValidationError{Issues: []domain.Issue{{Field: "pin", Code: domain.CodePINInvalid}}}
	}
	hash, err := s.Hasher.Hash(pin)
	if err != nil {
		return err
	}
	return s.Repo.SetPINHash(ctx, p.GymID, p.UserID, hash)
}

// VerifyExit checks the PIN that takes the tablet out of kiosk mode. Wrong tries are counted per user
// (in memory: one replica): after domain.MaxFailures in a row the user is locked for domain.LockFor, even
// for the right PIN, so a PIN cannot be guessed at the tablet.
func (s *Service) VerifyExit(ctx context.Context, p auth.Principal, pin string) error {
	now := s.Clock.Now()
	if s.locked(p.UserID, now) {
		return domain.ErrLocked
	}
	hash, err := s.Repo.PINHash(ctx, p.GymID)
	if err != nil {
		return err
	}
	if !s.Hasher.Matches(hash, pin) {
		if s.fail(p.UserID, now) {
			return domain.ErrLocked
		}
		return domain.ErrPINWrong
	}
	s.mu.Lock()
	delete(s.failures, p.UserID)
	s.mu.Unlock()
	return nil
}

func (s *Service) locked(user uuid.UUID, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.failures[user]
	if f == nil {
		return false
	}
	if !f.lockedUntil.IsZero() && !now.Before(f.lockedUntil) {
		delete(s.failures, user)
		return false
	}
	return now.Before(f.lockedUntil)
}

// fail records a wrong try and reports whether it locked the user.
func (s *Service) fail(user uuid.UUID, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failures == nil {
		s.failures = map[uuid.UUID]*failure{}
	}
	f := s.failures[user]
	if f == nil {
		f = &failure{}
		s.failures[user] = f
	}
	f.count++
	if f.count >= domain.MaxFailures {
		f.lockedUntil = now.Add(domain.LockFor)
		f.count = 0
		return true
	}
	return false
}
