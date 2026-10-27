# API-02: clients, gyms, branches and team

**Depends on:** API-01 (done). **Unlocks:** WEB-02, API-03. **Rules:** business-rules §2, §3, §4, §5, §14, §15, §16.

## Goal

Everything needed to create and manage who is who: the superadmin creates clients (an owner plus its gyms and
branches, with the free-plan limits), owners and admins manage the team and each gym's settings, and every
signed-in user can read the catalog of their organization. After this plan the web can run its whole
account side on the real API.

## Read first

`docs/domain/business-rules.md` (all of §2, 3, 4, 5, 14, 15, 16), `docs/domain/data-model.md` (API-02 section),
`docs/testing.md`, the `accounts` module README, and the prototype: `atm-pulso-web/src/features/platform`,
`features/team`, `features/organization`, `features/branding`, `features/auth/services/permissions.ts`.

## Where the code goes

Keep it in the **`accounts` module** (it owns `organizations`, `gyms`, `branches`, `users`, `user_branches`). Add
use cases in separate files (`app/clients.go`, `app/team.go`, `app/gyms.go`, `app/catalog.go`) and handlers in
`adapters/http`. Do not create modules that write these tables.

**Cross-module reads.** The next modules (members, payments...) need to ask "is this branch of my organization
and may this user reach it?" and "does this gym allow branch sharing?". Modules never import each other, so:
define a **small interface in the consumer** (using only stdlib, `uuid` and `platform/auth` types) and let
`accounts.Module` expose an object that satisfies it structurally; `cmd/api/main.go` wires them. Add that
exposure now (`Module.Branches()` returning something like `BranchInfo(ctx, org, branchID) (auth.BranchRef, gymID,
branchSharing, ok)`), with a test, so API-03 only consumes it.

## Cross-cutting pieces to build first

1. **`platform/tx`**: a transaction runner (`Run(ctx, func(pgx.Tx) error)`), copied in spirit from Orbita's
   `platform/tx`. Business operations that write several rows (create client, enroll...) run in one transaction.
2. **Organization scope middleware** (`platform/auth`): for org-scoped routes, `Principal.OrganizationID` comes
   from the token's user; for a **superadmin** it comes from the `X-Organization-Id` header (must be a UUID of an
   existing organization, else 400/404) and **only on `GET`**; a superadmin write answers `403 FORBIDDEN`.
   Platform routes (`/clients...`) are superadmin-only and ignore the header.
3. **Plan constants** in `accounts/domain/plan.go`: `FreePlan{MaxGyms: 1, MaxBranchesPerGym: 1}` and a pure
   function `Extras(branchCountPerGym []int) (extraGyms, extraBranches int)`, with table tests. Never repeat
   those numbers elsewhere.
4. **Concurrent hashing limit**: argon2id takes ~19 MiB per hash and the container has 256 MB. Add a
   semaphore (4 at a time) around `Hasher.Hash/Verify` so a burst of logins cannot exhaust memory.
5. **Tenancy migration (after password-onboarding migration `00003`)**: `branches.organization_id` with composite FKs, `user_branches.organization_id`, and
   `plan_agreements` (see data-model.md). Production tables are empty, so backfill is trivial; still write the
   `UPDATE` for safety.

## Endpoints

All under `/api/v1`, all need a Bearer token unless stated. Money as text. Errors as in AGENTS.md section 5.

### Catalog (any role; superadmin with `X-Organization-Id`)

`GET /catalog`
```json
{ "data": {
  "organization": { "id": "...", "name": "Grupo Atlas" },
  "gyms": [ { "id": "...", "name": "Atlas Fitness", "display_name": null, "tax_id": "20123456789",
              "brand_color": "#17677b", "day_pass_price": "20.00", "day_pass_price_is_default": false,
              "branch_sharing": true,
              "branches": [ { "id": "...", "name": "Miraflores", "address": "Av. ...", "accessible": true } ] } ] } }
```
`accessible` is `Principal.CanAccessBranch`. `day_pass_price` is always the **effective** price (15.00 when the
gym has none) so a receptionist can show it without being able to change it. Every role of the organization sees
all its gyms and branches (the directory is shared); only `accessible` branches can be worked in.

### Platform (superadmin only; owners and others get 403)

`GET /clients?search=&page=&page_size=`: clients with owner, gyms, branches and plan state; `search` matches
the client name, gym names and RUC (case-insensitive, contains).
```json
{ "data": [ { "id": "...", "name": "Grupo Atlas",
   "owner": { "id": "...", "document_number": "10000002", "full_name": "Andrea Salas", "is_active": true },
   "gyms": [ { "id": "...", "name": "Atlas Fitness", "tax_id": "...", "branches": [ { "id": "...", "name": "Miraflores" } ] } ],
   "plan": { "state": "extras", "extra_gyms": 2, "extra_branches": 0 } } ],
  "meta": { "page": 1, "page_size": 25, "total": 2 } }
```
`plan.state` is `free` when both extras are 0, else `extras`.

`GET /clients/summary`: `{ "clients": 2, "gyms": 4, "branches": 5, "client_users": 4 }` (users of clients = users with an organization).

`POST /clients` creates the organization, its owner and its gyms/branches **in one transaction**:
```json
{ "id": "optional-uuid",
  "owner": { "document_number": "45678912", "first_names": "MARIA ELENA", "last_names": "ROJAS CAMPOS",
             "email": "maria@example.com", "phone": "999111222" },
  "gyms": [ { "name": "Fuerza Uno", "tax_id": "20123456789",
              "branches": [ { "name": "Centro", "address": "Av. Uno 123" } ] } ],
  "agreed_extra": false }
```
Rules: the organization **name is `first_names + " " + last_names`** (normalized spaces); owner DNI valid and unused;
names, email (basic format) and phone (9 digits starting with 9) required; temporary password derived from the owner DNI; at least one gym and each gym
at least one branch; gym and branch names required; `tax_id` optional, 11 digits when present (it may repeat between
gyms). If the request goes beyond the free plan and `agreed_extra` is not `true` → `409 PLAN_LIMIT_EXCEEDED` with
`details: { "extra_gyms": n, "extra_branches": n }`; with `true`, create it and insert one `plan_agreements` row per
extra gym/branch with the superadmin as `agreed_by`. Answer `201` with the same shape as a list item. Optional `id` makes
it idempotent.

`POST /clients/{client_id}/gyms`: `{ "id?", "name", "tax_id?", "branches": [...], "agreed_extra": false }` adds a gym
(same limit and agreement rules, counting the client's existing gyms).

`POST /gyms/{gym_id}/branches`: `{ "id?", "name", "address?", "agreed_extra": false }` adds a branch to any gym
(superadmin only; same rules).

### Gym settings (owner of the gym's organization, or the admin of that gym)

`PATCH /gyms/{gym_id}` with any of:
`display_name` (string 1 to 80, or `null` to clear), `brand_color` (`#rrggbb` lower case), `day_pass_price`
(`"15.00"`, 0 to 1,000,000, or `null` to go back to the default) and `branch_sharing` (bool). Answer the gym as in the
catalog. A gym of another organization → 404; of the same organization but not manageable by the caller → 403; a
superadmin → 403.

### Team (owner and admin; superadmin may inspect reads, reception gets 403)

`GET /users`:
```json
{ "data": [ { "id": "...", "full_name": "Carlos Medina", "document_number": "10000005", "phone": null,
   "role": "admin", "is_active": true, "is_self": false,
   "gym": { "id": "...", "name": "Atlas Fitness" }, "branches": [] } ], "meta": { "...": 1 } }
```
Visibility: owner sees every user of its organization; admin sees itself and the receptionists of its gym (every
branch of the receptionist must belong to the admin's gym). Nobody sees the superadmin.

`POST /users`: `{ "id?", "full_name", "document_number", "role": "admin|receptionist", "gym_id" (admin), "branch_ids" (receptionist) }`.
`PATCH /users/{id}`: `{ full_name?, document_number?, role?, gym_id?, branch_ids? }` (DNI corrections by authorized superiors only; see business-rules §2).
`POST /users/{id}/deactivate`, `POST /users/{id}/activate`, `POST /users/{id}/password` without a caller-selected password
(a manager resets it to the DNI; it bumps `password_version` and requires a password change).

Rules: only an owner may create or change an `admin`; an admin only creates/edits receptionists whose branches are all in
its gym; a receptionist needs 1+ branches; branches and gym must belong to the caller's organization (a foreign id is a
400 `SCOPE_NOT_ALLOWED`... see codes); nobody modifies themselves, an owner (except the superadmin) or a user outside their
reach; the DNI is unique system-wide.

### Error codes added

| code | status | when |
|---|---|---|
| `PLAN_LIMIT_EXCEEDED` | 409 | beyond the free plan without `agreed_extra` (details with the extras) |
| `USER_DOCUMENT_TAKEN` | 409 | the DNI already belongs to a user |
| `ROLE_NOT_ASSIGNABLE` | 403 | an admin trying to create/edit an admin or owner |
| `SCOPE_NOT_ALLOWED` | 400 / 403 | foreign-organization body reference / inaccessible same-organization scope |
| `CANNOT_MODIFY_SELF` | 403 | editing, deactivating or resetting yourself here |
| `CANNOT_MODIFY_OWNER` | 403 | touching an owner as owner/admin |
| `BRANCHES_REQUIRED` | 400 | receptionist without branches (a `VALIDATION_FAILED` field is also fine) |

Keep `VALIDATION_FAILED` with `details: [{field, code}]` for field problems (`DOCUMENT_NUMBER_INVALID`, `FULL_NAME_INVALID`,
`EMAIL_INVALID`, `PHONE_INVALID`, `PASSWORD_TOO_SHORT`, `GYM_REQUIRED`, `BRANCH_REQUIRED`, `NAME_REQUIRED`, `TAX_ID_INVALID`,
`BRAND_COLOR_INVALID`, `PRICE_INVALID`...).

## Tests required (on top of docs/testing.md)

- Plan arithmetic (`Extras`) table test, including the boundary 1 gym / 1 branch = free, 2nd branch of a gym = 1 extra.
- Creating a client: happy path (organization, owner, gyms, branches, `plan_agreements` rows), the 409 without
  `agreed_extra`, the success with it, idempotent repeat with the same `id`, **atomicity** (a failure on the second gym
  leaves nothing behind), owner can log in right after.
- Role matrix for every endpoint (owner/admin/receptionist/superadmin), tenant isolation per endpoint, admin cannot
  touch another gym of the same organization, receptionist cannot be given a foreign branch.
- Team rules: admin cannot create admins; owner can; no self-modification; deactivated user's token dies at once;
  password reset kills old tokens; visibility lists per role.
- Superadmin inspection: `X-Organization-Id` works on `GET /catalog` and `GET /users`... and is **refused on every
  write**; ignored/refused for other roles.
- Database: `branches.organization_id` composite FK rejects a branch whose gym belongs to another organization; a
  receptionist cannot be linked to another client's branch.
- Concurrent logins stay within memory (semaphore test).

## Security checklist

- [ ] No endpoint takes `organization_id` from a body or URL (only the superadmin header, validated).
- [ ] Passwords are never returned or logged; initial passwords only travel in the request body.
- [ ] Foreign resources answer 404, never 403.
- [ ] `agreed_extra` can only be honored for a superadmin.

## Questions for the owner (defaults apply only if told to proceed)

| id | Question | Default |
|---|---|---|
| Q-PASSWORD | Who sets a new user's first password? | The creator types it; no forced change at first login. |
| Q-LOGO | Gyms can upload a logo in the prototype. Where to store it? | **Defer.** Ship without it (the web keeps the monogram and hides the upload). If approved later: `gym_logos` as `bytea` (max 512 KB, PNG/JPEG/WebP checked by magic bytes) served by an authenticated endpoint. |
| Q-DNI-EDIT | May a manager correct a user's DNI? | No. Deactivate and create again. |

## Smoke test (local)

```sh
API=http://localhost:3001/api/v1
TOKEN=$(curl -s $API/auth/login -H 'Content-Type: application/json' -d '{"document_number":"<superadmin dni>","password":"<password>"}' | jq -r .data.access_token)
curl -s -X POST $API/clients -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d @client.json | jq
curl -s $API/clients -H "Authorization: Bearer $TOKEN" | jq '.data[0].plan'
```

## Done when

The definition of done in docs/README.md, plus: WEB-02 can be implemented using only `openapi.yaml`, and the
owner reviewed the summary.

## Implementation review — 2026-10-27

- Changes: reviewed the prerequisite documents and API-02/WEB-01/WEB-02 before implementation. No application code or contract changes yet; no verification claims.
- Gaps: the team endpoint section forbids superadmin reads, while the required inspection test and business-rules §2 allow them. The team rules describe foreign body references as 400 `SCOPE_NOT_ALLOWED`, but the error table assigns that code 403. Resolve these contradictions before implementing the contract.
- Questions for the owner: approve the Q-PASSWORD, Q-LOGO and Q-DNI-EDIT defaults; confirm superadmin inspection of `GET /users`; confirm 400 for foreign-organization body references, 404 for foreign path resources, and 403 for inaccessible same-organization scope.
- Status: API-02 is pending these decisions. Later plans have not started. WEB-01's instruction to retain E2E data conflicts with the owner's explicit cleanup requirement; implementation must use isolated fixtures and remove them after tests.

## Owner decisions — 2026-10-27

- Password onboarding and manager resets follow business-rules §15, replacing Q-PASSWORD's default.
- Superadmin inspection includes team reads. Foreign body references use 400, foreign path resources
  use 404, and unauthorized same-organization scope uses 403.
- Logo uploads are deferred; future image storage is Cloudflare R2. No credentials needed now.
- Authorized superiors may correct user DNI under business-rules §2; self-correction stays forbidden.
- Add platform owner management endpoints for DNI correction and password reset, since the team
  endpoints cannot manage owners. Document and test them in OpenAPI with superadmin-only access.
- Implementation is in progress; no plan is complete yet.

### Password onboarding implementation

Implemented live mandatory-change state, restricted authenticated routes, forced return to login,
and conditional password updates. Added migration `00003` and a real PostgreSQL integration test
covering reset-state login, blocked routes, validation, token invalidation and login with the new
password. Full API tests passed with `-race -count=1`; fixture counts returned to zero. Client and
team creation/reset endpoints are still pending, so API-02 remains incomplete.
