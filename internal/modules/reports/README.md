# reports

Read-only, no tables. Reads `payments`, `receipts`, `members`, `memberships`, `day_passes` and
`access_entries` with SELECTs in its own adapter (the one module allowed to look at several others'
tables; the rule is "no Go imports", and this adapter imports none).

- **Rules**: `docs/domain/business-rules.md` sections 10 and 13. Money is summed in the database (`numeric`), dates are Lima dates.
- **Reception** gets the list without `meta.totals`, no download and no dashboard (403).
- **Download** (`GET /payments/export?format=csv|xlsx|pdf`, CSV by default): up to 10,000 rows (`413 EXPORT_TOO_LARGE`). The
  file is rendered by `platform/export` (same columns in CSV and Excel; formula-looking CSV cells prefixed with `'`); the PDF letterhead
  comes from `platform/gymbrand` (never fails because of the logo).
