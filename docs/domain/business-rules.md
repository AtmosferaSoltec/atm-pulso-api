# Business rules

The authoritative list of what Pulso does. Each rule says **where the web prototype shows it**
(paths under `atm-pulso-web/src/`) so you can see the intended behavior. When this file and the
prototype disagree, this file wins; if it is silent, ask the owner.

## 1. What Pulso is

A multi-tenant administration system for gyms. Three levels (model decided by the owner, plan 07):

- **Group** (*Grupo*): an internal label the platform superadmin uses to bundle gyms it decides are related
  (for example one person with three brands). It is **not a user** and nobody logs in as a group. There is
  **no owner** figure anywhere. Today it shares nothing; it only exists so that sharing between the gyms of
  a group can be built later.
- **Gym** (*Gimnasio*): a brand (name, logo, colour, optional RUC). It belongs to exactly one group; a group
  has one or many gyms, with the same or different RUC. It is the **tenant**: members, memberships,
  payments, receipts, access control and day passes belong to exactly one gym.
- **Branch** (*Sede*): a physical location of a gym.

Identity belongs to **atm-auth-api**: each auth *company* with Pulso enabled is a gym (the company of the
token is the *current gym*). Everything the user sees is in Spanish; identifiers are English.

## 2. Roles and scope  (`features/auth/services/permissions.ts`)

| Role | Given by | Reaches |
|---|---|---|
| `admin` | the console (superadmin) for the first one; then another admin, through the team page | every branch of the **current gym** (the company of the token) |
| `receptionist` | an admin | only the branches assigned to it |
| platform superadmin | atm-auth-api (not a Pulso user) | only `/admin/*`: groups, gym look and group, branches |
| `member` (**future, not active**) | atm-auth-api | will see their own data; today any role other than `admin` / `receptionist` is refused with `403 NO_ACCESS` |

- A person has one account in atm-auth-api (the **DNI is the login** there). In Pulso they have one user
  record **per gym** they work for, with their own role and branches in each. The DNI is unique inside a gym.
- The role is the `app_role` of the token. Changing it in the console (or from the team page) shows in
  the next token; Pulso updates its record when it sees the new role.
- **Only an admin creates users** (admins and receptionists) in the current gym, through the team page.
  Nobody edits their own role or scope. The DNI cannot be edited (it is the identity in atm-auth-api).
- A person who already works for another company can only be given a second one by Atmósfera, from
  the console (`USER_IN_OTHER_COMPANY`). A person with access to several gyms switches gym by asking
  atm-auth-api for a token of another company.
- Deactivating a user in Pulso takes effect immediately (`USER_INACTIVE`). Passwords, resets and
  sessions belong to atm-auth-api.
- The platform superadmin never touches business data: its token is refused on every route but `/admin/*`.

### Who sees which screen (the web hides, the API enforces)

| Screen | admin | receptionist |
|---|---|---|
| Dashboard (*Resumen*) | yes (current gym) | no (lands on Access) |
| Members | yes | yes |
| Access control | yes | yes |
| Payments list | yes | yes, **without KPIs** |
| Gym rules (*Gimnasios y reglas*) | its gym | no |
| Team (*Usuarios*) | its gym | no |
| Plans and rates (*Planes y tarifas*) | its gym | no |
| Pages of an optional module | only if the gym has the module (section 7b) | same |

The gym's identity (name, colour, logo) and its branches are configured by Atmósfera from console-web.

## 3. Tenancy rules

- Every query on business data filters by `gym_id` taken from the authenticated principal (the gym of
  the token). **Gyms of the same group are isolated from each other**: nothing is shared between gyms.
- A resource of another gym (even of the same group) addressed in a URL answers **404, never 403**.
  Foreign gym or branch references in a request body answer **400 SCOPE_NOT_ALLOWED**; inaccessible
  scope within the same gym (a branch a receptionist is not assigned to) answers **403 SCOPE_NOT_ALLOWED**.
- The group is never read from a request and is never a query filter.
- Branch access: an admin reaches the branches of the current gym; a receptionist the assigned ones
  (`auth.Principal.CanAccessBranch`). **Every endpoint that takes a `branch_id` checks it with that
  method.**

## 4. Groups, gyms and branches are created by Atmósfera  (console-web)

- There are no plans, limits or extras in the API: any number of groups, gyms and branches can exist.
  Commercial terms (Atmósfera charges per branch) are handled outside the system.
- **Only the platform superadmin** creates, renames and removes groups (`POST /admin/groups`,
  `PATCH`/`DELETE /admin/groups/{id}`; a group that still has gyms cannot be removed: `GROUP_IN_USE`),
  moves a gym to another group (`PATCH /admin/gyms/{id}` with `group_id`) and creates branches. Gym
  admins and reception **cannot** do any of this.
- A gym is a company created in console-web with Pulso enabled; Pulso records it by itself, with one
  default branch "Sede principal", inside the group that already holds a gym of the same auth client, or
  in a new group named after that client. The superadmin can regroup it afterwards. More branches are
  added by the superadmin (`POST /admin/gyms/{id}/branches`).
- The identity of a gym (name and RUC from the company, colour from `PATCH /admin/gyms/{id}`, logo from
  the company profile, see section 14) is configured from the console. A company may have no RUC; two gyms of one
  group may share a RUC or not.

## 5. Gyms and rules  (`features/organization`)

- The **member directory is shared across the branches of a gym** and **never across gyms**, not even
  inside one group. Each member belongs to one gym. (A mechanism to share between gyms of a group is
  deliberately **not built**; the group only reserves the place for it.)
- **Access between branches** is a per-gym rule, `gyms.branch_sharing` (default `true`), set by that
  gym's admin. When `false`, the members of that gym can only enter their enrollment (home) branch.
- Membership access (per member) is chosen at enrollment, see §7.
- The rules screen only shows the sharing toggle when the gym has **more than one branch**.

## 6. Members and documents  (`features/members`, `config/documentTypes.ts`)

- A member has: `document_type` (`dni` or `ce`), `document_number`, `full_name` (one field, as typed),
  `phone` (**required**, 9 digits, starts with 9), `home_branch_id` (the branch where it was
  enrolled). **No email.**
- `dni`: exactly 8 digits. `ce` (carné de extranjería): 9 to 12 digits.
- `(gym_id, document_number)` is unique: **per gym**, so the same person can be a member of two gyms
  (even of the same group) as two unrelated records. A duplicate answers `409 MEMBER_DOCUMENT_TAKEN`
  with the existing member's id so the web can open its card to renew.
- A member row may later be linked to a person of atm-auth-api (`members.auth_user_id`, nullable, unused
  and not exposed today) for the future `member` role.
- The web helps filling the form from `GET /documents/persons/{dni}` (DNI only; CE is manual). The
  lookup never blocks enrollment: if it fails the user types the name.
- Editable later by staff: **name and phone only**. The document never changes.

## 7. Memberships and enrollment  (`features/members/services/enrollment.ts`, `EnrollmentFields.tsx`)

- Memberships are sold **by whole months only**, `months` from 1 to 24. No days, no discounts.
- `start` date defaults to today; a **renewal** defaults to the end date of the member's last period
  **of the same plan** if it is in the future, otherwise today. The start can be set to a future date (the member is then *scheduled*).
- `end` = `start` + `months` calendar months; if the day does not exist in the target month it
  clamps to the last day of that month (Jan 31 + 1 month = Feb 28/29). Computed **on the server**.
- `amount` is the total charged, a non-negative money value. Any discount is negotiated by typing the
  final amount. The web offers quick prices (1 month 100, 2 months 190, 3 months 270, 6 months 500);
  **these are just suggestions in the web, not server data**.
- Payment method: `yape_plin`, `cash`, `card`, `transfer` (Spanish labels: *Yape/Plin*, *Efectivo*,
  *Tarjeta*, *Transferencia*).
- Each enrollment and each renewal creates a **new period** (membership row), a **payment**, and a
  **receipt** (*boleta*) with the next sequential number. All in one database transaction.
- The duration label shown is `"N mes"` / `"N meses"`.
- **Access scope** of a membership: `branch` (only the home branch; the default) or `gym` (the
  branches of the gym). The former `network` scope (every branch of the organization) **no longer
  exists**; a request with `network` is `400 VALIDATION_FAILED`, and old `network` periods were converted
  to `gym`. The period also stores the explicit list of branches it opens, which **always includes the
  home branch** and by default is every branch of the chosen scope. Staff may uncheck some (a promotion
  valid in 3 of 5 branches). The explicit list must only contain branches of the same gym. If the gym has
  `branch_sharing = false` the list is ignored and only the home branch is allowed (evaluated at access
  time).
- When the gym has a single branch the web hides the access choice.

## 7b. Plans and optional modules  (plan 08)

- A gym sells **membership plans** (e.g. *Baile*, *Todo el gym*), each with a price per duration (1 to 24 months). Prices are only suggestions: the
  final amount can always be typed. Names are unique per gym. Plans are deactivated, never deleted; each period keeps the plan id and the
  plan's name at the time of sale (receipts never change).
- A member **may hold several current periods at once**, one per plan. A plan **does not restrict access** (yet): the entry is allowed when any
  current period, of any plan, opens the branch.
- With exactly **one active plan** the sale does not ask for a plan (the API uses it; a gym with none gets "Membresía"). With several, `plan_id`
  is required. Only the gym's admin manages plans; reception reads the active ones.
- **Modules** (`sales`, `body_tracking`, `kiosk`) are switched per gym by the platform superadmin from console-web; the list is closed in
  code. A route of a module the gym lacks answers `403 FEATURE_DISABLED`; the web hides its pages. Not every gym wants everything.

## 7c. Same-plan purchases, overlap and corrections  (module `members`)

- **Renewal, not a new membership.** If the member already holds a current or scheduled period of a plan, buying that plan again is a
  *renewal*: it begins on the **end date of the member's last period of that plan** (the server computes it when `starts_on` is omitted).
  Different plans live side by side (Baile + Todo el gym).
- **Periods are half-open `[starts_on, ends_on)`** for overlap purposes: starting on the previous `ends_on` is the renewal and is allowed;
  starting one day earlier overlaps. A period of a plan may never overlap another period of **the same plan** of the same member
  (`409 MEMBERSHIP_OVERLAP` with `details { membership_id, starts_on, ends_on, suggested_starts_on }`). Coverage still counts `ends_on` as a covered day (section 8).
- **An explicit start before the end is rejected**, not silently moved: the staff must pick the suggested date (or a later one: a gap is allowed;
  a past start is allowed only when it collides with nothing, e.g. recording an old sale). The check runs under the member's row lock, so two simultaneous
  purchases cannot both pass. Idempotent retries (same `id`) return the original sale.
- **Correcting a period** (`PATCH /members/{id}/memberships/{membership_id}`): months, start date, amount and payment method. Only periods that
  have **not ended** (`ends_on >= today`, Lima) and whose new end is not in the past; the plan never changes; the end date is recalculated by the
  server; the same overlap rule applies against the other periods of the plan. Admin and reception (reception needs access to the branch that collected the payment).
- **What happens to the receipt (decision):** it is **corrected in place, never deleted or re-issued**. The receipt number and the payment row stay; the payment's
  `amount` and `method` take the new values and the printable receipt reads the period live, so it shows the new plan duration and dates. Traceability: every
  change inserts a row in `membership_amendments` (who, when, values before and after) and sets `memberships.amended_at` (`amended_at` in the API);
  the history is `GET .../amendments` (admin). `paid_on` is not changed. Because the payment amount changes, membership income reports of that day change
  too; the history keeps the old value. A change that alters nothing writes nothing.

## 8. Member status  (`features/members/services/memberData.ts`)

"Today" is the **current date in `America/Lima`** (the prototype uses a fixed demo date; do not copy
that). Given the periods of a member:

Periods of **all plans** count together. A period is *current* when `starts_on <= today <= ends_on` (both inclusive).

- Covered = some period of any plan is current. The **coverage end** starts at the latest end among the current periods and is extended by
  any period that begins on or before the day after it (a renewal queued behind, or another plan that continues it), repeatedly.
  `expiring` if the coverage end is **7 days or fewer** away, otherwise `active`; `days_remaining` = coverage end - today.
  (A member with *Baile* ending in 3 days and *Todo el gym* ending in 60 is `active` with 60 days; with a gap before the next period they are `expiring`.)
- Not covered: `scheduled` if some period starts after today, otherwise `expired`; `days_remaining` = days to the latest end of any period (negative when past).
- The server computes `status` and `days_remaining`; the web never recalculates them.
- Counts shown on the members screen: all, active (**only** `active`, not `expiring`), expiring,
  expired, scheduled. The dashboard's "Vigentes" is `active + expiring`.

## 9. Receipts (*boletas*)  (`features/members/components/MemberSheet.tsx`, `services/receipt.ts`)

- Number: `R-000001`, sequential **per gym**, allocated atomically (counter row with a lock
  or `INSERT ... ON CONFLICT DO UPDATE ... RETURNING`), so concurrent sales never repeat a number.
  Day passes use their own series `D-000001`, also per gym. Why per gym and not per group: the gym is the
  brand that issues the receipt (own name, own RUC, maybe none) and the unit of isolation, and the group
  is only an internal label that may change; a series that depended on it would renumber or collide when
  the superadmin regroups gyms.
- A receipt records: number, issue date (today, Lima), the member, the plan label, start and end,
  amount, method, the branch where it was collected and that branch's gym (display name, RUC, branch
  name and address) and the branches the membership opens.
- It is **not** a tax document; the printed text says so. The terms and conditions are static text
  that lives in the web (`membershipTerms.ts`).
- Any past receipt must be retrievable (reprint), not only the latest.

## 10. Payments  (`features/payments`)

- One payment row per receipt (membership or day pass): `branch_id` (where it was collected), `amount`,
  `method`, `paid_on` (today, Lima), `receipt_id`, and the kind (`membership` | `day_pass`).
- The list is **per branch** (the user's active branch), filterable by date range (`from`, `to`) and by
  method, paginated, newest first. The web opens on **today**; "this month" is the other shortcut.
- **KPIs** for the filtered set: total collected, number of payments, average ticket (none when 0
  payments), number of day passes, most used method, and the **total per payment method**
  (all four methods even when 0). **Receptionists must not get KPIs** (403).
- CSV export of the filtered set (all rows, not the page), with columns: receipt number, member name,
  document, plan, amount, method, date.

## 11. Access control  (`features/access/services/accessService.ts`)

Reception types the member's document (DNI or CE) with a numeric keypad and the system answers
allowed or denied. The decision is made **on the server** for a `branch_id` the user can access:

1. No member with that document in the gym → `NOT_REGISTERED`.
2. The membership does not open that branch (scope list, `branch_sharing`) → `BRANCH_NOT_ALLOWED`.
3. No period contains today: some period starts later → `MEMBERSHIP_NOT_STARTED`; otherwise
   `MEMBERSHIP_EXPIRED` (with the end date).
4. Otherwise `MEMBERSHIP_VALID`. With several plans, steps 2 and 4 look at **any** current period that opens the branch; the end date shown (and the
   7-day warning) is the coverage end (section 8) of the periods that open that branch.

Every check, allowed or not, is **logged** as an access entry (who, branch, time, result, reason
code). The web shows the last entries of the branch. Reason texts are Spanish and live in the web.

## 11b. Kiosk mode  (module `kiosk`; `features/kiosk`, `internal/modules/kiosk`)

A tablet at the door where the member types a document and enters, unsupervised. Only for gyms with the `kiosk` module
(superadmin switch); every route answers `403 FEATURE_DISABLED` without it.

- It is the web route `/kiosk`, full screen, no sidebar. It runs with the session of a **receptionist pinned to one branch**
  (an admin works too) and uses the normal `POST /access/checks`, so every attempt is logged exactly like reception's.
  With several branches the kiosk asks for the branch **once** and remembers it in that device (`localStorage`).
- The screen shows only the **first name** and the status (days left if allowed; a plain Spanish reason if denied:
  not registered, expired, not started yet, branch not allowed). Never the document, phone, amount or history. The typed
  document is masked (all digits but the last) and is dropped from the screen state as soon as the answer arrives.
  The answer clears after 5 s; a half-typed document after 20 s of idle.
- No connection, API error or rate limit (429, per IP) shows a clear notice and **never** an "allowed" state; a request
  that takes over 12 s counts as lost connection. One check at a time (no double send).
- **Exit PIN**: holding the logo 3 s asks for a PIN (4 to 8 digits) set by the **admin** (page "Quiosco"). Stored as a bcrypt
  hash per gym in `kiosk_settings` and checked by the API (`POST /kiosk/exit`); 5 wrong PINs lock that user for 5 minutes.
  Why not in the device: the admin sets it once for every tablet, a hash in the browser could be read or replaced by whoever
  uses the tablet, and the API can throttle. The kiosk refuses to start until a PIN exists.
- Limits of a web kiosk: leaving it is only protected inside the app; locking the tablet itself (Android screen pinning, iPad
  Guided Access) is a device setting. Wake Lock and fullscreen are requested when the browser has them (Wake Lock needs https).
- Phase 2 (pending): registered devices with their own credential instead of a shared receptionist session.

## 11c. Sales of products  (module `sales`; `internal/modules/sales`)

Optional module (`sales`), switched per gym by the superadmin. Every route is behind `auth.RequireFeature(sales)` (`403 FEATURE_DISABLED`); the web hides the pages.
It is **a count of units sold and of what members still have to pay**, not accounting: **no receipts or R-/D- series, no invoices, no tax, no inventory/stock**.

- **Catalog**: per gym, `name` (1-60, unique ignoring case), `price` (0.01 to 10000.00), `is_active`, `position`. Only the admin manages it (screen "Productos"). Products are never deleted, only deactivated.
- **A sale** is a normal sale of one or more products: items (product, quantity 1-99, at most 30 different products, total at most 100000.00), branch (must be reachable by the user),
  who registered it, Lima date, and an **optional member** of the same gym. The unit price is **read from the catalog by the server** and kept on the sale with the product name (a photo: old sales never change).
  Repeated products in one request are merged. Idempotent by the client's `id`.
- **Status**: `paid` (collected at the moment, with `method`: `cash`, `yape_plin`, `card`, `transfer`) or `pending` (not collected yet: **requires the member**; no method yet).
  `was_pending` stays true after a pending sale is collected, so history shows it started pending.
- **Pending balance of a member** = sum of their `pending` sales. It is the gym's (shared by all branches, like the directory). Any admin or receptionist with access can open the
  member's file, see the pending sales (products, quantities, dates, total) and collect them.
- **Collect** (decision): a collection pays **whole sales**. The user picks some pending sales of one member or none (= all of them), the payment method and the collecting branch; one `sale_collections`
  row records amount, method and who collected it, and the sales become `paid` (`was_pending = true`, pointing to the collection). There are no partial payments of one sale: it is paid whole or not at all
  (the simplest model that stays exactly consistent). Collecting is all-or-nothing, locks the sales, and two people collecting at once cannot collect twice (the second gets `409 NOTHING_TO_COLLECT` / `SALE_NOT_PENDING`). Idempotent by `id`.
- **Purchase history** of a member: `GET /members/{id}/sales` (paid and pending, all branches).
- **Roles**: admin and reception register sales and collect. Reception lists sales only of its branches; balances and a member's history are the gym's. Only the admin manages the catalog and reads the report.
- **Report** (admin, `/sales/report`): units and money by product and by day/month, filtered by Lima dates and branch. `total_sold` (all sales), `collected_total` (paid, including pending sales collected since)
  and `pending_total` (still pending). **Separate from membership income**: product sales never appear in `payments`, the CSV export or the dashboard (a test guards it).
- Not included (open): voiding a wrong sale, limits on what a member can owe, stock.

## 12. Day pass (*pase del día*)  (`features/dayPasses`)

- A one-time entry: no membership, no member row. Reception only confirms the payment method; **all
  customer data is optional** (document type and number, name, phone) and kept for future promotions.
- The **price is fixed per gym** (`gyms.day_pass_price`); when the gym has none the default is **15.00**.
  Only the gym's admin changes it (screen *Planes y tarifas*). The sale uses the stored price; the
  client cannot choose the amount.
- A sale, in one transaction, creates: the day pass, a receipt `D-NNNNNN`, a payment (kind
  `day_pass`) and an **allowed access entry** (reason `DAY_PASS`). No ticket is printed.
- Day passes count in the payments list, in the KPIs ("pases del día") and in the month revenue of the
  dashboard.
- Sold by any user who can access that branch (admin, receptionist). Default method: cash.

## 13. Dashboard  (`features/dashboard`)

For the active branch: **active members** (`active + expiring`, members whose home branch is it),
**month revenue** (all payments collected in that branch in the current month, day passes included),
**expiring** count, and the first five expiring members with their quick actions.

## 14. Gym identity  (`features/branding`)

Per gym: `display_name`, `brand_color` (`#rrggbb`, six hex digits, lower case) and a logo. The color
tints the web (buttons, selected items, receipts). Branches of one gym share the identity. Configured
by Atmósfera from the console (the gym admin cannot change it). 
The **logo** is the one of the company profile (uploaded in console-web to atm-storage-api and saved as
`logo_file_id` in atm-auth-api). Pulso stores, processes and serves no file: it reads `logo_file_id` from the
service API (`GET /v1/service/tenants`), exchanges it for a temporary signed link with atm-storage-api (service
token, `POST /v1/files/preview`) and returns that link as `logo_url` in `GET /me`, `GET /catalog` and the printable
receipt. Rules:

- Only the logo of the **caller's own gym** is ever resolved: the company comes from the verified token, never
  from the request, so a gym never receives another gym's link (not even a sibling in the same group).
- The link is temporary (about one hour). Clients must not store it; they ask again and fall back to the monogram
  when the image does not load.
- Pulso caches the answer per gym for at most 5 minutes (and never reuses a link that expires within a minute), so
  a logo changed in the console shows within minutes.
- Degradation: if atm-auth-api or atm-storage-api fail, time out (3 s), or the gym has no logo, `logo_url` is
  `null` and the request succeeds. A failure is remembered 30 s so an outage does not slow every request.
- Without `AUTH_CLIENT_SECRET` (development) no logo is resolved.

## 15. Team  (`features/team`)

- Fields: `full_name`, `document_number` (DNI), `role`, `branch_ids` (receptionists), `is_active`, optional
  `phone` (the user edits it in their profile).
- Creating a person calls atm-auth-api (members API of the system, in the current company): the account is
  created (the DNI is the first password and must be changed at the first login) or, if the DNI already
  exists, only the access is added. Pulso then records the person and, for a receptionist, the branches.
  Creation accepts no caller-selected password.
- There is no email recovery. An admin resets a password (back to the DNI, change required) through
  atm-auth-api.
- Changing a role is also applied in atm-auth-api. An admin has no assigned branches (they reach the whole
  gym); a receptionist needs at least one, all of the current gym.
- An admin can deactivate and reactivate users, never themselves; this blocks them in Pulso only.
  Deactivating never deletes history.
- Who is listed: the people atm-auth-api lets into the current company, merged with Pulso's records
  (people the console added appear the first time the list is read). If atm-auth-api cannot be reached
  the list falls back to the people Pulso already knows.

## 16. Platform panel (superadmin, in console-web)

- `GET /admin/groups` lists **groups** with their gyms (name, RUC when they have one, colour) and
  branches; it first records every company with Pulso enabled.
- The superadmin creates, renames and removes groups, moves a gym between groups, adds, renames and
  removes branches (a branch with history, people or that is the last one of its gym cannot be removed:
  `BRANCH_IN_USE`) and sets a gym's colour.

## 17. Profile  (`features/auth/components/ProfileDialog.tsx`)

Every user sees their data and edits **name and phone**. The DNI, role and scope are read-only, and the
password is changed in atm-auth-api.

## 18. Formats and conventions

- Money: `NUMERIC(14,2)` in the database, **string** in JSON (`"100.00"`), maximum 1,000,000.00.
- Dates `YYYY-MM-DD` (Lima for business dates), timestamps RFC 3339 UTC.
- Pagination: `page` (from 1) and `page_size` (default 25, max 100); answer `meta {page, page_size, total}`.
- Creating endpoints accept an optional client-generated `id` (UUID): repeating the same request with
  the same `id` returns the original result instead of creating a duplicate.
- Errors: `{ "error": { "code", "message", "details" } }`, codes stable, messages Spanish.
