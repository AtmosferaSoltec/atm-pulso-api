# vouchers module

Owns the **proofs of payment**: a photo or PDF of the transfer, Yape/Plin or card receipt, attached
to the payment of a receipt. Optional (cash needs none), up to five per payment (one payment may be
split in several transfers) and **immutable**: no edit, no delete, because they are evidence for later
claims.

## Table (it owns it)
`payment_vouchers` (key and metadata only; the files live in the private R2 bucket under
`vouchers/<organization>/<payment>/<id>.<ext>`). It references `payments` with a composite foreign key,
so a voucher can never belong to another organization's payment. The `payments` table itself belongs to
the sales modules (written through `platform/receipts`); this module only reads it.

## Endpoints
`POST /receipts/{id}/vouchers` (the file as the raw body: PNG, JPEG, WebP or PDF, up to 10 MB, checked
by content), `GET /receipts/{id}/vouchers`, `GET /vouchers/{id}/file`. They are addressed by the receipt
because that is what the sale returns and what the payments list shows. Reach is the reach of the branch
where the payment was collected (admin: the current gym, reception: their branches). The file endpoint needs the token, so the web
fetches it and shows the bytes: it is never a public link.

## Rules
- Cash payments refuse vouchers (`VOUCHER_NOT_FOR_CASH`); other types or sizes: `VOUCHER_TYPE_INVALID`,
  `VOUCHER_SIZE_INVALID`.
- The limit of five is enforced under a row lock on the payment, so concurrent uploads cannot beat it
  (`409 VOUCHER_LIMIT_REACHED`). A file whose row was refused is deleted from the bucket; if that delete
  fails the orphan is only logged.
- Without the R2 variables (development) uploads and downloads answer `503 STORAGE_UNAVAILABLE`.
- `GET /payments` reports `voucher_count` per payment.

## Errors
| code | status | when |
|---|---|---|
| `VALIDATION_FAILED` | 400 | cash payment, wrong file type, empty or too large file |
| `VOUCHER_LIMIT_REACHED` | 409 | the payment already has five |
| `NOT_FOUND` | 404 | no such receipt/voucher in the caller's reach |
| `STORAGE_UNAVAILABLE` | 503 | no object store configured |
