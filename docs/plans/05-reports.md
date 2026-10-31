# API-05: payments report and dashboard

> **Superseded by [plan 07](07-groups-and-gym-scoped-members.md):** "organization", "owner", "client" and "network" scope in this plan are replaced by group (internal label), no owner, and gym-scoped members; the tenant key is now `gym_id`.

**Depends on:** API-03 and API-04. **Unlocks:** WEB-05. **Rules:** business-rules §9, §17.

## Goal

Read-only endpoints for the payments screen (filters, KPIs, pagination, download) and the dashboard (real figures, no invented data).

## Read first

business-rules §9 and §17; the prototype `atm-pulso-web/src/features/payments` (filters, KPIs, CSV/print download) and
`features/dashboard`. Money is summed in the database (`numeric`), never in Go floats.

## Module

A **`reports`** module with no tables of its own: only SQL read models over `payments`, `memberships`, `members`, `access_events`.
It is the single exception that reads other modules' tables, and only through `SELECT` in its own adapter. Document that in the module README and
add an `archtest` exception limited to this module.

## Endpoints

`GET /payments?from=&to=&branch_id=&gym_id=&method=&kind=membership|day_pass&q=&page=&page_size=`: payments of the caller's reach
(owner: organization; admin: its gyms; reception: assigned branches), `from`/`to` are Lima dates (default: current month), newest first.
```json
{ "data": [ { "id": "...", "paid_on": "2026-10-27", "receipt_number": "R-000012", "kind": "membership", "amount": "270.00", "method": "cash",
   "member": { "id": "...", "full_name": "..." }, "branch": { "id": "...", "name": "Miraflores" } } ],
  "meta": { "page": 1, "page_size": 25, "total": 40,
            "totals": { "amount": "5400.00", "count": 40, "by_method": { "cash": "3000.00", "yape_plin": "1500.00", "card": "900.00", "transfer": "0.00" } } } }
```
Day pass rows have `member: null` (or the optional visitor name). `totals` cover the whole filter, not just the page.

`GET /payments/export?...same filters` returns `text/csv` (UTF-8 with BOM, `;` or `,` per Q-CSV), same columns, no pagination, up to 10,000 rows else
`413 EXPORT_TOO_LARGE`. Cells starting with `= + - @` are prefixed with `'` (CSV injection).

`GET /dashboard?branch_id=&gym_id=`: figures for today and the month:
```json
{ "data": { "date": "2026-10-27",
  "members": { "active": 120, "expiring": 14, "expired": 9, "scheduled": 2 },
  "income": { "today": "540.00", "month": "9800.00" },
  "access": { "allowed_today": 80, "denied_today": 5 },
  "expiring_soon": [ { "member_id": "...", "full_name": "...", "ends_on": "2026-10-30", "days_remaining": 3, "phone": "999..." } ] } }
```
`expiring_soon` is capped at 10, nearest first. No sample or default numbers: an empty organization returns zeros.

Error codes: `VALIDATION_FAILED` (bad range, `from > to`, range over 366 days), `EXPORT_TOO_LARGE`.

## Tests required

- Totals equal the sum of rows; `by_method` adds up to the amount; filters combine; Lima day boundaries (a payment at 23:30 Lima belongs to that day).
- Reach per role and tenant isolation; superadmin read-only inspection with the header.
- Export: header row, escaping of commas/quotes/newlines, CSV injection prefix, size limit.
- Dashboard on an empty organization (all zeros) and on a seeded fixture with known results.
- Performance guard: `EXPLAIN` of the main queries uses the indexes from data-model (document in the PR).

## Security checklist

- [ ] Every query is filtered by `organization_id` first, then by reach.
- [ ] Export is rate limited like login (it is heavy).

## Questions for the owner

| id | Question | Default |
|---|---|---|
| Q-CSV | Separator and format for the download. | Comma, UTF-8 BOM (opens in Excel). |
| Q-PDF | Does "Descargar" also need a PDF? | No; CSV only, the web prints the receipt itself. |

## Done when

docs/README.md's definition of done; WEB-05 can be built from `openapi.yaml`.

## Completion — 2026-10-27

- Implemented module `reports` (read-only, no tables): `GET /payments` with filters (`from`, `to`, `branch_id`, `gym_id`, `method`, `kind`, `q`) and `meta.totals` of the whole filter,
  `GET /payments/export` (CSV: BOM, comma, formula prefix, 10,000-row limit, throttled like login), `GET /dashboard`. All in `openapi.yaml`. `apierror.Meta` gained `counts` and `totals`.
- Decisions where documents disagreed (business-rules wins): `expiring_soon` has five members (not ten); reception gets the list **without** totals and gets 403 on the export and the dashboard;
  `by_method` always lists the four methods with `"0.00"`.
- Verified: money is summed in SQL (`numeric(14,2)`, found and fixed `"0"` vs `"0.00"`); Lima day boundary unit test; export size limit unit test; role matrix and tenant isolation; dashboard zeros for an empty organization.
- Gaps: no `EXPLAIN` review with generated volume yet (API-06).
