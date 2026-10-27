# reports

Read-only, no tables. Reads `payments`, `receipts`, `members`, `memberships`, `day_passes` and
`access_entries` with SELECTs in its own adapter (the one module allowed to look at several others'
tables; the rule is "no Go imports", and this adapter imports none).

- **Rules**: `docs/domain/business-rules.md` sections 10 and 13. Money is summed in the database (`numeric`), dates are Lima dates.
- **Reception** gets the list without `meta.totals`, no CSV and no dashboard (403).
- **CSV**: UTF-8 with BOM, comma, up to 10,000 rows (`413 EXPORT_TOO_LARGE`), formula-looking cells prefixed with `'`.
