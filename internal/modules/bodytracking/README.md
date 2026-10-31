# bodytracking module

Optional module `body_tracking` (plan 08, piece 4): **dated body measurements and routines of a member**, plus the access log of those
**sensitive health data** (Peruvian law 29733; rules in business-rules.md section 11d).

## Tables (it owns them; migration 00019)
`body_measurements`, `body_routines` (soft delete: `deleted_by`, `deleted_at`) and `body_access_log` (append-only, ids only, never values).
It reads `members` (gym and home branch) with one SELECT; it writes nothing else.

## Endpoints (all behind `auth.RequireFeature(body_tracking)` -> `403 FEATURE_DISABLED`; admin and receptionist)
- `GET|POST /members/{id}/measurements`, `PUT|DELETE /members/{id}/measurements/{record_id}`
- `GET|POST /members/{id}/routines`, `PUT|DELETE /members/{id}/routines/{record_id}`
- `GET /members/{id}/body-access-log` (admin only)

`PUT` replaces the whole record. `POST` is idempotent by the client `id`. Details and field codes: docs/openapi.yaml.

## Rules
- The member must belong to the token's gym (else 404 `MEMBER_NOT_FOUND`, also for a sibling gym); a receptionist needs access to the member's
  home branch (403 `BRANCH_NOT_ACCESSIBLE`).
- Every list read and every write appends its access-log row in the same transaction. Failed requests (validation, not found) log nothing.
- The data is never exposed by member lists/detail, reports or CSV; the log never stores values; nothing here is written to the application logs.
- Pure rules (ranges, one-decimal parsing, dates, text cleaning) live in `domain` and are unit-tested; HTTP/roles/isolation/audit in
  `internal/integration/bodytracking_test.go`.
