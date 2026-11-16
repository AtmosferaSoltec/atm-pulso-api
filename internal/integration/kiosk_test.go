package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKioskExitPINIsGuardedHashedAndThrottled(t *testing.T) {
	h := newHarness(t)
	tn := h.newTenant(t)
	admin, reception := h.token(t, tn.Admin), h.token(t, tn.Receptionist)

	// The kiosk is a base module: every gym has it, with no switch.
	assert.Equal(t, 401, h.call(t, http.MethodGet, "/kiosk/settings", "", nil).status)

	// No PIN yet.
	settings := h.call(t, http.MethodGet, "/kiosk/settings", reception, nil)
	require.Equal(t, 200, settings.status, settings.body)
	assert.Equal(t, false, settings.data()["pin_set"])
	noPIN := h.call(t, http.MethodPost, "/kiosk/exit", reception, map[string]any{"pin": "1234"})
	assert.Equal(t, 409, noPIN.status)
	assert.Equal(t, "KIOSK_PIN_NOT_SET", noPIN.errorCode())

	// Only the admin sets it; it must be 4 to 8 digits.
	assert.Equal(t, 403, h.call(t, http.MethodPut, "/kiosk/pin", reception, map[string]any{"pin": "1234"}).status)
	for _, bad := range []string{"", "123", "123456789", "12a4", "12 34"} {
		res := h.call(t, http.MethodPut, "/kiosk/pin", admin, map[string]any{"pin": bad})
		assert.Equal(t, 400, res.status, bad)
		assert.Equal(t, "VALIDATION_FAILED", res.errorCode(), bad)
	}
	require.Equal(t, 200, h.call(t, http.MethodPut, "/kiosk/pin", admin, map[string]any{"pin": "2468"}).status)

	// Stored as a hash, never the PIN.
	var hash string
	require.NoError(t, h.pool.QueryRow(t.Context(), `SELECT pin_hash FROM kiosk_settings WHERE gym_id = $1`, tn.GymOne).Scan(&hash))
	assert.NotContains(t, hash, "2468")
	assert.Contains(t, hash, "$2")
	settings = h.call(t, http.MethodGet, "/kiosk/settings", reception, nil)
	assert.Equal(t, true, settings.data()["pin_set"])
	assert.NotContains(t, settings.body, "pin_hash")

	// Another gym's PIN does not open this one.
	otherGym := h.call(t, http.MethodPost, "/kiosk/exit", h.token(t, tn.AdminTwo), map[string]any{"pin": "2468"})
	assert.Equal(t, 409, otherGym.status, "gym two has no PIN of its own yet")
	assert.Equal(t, "KIOSK_PIN_NOT_SET", otherGym.errorCode())

	// Right PIN exits; wrong PINs are counted and lock the user after five.
	assert.Equal(t, 200, h.call(t, http.MethodPost, "/kiosk/exit", reception, map[string]any{"pin": "2468"}).status)
	for i := 0; i < 4; i++ {
		res := h.call(t, http.MethodPost, "/kiosk/exit", reception, map[string]any{"pin": "0000"})
		assert.Equal(t, 403, res.status)
		assert.Equal(t, "KIOSK_PIN_INVALID", res.errorCode())
	}
	locked := h.call(t, http.MethodPost, "/kiosk/exit", reception, map[string]any{"pin": "0000"})
	assert.Equal(t, 429, locked.status)
	assert.Equal(t, "KIOSK_PIN_LOCKED", locked.errorCode())
	assert.Equal(t, 429, h.call(t, http.MethodPost, "/kiosk/exit", reception, map[string]any{"pin": "2468"}).status, "locked even with the right PIN")
	assert.Equal(t, 200, h.call(t, http.MethodPost, "/kiosk/exit", admin, map[string]any{"pin": "2468"}).status, "another user is not locked")

	// The admin can change it; the old one stops working.
	require.Equal(t, 200, h.call(t, http.MethodPut, "/kiosk/pin", admin, map[string]any{"pin": "13579"}).status)
	assert.Equal(t, 403, h.call(t, http.MethodPost, "/kiosk/exit", admin, map[string]any{"pin": "2468"}).status)
	assert.Equal(t, 200, h.call(t, http.MethodPost, "/kiosk/exit", admin, map[string]any{"pin": "13579"}).status)
}
