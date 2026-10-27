# Release notes: API-02 to API-06

Hand-over for whoever deploys. Nothing here was pushed or deployed: everything was built and tested locally.

## What was built

| Plan | Result |
|---|---|
| API-02 | Catalog, platform clients and owner management, gym settings, team (create, edit scope, activate, deactivate, correct DNI, reset password), plan limits and agreements |
| API-03 | Members, enrollment, renewals, numbered receipts (`R-000001`) and payments, status derived in SQL |
| API-04 | Access checks (server decision, always logged), history, summary, day passes (`D-000001`) |
| API-05 | Payments report with totals, CSV export, dashboard |
| API-06 | Contract test (code = `openapi.yaml`), security headers, migration and volume checks, owner-journey test |

The API has 39 operations over 33 paths; `docs/openapi.yaml` is the contract the web generates its types from.

## Migrations to apply (in order, automatic with `api migrate up`)

| # | Name | Notes |
|---|---|---|
| 00003 | `accounts_require_password_change` | `must_change_password`; existing users keep `false` |
| 00004 | `accounts_tenant_constraints` | `organization_id` on branches and user_branches, composite FKs, `plan_agreements` |
| 00005 | `members_create_members_and_payments` | members, memberships, membership_branches, receipts, receipt_counters, payments |
| 00006 | `access_create_entries_and_day_passes` | day_passes, access_entries, FK from payments |

Verified on an empty database (up, down all six, up again) and from the production state (version 2 with one
superadmin inserted by hand: it survives and ends at version 6). The `down` of each migration works.

## Environment variables

**None added.** `.env.example` is unchanged. No new secrets are needed on the server.

## Deployer checklist

1. Merge to `main` (CI runs format, vet, lint and the tests against PostgreSQL), then push `production`.
2. The compose `migrate` step applies 00003 to 00006. Watch it finish before the app starts.
3. The first superadmin does not exist in production yet: insert it by hand with `go run ./scripts/first-superadmin`
   piped to `psql` (see README). Nothing seeds data.
4. Smoke test: login, `GET /me`, `GET /catalog` (with the superadmin: `X-Organization-Id` is required, so check `GET /clients`), then create a test client and delete nothing in production.
5. Headers to expect on every response: `X-Content-Type-Options: nosniff`, `Cache-Control: no-store`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`.

## Verified

`gofmt`, `go vet`, `golangci-lint` (0 issues) and `go test ./... -race -count=1` against PostgreSQL; the test database ends with 0 rows.
Highlights: gapless receipt numbers under concurrency (`R-` and `D-`), atomic enrollment and day pass sales, role matrix and tenant isolation on every
endpoint, superadmin read-only inspection, password onboarding, and a full journey (superadmin → owner → reception → member → access → day pass → reports).

Volume check (20,000 members, 40,000 memberships, 200,000 payments, PostgreSQL 17):

| Query | Time |
|---|---|
| Members list, first page / page 800 / by status | 77 / 58 / 52 ms (was ~980 ms before paginating first) |
| Members search by name or document | 15 ms |
| Payments page, 30 days / 365 days | 34 / 105 ms |
| Payments totals, 30 days / 365 days | 20 / 48 ms |
| CSV export, 10,001 rows | ~850 ms |
| Dashboard | 61 ms |

No extra index was needed beyond those in the migrations.

## Decisions taken where documents disagreed

business-rules.md won over the plans:

- An unknown document at the door is a logged `200` denial (`NOT_REGISTERED`); only an unknown `member_id` is 404.
- The day pass price is the gym's; a client-sent amount is accepted only when it equals it.
- The dashboard lists five members about to expire (not ten); reception gets payments without totals, and 403 on the export and the dashboard.
- A new user's password is their DNI and must be changed at first login; a superior may correct a DNI.

## Known limits and open points

- Gym logos are deferred (Cloudflare R2 later). The web keeps the monogram and the colour.
- Access entries are kept forever (no retention policy decided).
- Rate limits are per IP and in memory (one replica); the CSV download shares the login's stricter limit.
- Memory: 256 MB container; password hashing runs at most 4 at a time.
- The web is not connected yet: see `atm-pulso-web/docs/plans/`.
