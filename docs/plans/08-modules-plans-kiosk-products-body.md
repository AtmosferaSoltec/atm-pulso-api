# API-08: modules per gym, membership plans, kiosk, products, body tracking

**State:** pieces 1 (modules + plans, migrations 00014/00015) and 2 (kiosk, migration 00016) implemented locally, waiting for review.
Pieces 3 and 4 are for other agents, in this order, working from this document.
Where this file disagrees with older plans or text, this file wins.

## Why

A real prospect showed that Pulso is multi-gym and **not every gym wants everything**. Two kinds of flexibility:

1. **Automatic simplification**: with one option (one branch, one plan) the UI shows no selector.
2. **Modules switched per gym by the platform superadmin** from console-web: `sales`, `body_tracking`, `kiosk`.

## Piece 1: modules and membership plans (DONE)

### Modules (infrastructure every later piece uses)

- Stored in `gyms.features text[]` (migration 00014). The valid list is **closed in code**:
  `internal/platform/auth/features.go` (`auth.AllFeatures`); adding a module needs no migration.
- Switched by the superadmin only: `PATCH /admin/gyms/{gym_id}/features` with the **complete** list (`{"features": ["kiosk"]}`);
  unknown names are `400 VALIDATION_FAILED` with field code `FEATURES_INVALID`. Repeats are ignored; order is fixed.
- Exposed as `features` of the gym in `GET /me` (`gym.features`), `GET /catalog` (`gyms[0].features`) and `GET /admin/groups`.
- The `Principal` is rebuilt on every request and carries `Features`, so a change shows at the gym's next request.
- **Guard for the next pieces**: `auth.RequireFeature(auth.FeatureSales | auth.FeatureBodyTracking | auth.FeatureKiosk)`
  (gin middleware, mount per route after the authentication group) answers `403 FEATURE_DISABLED`. Also `Principal.HasFeature(f)`
  for code that must branch (e.g. do not write audit rows for a module that is off). Every route of a module MUST use the guard,
  and every module test must assert the 403 with the module off and success with it on.
- **Web**: `useHasFeature('kiosk')` and `useGymFeatures()` (`src/features/gym`), reading the catalog of the current gym.
  Pages of a module: register them in `pageFeatures` (`src/config/pages.ts`, `Page -> GymFeature`); the sidebar and the router
  then hide them. Error text for `FEATURE_DISABLED` already exists in `errorMessages.ts`.
- **Console**: `GymFeaturesField` (a switch per module inside each gym panel of the Pulso section).

### Membership plans

- Tables `membership_plans` (gym, name unique per gym ignoring case, optional description, `is_active`, `position`) and
  `membership_plan_prices` (plan, months 1..24, amount). Migration 00015 replaced `gyms.membership_prices`: every existing gym
  got a plan "Membresía" carrying its old list; every period got `plan_id` + `plan_name` (a **snapshot**, so old receipts never change).
- Plans are never deleted, only deactivated. Admin creates/edits/deactivates (`POST /membership-plans`, `PATCH /membership-plans/{id}`,
  prices replace the whole list); reception only reads (`GET /membership-plans`, active ones only).
- Sales (`POST /members`, `POST /members/{id}/renewals`) accept `plan_id`. Optional when the gym has exactly one active plan (that one is
  used); a gym with no plans at all gets "Membresía" created on its first sale; with several active plans it is required
  (`400 PLAN_REQUIRED`); not an active plan of the gym: `400 PLAN_INVALID`. The amount stays free (the plan price is a suggestion).
- A **renewal continues the same plan**: its default start is the end of the member's last period of that plan.
- A plan does **not** restrict access: entry is allowed when any current period opens the branch.
- Status of a member and the coverage rule are in business-rules.md section 8 (SQL in `platform/memberstatus`, Go in
  `members/domain.CoverageEnd`, tested in both).
- Web: "Planes y tarifas" (admin; replaces Tarifas, also holds the day pass price), plan selector in enrollment/renewal only
  with more than one active plan, plan names in the member list, the receipt chips and the receipt itself.

## Piece 2: kiosk mode (module `kiosk`) (DONE, phase 1)

- Lives **inside pulso-web**, a separate route, full screen: large numeric keypad for the document, auto-reset after 5 s, shows only
  the **first name + status + days remaining**, an exit PIN. Nothing else about the member (no phone, no document echo).
- Phase 1: the kiosk runs with the session of an account **pinned to one branch** (a receptionist with a single branch). It calls the
  existing `POST /access/checks`; if a narrower response is wanted for the kiosk, add a module-guarded endpoint
  (`auth.RequireFeature(auth.FeatureKiosk)`) that returns only first name, status, days remaining; never return more than the screen shows.
- Exit PIN: stored hashed per gym (new table owned by the new module, or a column in `gyms` through an accounts port), set by the admin.
  Rate limit wrong PINs. Phase 2 (not now): registered devices with their own credential.
- Fingerprint readers are out of scope.

### Piece 2 as built (phase 1)
- Web: route `/kiosk` (`src/features/kiosk`), page "Quiosco" in settings (open the kiosk, branch of the device, admin sets the PIN).
  Uses the existing `POST /access/checks`; no narrower endpoint was added (the web keeps only first name, status and days).
- API: module `internal/modules/kiosk`, table `kiosk_settings` (bcrypt hash per gym), `GET /kiosk/settings`, `PUT /kiosk/pin` (admin),
  `POST /kiosk/exit`; all behind `RequireFeature(kiosk)`. Wrong-PIN lockout: 5 tries, 5 minutes, per user, in memory.
- Rate limit: the per-IP limiter of the API (429 `TOO_MANY_REQUESTS`) already covers `/access/checks`; the kiosk shows a plain notice.
- Tests: Go integration `kiosk_test.go` + unit; web unit `tests/kioskScreen.test.mjs`, e2e `tests/kiosk.spec.ts` (mocked API), live
  `tests/live/kiosk.live.ts` against the local stack (`playwright.live.config.ts`).
- **Phase 2 (pending)**: registered devices with their own credential (device token bound to a branch, revocable by the admin) instead
  of a shared receptionist session; a narrower module-guarded check endpoint; PIN rate limit shared across replicas.

- Web: add `kiosk` to `pageFeatures` or mount the route only when `useHasFeature('kiosk')`.

## Piece 3: sales of products (module `sales`) (DONE locally, migrations 00018 and 00020; see `internal/modules/sales/README.md` and business-rules 11c)

- Short catalog per gym: name, price, active. Quick sale with optional member. **No receipts or R-/D- series, no inventory.**
- A sale not collected yet is `pending` (requires a member); later any admin/receptionist collects it from the member's file ("Cobrar", with payment method).
- A module `sales` (the catalog is part of it) with its own tables (`gym_id` + composite FKs like everything else), routes behind `auth.RequireFeature(auth.FeatureSales)`.
- Sales report **separate** from membership income (do not add product sales to `payments` totals or the dashboard income).
- A member reference needs a port wired in `main.go` (modules never import each other).

## Piece 4: measurements and routines (module `body_tracking`) (DONE locally, migration 00019; see business-rules 11d and the module README)

- Dated records (weight, height, notes) and routines as dated text.
- **Health data**: visible to admin and reception only when the module is on; **audit log** of who read or wrote (table with actor,
  member, action, time; ids only, never the values). Never log values.
- Routes behind `auth.RequireFeature(auth.FeatureBodyTracking)`; the audit write must be in the same transaction as the read/write it records.

## Tests added by piece 1

Module switching and exposure (roles, validation, isolation between gyms, immediate effect), the guard through the real authentication,
plans (roles, validation, names, isolation, order, deactivation), plan choice in sales, snapshot of the plan name on receipts, several
plans at once (status, access, renewals), coverage rule (unit and integration), dashboard counts, and the migration from the old
price list run in a throwaway schema (up, checks, down).
