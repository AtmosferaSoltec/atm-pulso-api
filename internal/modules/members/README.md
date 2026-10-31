# members

Owns `members`, `memberships` and `membership_branches`. Enrolling and renewing also issue the
receipt and the payment through `internal/platform/receipts` (shared with day passes).

- **Rules**: `docs/domain/business-rules.md` sections 6 to 10. Status is derived in SQL from the periods and the Lima date; it is never stored.
- **Reads of other modules' tables**: `internal/platform/branchdir` reads `branches` and `gyms` (owned by accounts) with SELECTs only. No Go imports between modules.
- **Logo**: the printable receipt carries `gym.logo_url` (temporary link) through the optional `LogoResolver` port, resolved for the caller's own gym only; null when unavailable.
- **Atomicity**: one transaction per enrollment or renewal (member, period, branches, counter, receipt, payment).
- **Idempotency**: a repeated `id` returns the original sale with 200.

## Same-plan rule and corrections (business-rules.md section 7c)
- Buying a plan the member already holds is a renewal that starts where the plan's last period ends; periods of the same plan never overlap
  (half-open `[starts_on, ends_on)`; `409 MEMBERSHIP_OVERLAP`). Checked under the member row lock (`checkOverlap`).
- `PATCH /members/{id}/memberships/{membership_id}` corrects months, start, amount and method of a period that has not ended: the end is recalculated, the payment and receipt are
  corrected in place and a row is added to `membership_amendments` (`GET .../amendments`, admin). Errors: `MEMBERSHIP_NOT_FOUND`, `MEMBERSHIP_ENDED`, `MEMBERSHIP_OVERLAP`.
