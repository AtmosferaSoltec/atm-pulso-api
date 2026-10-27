# API-03: members, enrollment, receipts and payments

**Depends on:** API-02. **Unlocks:** WEB-03, API-04. **Rules:** business-rules §6, §7, §8, §9, §10, §18.

## Goal

Register members, sell and renew memberships, issue numbered receipts and record the payments, with the member
status computed on the server. After this plan the members screen, the enrollment and renewal forms and the printable
receipt can run on the real API.

## Read first

business-rules §6 to §10 and §18, data-model (API-03 section), testing.md; the prototype:
`atm-pulso-web/src/features/members` (`MemberForm`, `EnrollmentFields`, `MemberSheet`, `services/enrollment.ts`,
`services/receipt.ts`, `services/memberData.ts`, `hooks/useMemberFilters.ts`), `features/payments`.

## Module

New module **`members`** (owns members, memberships, membership_branches, receipts, receipt_counters, payments' *creation*).
Reads branch/gym data through the small interface API-02 exposed (`BranchInfo`...), never by importing `accounts`.
Create `members/domain` with the pure rules: `EndDate(start, months)` (calendar months with clamping), `Status(periods, today)`,
`AccessBranches(scope, home, orgBranches)`, document validation per type, plan label, receipt formatting. Test them with tables
(Jan 31 + 1 month, leap years, month boundaries, today on the start/end day, 7-day boundary).
The payments *list and reports* come in API-05, but the `payments` table and its insert belong here (it is created with the receipt).
If it reads better, put receipts and payments in a `payments` module that `members` calls through an interface; keep the
"module owns its tables" rule.

## Endpoints

All need a Bearer token. Superadmin (with `X-Organization-Id`) can read, never write. "Today" is `America/Lima`.

`GET /members?q=&status=&home_branch_id=&page=&page_size=`: the shared directory of the organization (every role sees all
of it). `q` matches the name (case-insensitive contains) or the document (contains). `status` is one of `active`,
`expiring`, `expired`, `scheduled`. Newest first.
```json
{ "data": [ { "id": "...", "document_type": "dni", "document_number": "74829163", "full_name": "Valentina Rodríguez",
   "phone": "999000001", "home_branch": { "id": "...", "name": "Miraflores", "gym_name": "Atlas Fitness" },
   "status": "expiring", "days_remaining": 3, "plan_label": "3 meses", "starts_on": "2026-07-29", "ends_on": "2026-10-29",
   "last_receipt": "R-000001" } ],
  "meta": { "page": 1, "page_size": 25, "total": 8,
            "counts": { "all": 8, "active": 3, "expiring": 4, "expired": 1, "scheduled": 0 } } }
```
`counts` ignore the `status` filter but respect `q` and `home_branch_id`.

`POST /members` enrolls a member, creating **the member, its first membership, the payment and the receipt in one transaction**:
```json
{ "id": "optional-uuid", "branch_id": "...", "document_type": "dni", "document_number": "45678912",
  "full_name": "Andrea García", "phone": "999222333",
  "months": 3, "starts_on": "2026-10-27", "amount": "270.00", "method": "cash",
  "access_scope": "branch", "access_branch_ids": ["..."] }
```
`branch_id` is the branch where it is enrolled and collected: the caller must reach it; it becomes `home_branch_id`.
`starts_on` defaults to today. `access_scope` defaults to `branch`; `access_branch_ids` is optional (defaults to every branch
of the scope), must include the home branch, and must be a subset of the scope's branches and of the organization.
Answer `201`:
```json
{ "data": { "member": { "...": "as in the list" },
            "membership": { "id": "...", "months": 3, "starts_on": "...", "ends_on": "...", "amount": "270.00", "access_scope": "branch" },
            "receipt": { "id": "...", "number": "R-000012" } } }
```
Duplicate document → `409 MEMBER_DOCUMENT_TAKEN` with `details: { "member_id": "..." }`.

`GET /members/{id}`: the member with all periods (`memberships`), access branches (names), status, and the latest receipt summary.

`PATCH /members/{id}`: `{ "full_name", "phone" }` only. The document never changes.

`POST /members/{id}/renewals`: `{ "id?", "branch_id", "months", "starts_on?", "amount", "method", "access_scope?", "access_branch_ids?" }`.
`starts_on` defaults to the latest end date when it is in the future, otherwise today (the server decides; the web only suggests).
`branch_id` is where it is collected and may differ from the home branch. Creates a membership, a payment and a receipt;
answers like the enrollment. The member keeps its `home_branch_id`.

`GET /members/{id}/receipts` lists the member's receipts (number, issued_on, plan label, amount, method). `GET /receipts/{id}`
returns the printable data:
```json
{ "data": { "id": "...", "number": "R-000012", "kind": "membership", "issued_on": "2026-10-27",
  "gym": { "name": "Atlas Fitness", "tax_id": "20123456789", "brand_color": "#17677b" },
  "branch": { "name": "Miraflores", "address": "Av. ..." },
  "member": { "full_name": "...", "document_type": "dni", "document_number": "...", "phone": "999..." },
  "membership": { "months": 3, "plan_label": "3 meses", "starts_on": "...", "ends_on": "...", "access_branches": [ { "id": "...", "label": "Atlas Fitness · Miraflores" } ] },
  "payment": { "amount": "270.00", "method": "cash" } } }
```
`gym.name` is the display name when the gym has one. A receipt of another organization answers 404.

### Error codes added

`MEMBER_DOCUMENT_TAKEN` (409), `MEMBER_NOT_FOUND` (404), `BRANCH_NOT_ACCESSIBLE` (403: the caller cannot work in that branch),
`ACCESS_BRANCHES_INVALID` (400: not a subset / missing the home branch / foreign branch). Field validation codes:
`DOCUMENT_TYPE_INVALID`, `DOCUMENT_NUMBER_INVALID`, `FULL_NAME_INVALID`, `PHONE_INVALID`, `MONTHS_INVALID`, `START_DATE_INVALID`,
`AMOUNT_INVALID`, `METHOD_INVALID`, `ACCESS_SCOPE_INVALID`.

## Implementation notes

- **Receipt number**: inside the enrollment transaction run
  `INSERT INTO receipt_counters (organization_id, kind, last_number) VALUES ($1,'membership',1)
   ON CONFLICT (organization_id, kind) DO UPDATE SET last_number = receipt_counters.last_number + 1 RETURNING last_number`.
  A rolled-back transaction rolls the counter back too: no gaps, no duplicates.
- **Status in SQL** (data-model.md): the list, the counts and the status filter run in the database, with `today` as a parameter.
- **Idempotency**: when `id` is sent and a member/membership with that id already exists in the organization, answer the original
  result with `200` instead of creating again. Same `id` in another organization is a plain conflict (do not reveal it).
- **Money** is parsed from text with a decimal type (`shopspring/decimal` or exact integer cents), never `float64`.
- **Scope of the list**: all members of the organization, no matter the branch of the caller (shared directory).
- Do not log document numbers, names or phones.

## Tests required (on top of docs/testing.md)

- Domain tables: end date, status, access branches, document validation, plan label.
- Enrollment happy path for DNI and CE; creates exactly member + membership + membership_branches + receipt + payment; response shape.
- Duplicate document → 409 with the existing id; the same document in **another** organization is allowed.
- Renewal: start default (future end vs past end), new receipt number, home branch unchanged, collected at another branch.
- Access scope rules: default list per scope, subset validation, home always included, foreign branch refused.
- **Concurrency**: 20 parallel enrollments in one organization produce receipt numbers 1 to 20, unique and gapless; a failed enrollment
  (validation error after allocation attempt) does not burn a number.
- **Atomicity**: force a failure after the member insert (for example an invalid method passing through a unit seam) and assert nothing remains.
- Status/counts: fixed Lima date, members in every state, the 7-day boundary, `counts` ignoring the status filter but not `q`.
- Role matrix and tenant isolation for every endpoint, including `GET /receipts/{id}` and `PATCH /members/{id}` by id from another tenant.
- A receptionist can only enroll in an assigned branch; an admin only in its gym's branches.
- Database: composite FKs refuse a member whose home branch is another client's; `CHECK`s on months, amount and dates.

## Security checklist

- [ ] Every id in the path or body is re-checked against `organization_id`.
- [ ] `branch_id` always passes `Principal.CanAccessBranch` before any write.
- [ ] No personal data in logs; receipts only for the owning organization.

## Questions for the owner

| id | Question | Default |
|---|---|---|
| Q-MEMBER-DELETE | Can a member be deleted? | No: not in scope (history matters). |
| Q-PHONE | Is the phone required for members? | Yes, as in the prototype. |
| Q-LIST-SORT | Default order of the members list. | Newest enrolled first. |

## Done when

docs/README.md's definition of done; WEB-03 can be built from `openapi.yaml`; the owner reviewed the summary.
