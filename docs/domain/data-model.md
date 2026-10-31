# Data model

What exists today and what each plan adds. PostgreSQL, schema `pulso`, role `pulso_app`. Names are
English `snake_case`, plural tables, `TIMESTAMPTZ` in UTC, UUID v7 ids, `NULL` instead of empty
strings, money `NUMERIC(14,2)`.

**Production already has migrations `00001` to `00012` applied. Never edit them.** All changes are new
files `000NN_<module>_<what>.sql` with `Up` and `Down`.

## Tenant safety, by design

Every business table carries `gym_id` (the **tenant**: a gym is a brand, see business rules §1). To let the
**database itself** refuse cross-tenant links, parent tables expose `UNIQUE (id, gym_id)` and children use a
**composite foreign key** `(parent_id, gym_id) REFERENCES parent (id, gym_id)`. Follow it for every new link.
Tests must prove that inserting a cross-gym link fails (see `TestDatabaseEnforcesTenancyAndRoleRules`).
The **group** is not a tenant key: no business table references it, only `gyms.group_id` does.

> Replaced by migration 00013: until 00012 the tenant key was `organization_id` (the whole customer) and the
> member directory was shared by all gyms of an organization. Organizations are now **groups** (an internal
> label of the superadmin) and the tenant key is the gym. The text of "API-02 adds" and "API-03 adds"
> below describes the history with the old names; read `organization_id` as `gym_id` there, and every
> `organizations` mention as `groups`.

## Existing (after migration 00013, groups and gym-scoped members)

```
groups        (id, name, created_at, updated_at)                           -- internal label chosen by the superadmin; not a user, no owner
gyms          (id, group_id -> groups ON DELETE RESTRICT, name, display_name, tax_id, brand_color, day_pass_price,
               branch_sharing, membership_prices, auth_tenant_id UNIQUE, auth_client_id, created_at, updated_at)
                                                                -- an auth Company with Pulso enabled; auth_client_id is its auth Client
branches      (id, gym_id -> gyms, name, address, created_at, updated_at, UNIQUE (id, gym_id))
users         (id, gym_id -> gyms, auth_user_id, document_number, full_name, phone, role, is_active, created_at, updated_at,
               UNIQUE (gym_id, auth_user_id), UNIQUE (gym_id, document_number), UNIQUE (id, gym_id), role IN ('admin','receptionist'))
user_branches (user_id, branch_id, gym_id)                      -- a receptionist's branches, composite FKs on gym_id
members       (id, gym_id, home_branch_id, document_type, document_number, full_name, phone, auth_user_id NULL, ...,
               UNIQUE (gym_id, document_number), UNIQUE (gym_id, auth_user_id) WHERE auth_user_id IS NOT NULL)
memberships   (..., gym_id, access_scope IN ('branch','gym'))   -- 'network' removed
receipt_counters (gym_id, kind, last_number)  receipts (gym_id, kind, number, UNIQUE (gym_id, kind, number))   -- series per gym
payments, day_passes, access_entries, membership_branches, payment_vouchers: `gym_id` instead of `organization_id`
```

`gyms.tax_id` is the RUC; it is NULL when the company has none, and may repeat between gyms.
`gyms.day_pass_price` NULL means "default 15.00". `members.auth_user_id` is reserved for the future `member`
role (a person of atm-auth-api who sees their own data); nothing reads or writes it yet.

### Superseded: existing shape until migration 00012

```
organizations (id, name, auth_client_id UNIQUE, ...) gyms (id, organization_id, ...) branches/users/user_branches with organization_id
```

## API-02 adds

- ~~`users.must_change_password`~~: dropped by migration 00012 with every other password column (atm-auth-api owns passwords).


- **`branches.organization_id`** (NOT NULL, backfilled from the gym; production is empty so it is trivial),
  `UNIQUE (id, organization_id)` and `FOREIGN KEY (gym_id, organization_id) REFERENCES gyms (id, organization_id)`.
  Every later table links to branches with `(branch_id, organization_id)`.
- **`user_branches.organization_id`** with composite FKs to `users` and `branches`, so a receptionist can never
  be linked to a branch of another client.
- ~~`plan_agreements`~~: dropped by migration 00009 (the product has no plans or limits).
- ~~`gyms.logo_key`~~: dropped by migration 00012; the logo comes from the company profile in atm-auth-api (`logo_file_id`) and atm-storage-api, resolved at request time, never stored here.

## API-03 adds

```
members          (id, organization_id, home_branch_id, document_type 'dni'|'ce', document_number, full_name, phone,
                  created_at, updated_at, UNIQUE (organization_id, document_number), UNIQUE (id, organization_id),
                  FK (home_branch_id, organization_id) -> branches)
memberships      (id, organization_id, member_id, months 1..24, starts_on, ends_on, amount, access_scope
                  'branch'|'gym'|'network', created_at, FK (member_id, organization_id) -> members)
membership_branches (membership_id, branch_id, organization_id)   -- branches this period opens; includes the home branch
receipt_counters (organization_id, kind 'membership'|'day_pass', last_number, PRIMARY KEY (organization_id, kind))   -- now gym_id
receipts         (id, organization_id, kind, number int, branch_id, issued_on date, created_at,
                  UNIQUE (organization_id, kind, number), UNIQUE (id, organization_id))
payments         (id, organization_id, receipt_id UNIQUE, branch_id, kind 'membership'|'day_pass',
                  method 'yape_plin'|'cash'|'card'|'transfer', amount, paid_on date, member_id NULL, membership_id NULL,
                  day_pass_id NULL, created_by, created_at)
```

Indexes to add: `members (organization_id, home_branch_id)`, `memberships (member_id, ends_on)`,
`payments (organization_id, branch_id, paid_on DESC)`, `receipts` by `(organization_id, kind, number)`.
`CHECK`s: `months BETWEEN 1 AND 24`, `ends_on > starts_on`, `amount >= 0`, document formats per type.

The receipt **number** shown to users is `R-` or `D-` plus the number zero-padded to six digits; store the
integer.

### Member status is derived, not stored

Never store `status`. Compute it from `memberships` for a given `today` (Lima date). For a member:

```
active_now   = EXISTS period WHERE starts_on <= today AND ends_on >= today
scheduled    = EXISTS period WHERE starts_on >  today
latest_end   = MAX(ends_on)
status       = active_now ? (latest_end - today <= 7 ? 'expiring' : 'active')
                          : (scheduled ? 'scheduled' : 'expired')
days_remaining = latest_end - today
```

Do it in SQL (a `LEFT JOIN LATERAL` or a grouped subquery) so filtering by status and paging work in the
database. Keep the `today` as a query parameter so tests can fix it.

## API-04 adds

```
access_entries (id, organization_id, branch_id, member_id NULL, display_name, document_number NULL, allowed bool,
                reason_code, kind 'check'|'day_pass', checked_by, checked_at timestamptz)
day_passes     (id, organization_id, branch_id, receipt_id, amount, method, customer_document_type NULL,
                customer_document_number NULL, customer_name NULL, customer_phone NULL, created_by, created_at)
```

`reason_code` values: `MEMBERSHIP_VALID`, `MEMBERSHIP_EXPIRED`, `MEMBERSHIP_NOT_STARTED`, `BRANCH_NOT_ALLOWED`,
`NOT_REGISTERED`, `DAY_PASS`. Index `access_entries (branch_id, checked_at DESC)`. Access entries and
day pass customers are personal data (Peru Law 29733): never log them.

## API-05 adds

No tables. Aggregate queries over `payments` and `memberships`. Add the indexes the explain plans ask for.

## payment_vouchers (module vouchers)
Proof of payment files attached to a payment: `id`, `organization_id`, `payment_id` (composite FK to
`payments(id, organization_id)`, `ON DELETE CASCADE`), `storage_key` (private R2 object), `content_type`
(png, jpeg, webp or pdf), `size_bytes`, `uploaded_by`, `created_at`. At most five per payment, never for
cash, immutable. `payments` gets `UNIQUE (id, organization_id)` for that foreign key.
