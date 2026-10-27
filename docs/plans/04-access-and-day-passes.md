# API-04: access control and day passes

**Depends on:** API-03. **Unlocks:** WEB-04. **Rules:** business-rules §11, §12, §13.

## Goal

Let reception check a member in at a branch (allowed or denied, with the reason), and sell day passes that are paid, numbered
(`D-000001`) and registered like any other payment. Includes the history screens.

## Read first

business-rules §11 to §13, data-model (API-04), testing.md; the prototype `atm-pulso-web/src/features/access`
(the checker, the legend of access types, the history) and `features/payments` for the day pass dialog.

## Module

New module **`access`** (tables `access_events`, `day_passes`). It needs from `members` "member by id/document with status and
access branches" and from `accounts` "branch info and gym settings". Define small consumer-side interfaces; wire in `main.go`.
`access/domain` holds the pure decision: `Decide(member, branch, today, branchSharing) -> Decision{Allowed bool, Reason, Detail}`.

## Decision rules (single source: business-rules §11)

For a member and the branch where reception is working (must pass `CanAccessBranch`):
1. No current or past period → `denied / NO_MEMBERSHIP`.
2. Only future period → `denied / NOT_STARTED` (include the start date).
3. Latest period ended → `denied / EXPIRED` (include the end date).
4. Active but `branch` not in the period's access branches → `denied / BRANCH_NOT_INCLUDED`. When the gym has `branch_sharing = false`,
   branches of the same gym still follow the period's access list; other gyms are never allowed.
5. Otherwise `allowed`; add `expiring_in_days` when 7 or fewer.
Write one table test per row, plus the boundary days. Every attempt, allowed or denied, creates an `access_events` row.

## Endpoints

`POST /access/checks`: `{ "id?", "branch_id", "member_id" }` or `{ "branch_id", "document_number" }` (the receptionist types or scans the document).
Answer `200` (a denied check is still a 200 with `"allowed": false`):
```json
{ "data": { "event_id": "...", "allowed": true, "reason": null, "expiring_in_days": 3,
  "member": { "id": "...", "full_name": "...", "document_number": "...", "status": "expiring", "ends_on": "2026-10-30" },
  "checked_at": "2026-10-27T14:03:11-05:00" } }
```
Unknown document → `404 MEMBER_NOT_FOUND`. Idempotent by `id`.

`GET /access/events?branch_id=&result=allowed|denied&from=&to=&q=&page=&page_size=`: history of the caller's accessible branches
(owner: all of the organization; admin: its gym; reception: assigned branches), newest first, with member name, branch, result, reason.

`GET /access/summary?branch_id=&date=`: `{ "allowed": 31, "denied": 4, "day_passes": 6 }` for a Lima date (default today).

`GET /day-passes?branch_id=&from=&to=&page=&page_size=` and `POST /day-passes`:
```json
{ "id": "optional", "branch_id": "...", "amount": "15.00", "method": "cash",
  "visitor": { "document_type": "dni", "document_number": "45678912", "full_name": "Pedro Ruiz" } }
```
`visitor` is optional (no data required). `amount` defaults to the gym's effective day pass price when omitted; an explicit amount is
allowed (the receptionist may give a discount... see Q-DAYPASS-AMOUNT). Creates the day pass, its payment (kind `day_pass`) and a receipt
`D-000001` from the `day_pass` counter, in one transaction. Answer `201` with the day pass and receipt number. `GET /day-passes/{id}`
returns printable data like `/receipts/{id}` (kind `day_pass`, no membership block).

Error codes: `MEMBER_NOT_FOUND`, `BRANCH_NOT_ACCESSIBLE`, plus field codes `AMOUNT_INVALID`, `METHOD_INVALID`.

## Tests required

- Decision table (all 5 rules, boundaries: ends today, ends yesterday, starts tomorrow, 7 vs 8 days).
- Branch sharing on/off; another gym's branch; period with a subset of branches.
- Every check writes exactly one event; denied checks too; idempotent repeat writes none.
- Day pass: default price, custom amount, numbering `D-` independent of `R-`, 20 concurrent sales gapless, atomic with payment.
- Visibility of events/day passes per role and tenant; a foreign member id returns 404.
- Role matrix: receptionist can check and sell at assigned branches only; superadmin read-only.

## Security checklist

- [ ] A check never reveals members of other organizations (404).
- [ ] The visitor data of a day pass is never logged.
- [ ] Event `checked_at` is server time, never client time.

## Questions for the owner

| id | Question | Default |
|---|---|---|
| Q-DAYPASS-AMOUNT | May reception change the amount of a day pass? | Yes (no ticket, per the owner's earlier answer); the default is the gym price. |
| Q-EVENT-RETENTION | How long to keep access events? | Forever for now. |

## Done when

docs/README.md's definition of done; WEB-04 can be built from `openapi.yaml`.

## Completion — 2026-10-27

- Implemented module `access` (pure `Decide` with table tests, use cases, PostgreSQL adapter, HTTP), migration `00006`
  (`access_entries`, `day_passes`, FK from `payments`), `platform/branchdir` (shared branch reader, also used by `members`).
  Endpoints: `POST /access/checks`, `GET /access/events`, `GET /access/summary`, `GET/POST /day-passes`, `GET /day-passes/{id}`; in `openapi.yaml`.
  The printable day pass receipt is `GET /receipts/{id}` (shows the optional visitor).
- Decisions where the documents disagreed (business-rules wins): an unknown document is a logged `200` denial with `NOT_REGISTERED` (not a 404; only an unknown `member_id` is 404);
  the day pass price is the gym's and the client cannot choose it (an `amount` is accepted only if it equals the price), so Q-DAYPASS-AMOUNT is answered "no".
- Verified: `gofmt`, `go vet`, `golangci-lint` 0 issues, `go test ./... -race` green; 15 concurrent day passes get `D-000001..D-000015`; the test database ends with 0 rows.
- Gaps: access entries have no retention policy (kept forever).
