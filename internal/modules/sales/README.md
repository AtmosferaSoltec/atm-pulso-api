# sales module

Optional module (`auth.FeatureSales`, plan 08 piece 3): the **sales of products** a gym makes at the counter (batidos, agua...), with a short **product catalog**.
A sale may be linked to a member; a sale not collected yet is **pending** and is collected later from the member's file. It is not accounting: no receipts or
R-/D- series, no invoices, no inventory. Rules: `docs/domain/business-rules.md` section 11c.

## Tables (it owns them)
`products`, `sales`, `sale_items`, `sale_collections` (migration 00018), `product_categories` (migration 00024; `products.category_id` is `ON DELETE SET NULL`). Composite FKs `(x_id, gym_id)` everywhere. It never writes `payments`/`receipts`:
sales money is **separate** from membership income. Migration 00020 renamed the module key `products` -> `sales` in `gyms.features`.

## Ports
- `Repository` (postgres adapter): all SQL, one transaction per sale and per collection.
- `Vouchers` (optional): counts of the proofs of payment of sales and collections, from the vouchers module (wired in `main.go`), for `voucher_count`.
- `Directory` = `platform/branchdir` (branches, and whether a gym exists for the admin routes) and `People` = `platform/peopledir` (member existence and names, staff names, and `MemberCards`: contact data of a batch of members, optionally filtered by a `fold_text` search): read-only SELECTs, no imports of other modules.

## Endpoints (all behind `RequireFeature(sales)` -> `403 FEATURE_DISABLED`)
| | Who |
|---|---|
| `GET /products` (`?include_inactive=true` admin only) | admin, reception |
| `POST /products`, `PATCH /products/{id}`, `DELETE /products/{id}` (only never-sold products; otherwise deactivate) | admin |
| `GET /product-categories` | admin, reception |
| `POST /product-categories`, `PATCH`/`DELETE /product-categories/{id}` | admin |
| `GET`/`POST /admin/gyms/{gym_id}/product-categories` (bulk `names`), `PATCH`/`DELETE /admin/gyms/{gym_id}/product-categories/{id}` (no module guard) | platform superadmin (console-web) |
| `POST /sales` (status `paid` or `pending`), `GET /sales` | admin, reception (reception lists its branches) |
| `GET /sales/pending` (members with a balance), `GET /members/{id}/pending-sales`, `GET /members/{id}/sales` (history) | admin, reception |
| `GET /sales/debtors?page=&page_size=&q=` (debtors list, see below) | admin, reception |
| `POST /sales/collections` (collect pending sales) | admin, reception (branch reachable) |
| `GET /sales/report` | admin |
| `GET`/`POST /sales/{id}/vouchers`, `GET`/`POST /sales/collections/{id}/vouchers` (served by the **vouchers** module, same guard) | admin, reception (branch reachable) |
| `GET /sales/export?format=csv\|xlsx\|pdf` (the report's period and branch, one row per sale, up to 10,000: `413 EXPORT_TOO_LARGE`) | admin |

Sale rows (`GET /sales`, `/members/{id}/sales`, `/members/{id}/pending-sales`, `POST /sales`) carry `voucher_count`: the proofs of the sale paid on the spot,
or of its collection when it was collected later; 0 while pending.

## Debtors list (`GET /sales/debtors`)
One row per member with at least one open pending sale: `member {id, full_name, document_type, document_number, phone}`, `balance` (money string),
`pending_count`, `oldest_sold_at` (RFC3339) and `oldest_sold_on` (Lima date). Order: oldest pending sale first, then name, then member id.
Paginated (`meta {page, page_size, total}`, `page_size` default 50, max 100). `q` matches the name ignoring case and accents (`fold_text`) or part of the
document. **Reach: gym-wide for both roles**, the same as `/members/{id}/pending-sales` and `/sales/pending` (a member's balance is the gym's, whatever
branch sold it). No money grand total on purpose (totals live in the report). The aggregate reads only `sales`; member data and the search come from
`peopledir.MemberCards` in one batch (no JOIN to `members`, no N+1); ordering by name and paging happen in the use case (debtors of a gym are few).

## Errors (stable codes)
`FEATURE_DISABLED` 403, `PRODUCT_NOT_FOUND` 404, `PRODUCT_NAME_TAKEN` 409, `PRODUCT_HAS_SALES` 409, `CATEGORY_NOT_FOUND` 404, `CATEGORY_NAME_TAKEN` 409, `SALE_NOT_FOUND` 404, `SALE_NOT_PENDING` 409, `NOTHING_TO_COLLECT` 409, `MEMBER_NOT_FOUND` 404,
`BRANCH_NOT_ACCESSIBLE` 403, `SCOPE_NOT_ALLOWED` 400, `CONFLICT` 409, `VALIDATION_FAILED` with field codes `PRODUCT_NAME_INVALID`, `PRODUCT_PRICE_INVALID`,
`PRODUCT_POSITION_INVALID`, `CATEGORY_INVALID`, `CATEGORY_NAME_INVALID`, `CATEGORY_NAMES_INVALID`, `CATEGORY_POSITION_INVALID`, `ITEMS_INVALID`, `PRODUCT_INVALID`, `TOTAL_TOO_LARGE`, `STATUS_INVALID`, `METHOD_INVALID`, `MEMBER_REQUIRED`, `MEMBER_INVALID`, `DATE_INVALID`,
`DATE_RANGE_INVALID`, `GROUP_BY_INVALID`, `SALE_IDS_INVALID`.

## Concurrency and idempotency
A sale or collection with the same client `id` of the gym answers the stored one (200). Collecting locks the pending sales (`FOR UPDATE`) inside the transaction, so two simultaneous
"collect all" collect once. Prices are read with `FOR SHARE` so a concurrent price edit cannot produce a half-priced sale.
