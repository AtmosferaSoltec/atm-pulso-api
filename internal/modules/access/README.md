# access

Owns `access_entries` and `day_passes`. Day pass sales use `internal/platform/receipts` for the
`D-` numbering and the payment, so they never share a transaction boundary with `members`.

- **Rules**: `docs/domain/business-rules.md` sections 11 and 12. The decision (`domain.Decide`) is pure and tested by table; the server decides, the web only shows the reason code.
- **Every check is logged**, allowed or denied, including unknown documents (`NOT_REGISTERED`).
- **Day pass price**: the gym's price (15.00 by default). A client-sent amount is accepted only when it equals it.
- **Reads of other modules' tables**: members/memberships (SELECT) and `platform/branchdir`.
- Entries and day pass customers are personal data: never log them.
