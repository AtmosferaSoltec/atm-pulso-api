# vouchers module

Owns the **proofs of payment**: a photo or PDF of the transfer, Yape/Plin or card receipt, attached
to the money it proves: the payment of a receipt (memberships, day passes), a **product sale paid on the
spot** or a **collection of pending product sales** (module `sales`, which never writes payments). Optional
(cash needs none), up to five per payment / sale / collection (one payment may be split in several transfers)
and **immutable**: no edit, no delete, because they are evidence for later claims.

## Tables (it owns them)
- `payment_vouchers` (migration 00011): key and metadata only; files under `vouchers/<gym>/<payment>/<id>.<ext>`.
  Composite FK to `payments`.
- `sale_vouchers` (migration 00025): one table for both kinds of sales money, with exactly one of `sale_id` or
  `collection_id` (CHECK), composite FKs `(sale_id, gym_id)` -> `sales`, `(collection_id, gym_id)` -> `sale_collections`
  and `(uploaded_by, gym_id)` -> `users`, so a voucher can never belong to another gym's sale. Files under
  `vouchers/<gym>/sales/<sale>/<id>.<ext>` and `vouchers/<gym>/collections/<collection>/<id>.<ext>`.

Why a second table and not a generalized `payment_vouchers`: the existing table and its FK to `payments` stay
untouched (no data migration), and each owner keeps a real composite FK instead of a polymorphic id.
The tables it proves (`payments`, `sales`, `sale_collections`) belong to other modules; this module only reads them.

## Endpoints
`POST /receipts/{id}/vouchers` (the file as the raw body: PNG, JPEG, WebP or PDF, up to 10 MB, checked
by content), `GET /receipts/{id}/vouchers`, `GET /vouchers/{id}/file`. They are addressed by the receipt
because that is what the sale returns and what the payments list shows. Reach is the reach of the branch
where the payment was collected (admin: the current gym, reception: their branches). The file endpoint needs the token, so the web
fetches it and shows the bytes: it is never a public link.

Product sales (behind `RequireFeature(sales)`, `403 FEATURE_DISABLED`): `POST`/`GET /sales/{id}/vouchers` for a sale
paid on the spot and `POST`/`GET /sales/collections/{id}/vouchers` for a collection. Same body, rules, limit and
errors as above; reach is the branch of the sale or of the collection. Uploading to an open pending sale answers
`409 VOUCHER_SALE_NOT_PAID`, and to a pending sale already collected `409 VOUCHER_SALE_COLLECTED` (its proof belongs to
the collection); listing either answers an empty list. Their files are served by the same `GET /vouchers/{id}/file`,
which also asks for the sales module for these.

`BodyLimits(prefix)` gives every upload route its 10 MB body limit (used by `cmd/api` and the tests).
`Module.SaleVoucherCounts` is handed to the sales module (its `Vouchers` port, wired in `main.go`) for the
`voucher_count` of its sale rows.

## Rules
- Cash payments refuse vouchers (`VOUCHER_NOT_FOR_CASH`); other types or sizes: `VOUCHER_TYPE_INVALID`,
  `VOUCHER_SIZE_INVALID`.
- The limit of five is enforced under a row lock on the payment, so concurrent uploads cannot beat it
  (`409 VOUCHER_LIMIT_REACHED`). A file whose row was refused is deleted from the bucket; if that delete
  fails the orphan is only logged.
- Without the R2 variables (development) uploads and downloads answer `503 STORAGE_UNAVAILABLE`.
- `GET /payments` reports `voucher_count` per payment; the sales lists report it per sale (the collection's for a
  sale collected later).

## Errors
| code | status | when |
|---|---|---|
| `VALIDATION_FAILED` | 400 | cash (field `receipt`, `sale` or `collection`: `VOUCHER_NOT_FOR_CASH`), wrong file type, empty or too large file (field `voucher`) |
| `VOUCHER_LIMIT_REACHED` | 409 | the payment, sale or collection already has five |
| `VOUCHER_SALE_NOT_PAID` | 409 | the sale is still pending |
| `VOUCHER_SALE_COLLECTED` | 409 | the sale was paid by a collection: attach to the collection |
| `FEATURE_DISABLED` | 403 | sale/collection vouchers (and their files) in a gym without the sales module |
| `NOT_FOUND` | 404 | no such receipt/sale/collection/voucher in the caller's reach |
| `STORAGE_UNAVAILABLE` | 503 | no object store configured |
