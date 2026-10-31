# sales module

Optional module (`auth.FeatureSales`, plan 08 piece 3): the **sales of products** a gym makes at the counter (batidos, agua...), with a short **product catalog**.
A sale may be linked to a member; a sale not collected yet is **pending** and is collected later from the member's file. It is not accounting: no receipts or
R-/D- series, no invoices, no inventory. Rules: `docs/domain/business-rules.md` section 11c.

## Tables (it owns them)
`products`, `sales`, `sale_items`, `sale_collections` (migration 00018). Composite FKs `(x_id, gym_id)` everywhere. It never writes `payments`/`receipts`:
sales money is **separate** from membership income. Migration 00020 renamed the module key `products` -> `sales` in `gyms.features`.

## Ports
- `Repository` (postgres adapter): all SQL, one transaction per sale and per collection.
- `Directory` = `platform/branchdir` (branches) and `People` = `platform/peopledir` (member existence and names, staff names): read-only SELECTs, no imports of other modules.

## Endpoints (all behind `RequireFeature(sales)` -> `403 FEATURE_DISABLED`)
| | Who |
|---|---|
| `GET /products` (`?include_inactive=true` admin only) | admin, reception |
| `POST /products`, `PATCH /products/{id}` | admin |
| `POST /sales` (status `paid` or `pending`), `GET /sales` | admin, reception (reception lists its branches) |
| `GET /sales/pending` (members with a balance), `GET /members/{id}/pending-sales`, `GET /members/{id}/sales` (history) | admin, reception |
| `POST /sales/collections` (collect pending sales) | admin, reception (branch reachable) |
| `GET /sales/report` | admin |

## Errors (stable codes)
`FEATURE_DISABLED` 403, `PRODUCT_NOT_FOUND` 404, `PRODUCT_NAME_TAKEN` 409, `SALE_NOT_FOUND` 404, `SALE_NOT_PENDING` 409, `NOTHING_TO_COLLECT` 409, `MEMBER_NOT_FOUND` 404,
`BRANCH_NOT_ACCESSIBLE` 403, `SCOPE_NOT_ALLOWED` 400, `CONFLICT` 409, `VALIDATION_FAILED` with field codes `PRODUCT_NAME_INVALID`, `PRODUCT_PRICE_INVALID`,
`PRODUCT_POSITION_INVALID`, `ITEMS_INVALID`, `PRODUCT_INVALID`, `TOTAL_TOO_LARGE`, `STATUS_INVALID`, `METHOD_INVALID`, `MEMBER_REQUIRED`, `MEMBER_INVALID`, `DATE_INVALID`,
`DATE_RANGE_INVALID`, `GROUP_BY_INVALID`, `SALE_IDS_INVALID`.

## Concurrency and idempotency
A sale or collection with the same client `id` of the gym answers the stored one (200). Collecting locks the pending sales (`FOR UPDATE`) inside the transaction, so two simultaneous
"collect all" collect once. Prices are read with `FOR SHARE` so a concurrent price edit cannot produce a half-priced sale.
