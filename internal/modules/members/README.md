# members

Owns `members`, `memberships` and `membership_branches`. Enrolling and renewing also issue the
receipt and the payment through `internal/platform/receipts` (shared with day passes).

- **Rules**: `docs/domain/business-rules.md` sections 6 to 10. Status is derived in SQL from the periods and the Lima date; it is never stored.
- **Reads of other modules' tables**: `internal/platform/branchdir` reads `branches` and `gyms` (owned by accounts) with SELECTs only. No Go imports between modules.
- **Logo**: the printable receipt carries `gym.logo_url` (temporary link) through the optional `LogoResolver` port, resolved for the caller's own gym only; null when unavailable.
- **Atomicity**: one transaction per enrollment or renewal (member, period, branches, counter, receipt, payment).
- **Idempotency**: a repeated `id` returns the original sale with 200.
