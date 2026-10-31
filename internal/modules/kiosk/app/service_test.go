package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/kiosk/domain"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/platform/auth"
)

type memRepo struct{ hash string }

func (m *memRepo) PINHash(context.Context, uuid.UUID) (string, error) {
	if m.hash == "" {
		return "", domain.ErrPINNotSet
	}
	return m.hash, nil
}
func (m *memRepo) SetPINHash(_ context.Context, _, _ uuid.UUID, h string) error {
	m.hash = h
	return nil
}

type plainHasher struct{}

func (plainHasher) Hash(pin string) (string, error) { return "h:" + pin, nil }
func (plainHasher) Matches(hash, pin string) bool   { return hash == "h:"+pin }

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time { return c.now }

func TestVerifyExitLocksAfterFiveWrongPINs(t *testing.T) {
	clk := &fixedClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	svc := &Service{Repo: &memRepo{}, Hasher: plainHasher{}, Clock: clk}
	admin := auth.Principal{UserID: uuid.New(), GymID: uuid.New(), Role: auth.RoleAdmin}
	reception := auth.Principal{UserID: uuid.New(), GymID: admin.GymID, Role: auth.RoleReceptionist}
	ctx := context.Background()

	assert.ErrorIs(t, svc.VerifyExit(ctx, reception, "1234"), domain.ErrPINNotSet)
	assert.ErrorIs(t, svc.SetPIN(ctx, reception, "1234"), domain.ErrForbidden)
	var verr *domain.ValidationError
	assert.ErrorAs(t, svc.SetPIN(ctx, admin, "12"), &verr)
	require.NoError(t, svc.SetPIN(ctx, admin, "2468"))
	st, err := svc.Settings(ctx, reception)
	require.NoError(t, err)
	assert.True(t, st.PINSet)

	for i := 0; i < domain.MaxFailures-1; i++ {
		assert.ErrorIs(t, svc.VerifyExit(ctx, reception, "0000"), domain.ErrPINWrong)
	}
	assert.ErrorIs(t, svc.VerifyExit(ctx, reception, "0000"), domain.ErrLocked, "the fifth wrong try locks")
	assert.ErrorIs(t, svc.VerifyExit(ctx, reception, "2468"), domain.ErrLocked, "even the right PIN while locked")
	require.NoError(t, svc.VerifyExit(ctx, admin, "2468"), "another user is not locked")

	clk.now = clk.now.Add(domain.LockFor + time.Second)
	require.NoError(t, svc.VerifyExit(ctx, reception, "2468"), "the lock expires")
	assert.ErrorIs(t, svc.VerifyExit(ctx, reception, "1111"), domain.ErrPINWrong, "and the counter restarted")
}
