# kiosk module

Settings of the **access tablet** (kiosk mode, plan 08). The screen itself lives in pulso-web (`/kiosk`) and
uses the existing `POST /access/checks`; this module only owns the **exit PIN**.

## Table (it owns it)
`kiosk_settings` (one row per gym): `pin_hash` (bcrypt, salted), `updated_by`, `updated_at`. The PIN is never stored
nor returned.

## Endpoints (all behind `auth.RequireFeature(kiosk)` -> `403 FEATURE_DISABLED`)
- `GET /kiosk/settings` (admin, reception): `{ "pin_set": bool }`. The kiosk refuses to start without a PIN.
- `PUT /kiosk/pin` (admin only): `{ "pin": "1234" }`, 4 to 8 digits (`400 VALIDATION_FAILED`, field code `PIN_INVALID`).
- `POST /kiosk/exit` (admin, reception): `{ "pin": "1234" }`. `200 {"data":{"ok":true}}` when it matches. `PUT /kiosk/pin` answers `200 {"data":{"pin_set":true}}`.

## Rules and errors
- Wrong PIN `403 KIOSK_PIN_INVALID`; no PIN yet `409 KIOSK_PIN_NOT_SET`.
- Throttle: 5 wrong PINs in a row lock that user for 5 minutes (`429 KIOSK_PIN_LOCKED`, even for the right PIN). In memory
  (one replica, like the IP rate limiter); a restart clears it.
- Why server side: the admin sets it once for every tablet of the gym, it survives a cleared browser, and a
  wrong-PIN counter exists. A hash kept in the browser could be read or replaced by anyone using the tablet.
- Phase 2 (pending): registered devices with their own credential instead of a fixed receptionist session.
