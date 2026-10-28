# Data model

What exists today and what each plan adds. PostgreSQL, schema `pulso`, role `pulso_app`. Names are
English `snake_case`, plural tables, `TIMESTAMPTZ` in UTC, UUID v7 ids, `NULL` instead of empty
strings, money `NUMERIC(14,2)`.

**Production already has migrations `00001` and `00002` applied. Never edit them.** All changes are new
files `000NN_<module>_<what>.sql` with `Up` and `Down`.

## Tenant safety, by design

Every business table carries `organization_id`. To let the **database itself** refuse cross-tenant
links, parent tables expose `UNIQUE (id, organization_id)` and children use a **composite foreign key**
`(parent_id, organization_id) REFERENCES parent (id, organization_id)`. The existing example is
`users (gym_id, organization_id) -> gyms (id, organization_id)`. Follow it for every new link. Tests must
prove that inserting a cross-tenant link fails (see `TestDatabaseEnforcesTenancyAndRoleRules`).

## Existing (API-01)

```
organizations (id, name, contact_document_number, contact_name, contact_email, contact_phone, created_at, updated_at)
gyms          (id, organization_id -> organizations, name, display_name, tax_id, brand_color, day_pass_price,
               branch_sharing, created_at, updated_at, UNIQUE (id, organization_id))
branches      (id, gym_id -> gyms, name, address, created_at, updated_at)
users         (id, organization_id, gym_id, document_type='dni', document_number UNIQUE, full_name, phone, role,
               password_hash, password_version, is_active, created_at, updated_at, + role/scope CHECK and composite FK to gyms)
user_branches (user_id -> users, branch_id -> branches)         -- a receptionist's branches
```

`gyms.tax_id` is the RUC and may repeat between gyms. `gyms.day_pass_price` NULL means "default 15.00".

## API-02 adds

- **`users.must_change_password`**: non-null boolean, false for existing accounts. New users and
  manager resets set it true. Migration `00003` introduces this state without changing existing
  passwords. Password updates compare `password_version` to prevent concurrent reset overwrite.


- **`branches.organization_id`** (NOT NULL, backfilled from the gym; production is empty so it is trivial),
  `UNIQUE (id, organization_id)` and `FOREIGN KEY (gym_id, organization_id) REFERENCES gyms (id, organization_id)`.
  Every later table links to branches with `(branch_id, organization_id)`.
- **`user_branches.organization_id`** with composite FKs to `users` and `branches`, so a receptionist can never
  be linked to a branch of another client.
- ~~`plan_agreements`~~: dropped by migration 00009 (the product has no plans or limits).
- **`gym_logos`** *(only if the owner approves Q-LOGO)*: `gym_id` PK, `content_type`, `bytes bytea` (max 512 KB),
  `updated_at`.

## API-03 adds

```
members          (id, organization_id, home_branch_id, document_type 'dni'|'ce', document_number, full_name, phone,
                  created_at, updated_at, UNIQUE (organization_id, document_number), UNIQUE (id, organization_id),
                  FK (home_branch_id, organization_id) -> branches)
memberships      (id, organization_id, member_id, months 1..24, starts_on, ends_on, amount, access_scope
                  'branch'|'gym'|'network', created_at, FK (member_id, organization_id) -> members)
membership_branches (membership_id, branch_id, organization_id)   -- branches this period opens; includes the home branch
receipt_counters (organization_id, kind 'membership'|'day_pass', last_number, PRIMARY KEY (organization_id, kind))
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
