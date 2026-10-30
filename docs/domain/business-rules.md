# Business rules

The authoritative list of what Pulso does. Each rule says **where the web prototype shows it**
(paths under `atm-pulso-web/src/`) so you can see the intended behavior. When this file and the
prototype disagree, this file wins; if it is silent, ask the owner.

## 1. What Pulso is

A multi-tenant administration system for gyms. Identity belongs to **atm-auth-api**: an auth *client*
(the customer) is an **organization**, and each auth *company* of that client with Pulso enabled is a
**gym**. Each gym has **branches** (*sedes*), created only by Atmósfera. Members, payments, access control
and day passes belong to exactly one organization. Everything the user sees is in Spanish; identifiers
are English.

## 2. Roles and scope  (`features/auth/services/permissions.ts`)

| Role | Given by | Reaches |
|---|---|---|
| `admin` | the console (superadmin) for the first one; then another admin, through the team page | every branch of the **current gym** (the company of the token) |
| `receptionist` | an admin | only the branches assigned to it |
| platform superadmin | atm-auth-api (not a Pulso user) | only `/admin/*`: branches and gym look |

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
| Prices (*Tarifas*) | its gym | no |

The gym's identity (name, colour, logo) and its branches are configured by Atmósfera from console-web.

## 3. Tenancy rules

- Every query on business data filters by `organization_id` taken from the authenticated principal.
- A resource of another organization addressed in a URL answers **404, never 403**. Foreign
  organization references in a request body answer **400 SCOPE_NOT_ALLOWED**; inaccessible scope
  within the same organization answers **403 SCOPE_NOT_ALLOWED**.
- Branch access: an admin reaches the branches of the current gym; a receptionist the assigned ones
  (`auth.Principal.CanAccessBranch`). **Every endpoint that takes a `branch_id` checks it with that
  method.**

## 4. Gyms and branches are created by Atmósfera  (console-web)

- There are no plans, limits or extras in the API: any number of gyms and branches can exist.
  Commercial terms (Atmósfera charges per branch) are handled outside the system.
- A gym is a company created in console-web with Pulso enabled; Pulso records it by itself, with one
  default branch "Sede principal". More branches are added by the superadmin
  (`POST /admin/gyms/{id}/branches`). Gym admins **cannot** create gyms or branches.
- The identity of a gym (name and RUC from the company, colour from `PATCH /admin/gyms/{id}`, logo from
  the company profile later) is configured from the console. A company may have no RUC.

## 5. Gyms and rules  (`features/organization`)

- The **member directory is always shared** across all gyms and branches of the same owner. There is
  **no setting** for it.
- **Access between branches** is a per-gym rule, `gyms.branch_sharing` (default `true`), set by that
  gym's admin or by the owner. When `false`, the members whose **home gym** is that gym can only
  enter their enrollment (home) branch.
- Membership access (per member) is chosen at enrollment, see §7.
- There is no account-wide "default access scope" any more.
- The rules screen only shows the sharing toggle when the organization has **more than one branch**.

## 6. Members and documents  (`features/members`, `config/documentTypes.ts`)

- A member has: `document_type` (`dni` or `ce`), `document_number`, `full_name` (one field, as typed),
  `phone` (**required**, 9 digits, starts with 9), `home_branch_id` (the branch where it was
  enrolled). **No email.**
- `dni`: exactly 8 digits. `ce` (carné de extranjería): 9 to 12 digits.
- `(organization_id, document_number)` is unique. A duplicate answers `409 MEMBER_DOCUMENT_TAKEN`
  with the existing member's id so the web can open its card to renew.
- The web helps filling the form from `GET /documents/persons/{dni}` (DNI only; CE is manual). The
  lookup never blocks enrollment: if it fails the user types the name.
- Editable later by staff: **name and phone only**. The document never changes.

## 7. Memberships and enrollment  (`features/members/services/enrollment.ts`, `EnrollmentFields.tsx`)

- Memberships are sold **by whole months only**, `months` from 1 to 24. No days, no discounts.
- `start` date defaults to today; a **renewal** defaults to the current end date if it is in the
  future, otherwise today. The start can be set to a future date (the member is then *scheduled*).
- `end` = `start` + `months` calendar months; if the day does not exist in the target month it
  clamps to the last day of that month (Jan 31 + 1 month = Feb 28/29). Computed **on the server**.
- `amount` is the total charged, a non-negative money value. Any discount is negotiated by typing the
  final amount. The web offers quick prices (1 month 100, 2 months 190, 3 months 270, 6 months 500);
  **these are just suggestions in the web, not server data**.
- Payment method: `yape_plin`, `cash`, `card`, `transfer` (Spanish labels: *Yape/Plin*, *Efectivo*,
  *Tarjeta*, *Transferencia*).
- Each enrollment and each renewal creates a **new period** (membership row), a **payment**, and a
  **receipt** (*boleta*) with the next sequential number. All in one database transaction.
- The plan label shown is `"N mes"` / `"N meses"`.
- **Access scope** of a membership: `branch` (only the home branch; the default), `gym` (the home
  gym's branches) or `network` (every branch of the organization). It also stores the explicit list
  of branches it opens, which **always includes the home branch** and by default is every branch of
  the chosen scope. Staff may uncheck some (a promotion valid in 3 of 5 branches). The explicit list
  must only contain branches of the same organization. If the home gym has `branch_sharing = false`
  the list is ignored and only the home branch is allowed (evaluated at access time).
- When the organization has a single branch the web hides the access choice.

## 8. Member status  (`features/members/services/memberData.ts`)

"Today" is the **current date in `America/Lima`** (the prototype uses a fixed demo date; do not copy
that). Given the periods of a member:

- `active`: some period contains today. If it ends in **7 days or fewer** (counting from today to the
  end of the latest period) the status is `expiring`.
- not active: `scheduled` if some period starts after today, otherwise `expired`.
- `days_remaining` = days from today to the latest end date (negative when expired).
- The server computes `status` and `days_remaining`; the web never recalculates them.
- Counts shown on the members screen: all, active (**only** `active`, not `expiring`), expiring,
  expired, scheduled. The dashboard's "Vigentes" is `active + expiring`.

## 9. Receipts (*boletas*)  (`features/members/components/MemberSheet.tsx`, `services/receipt.ts`)

- Number: `R-000001`, sequential **per organization**, allocated atomically (counter row with a lock
  or `INSERT ... ON CONFLICT DO UPDATE ... RETURNING`), so concurrent sales never repeat a number.
  Day passes use their own series `D-000001`, also per organization.
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

1. No member with that document in the organization → `NOT_REGISTERED`.
2. The membership does not open that branch (scope list, `branch_sharing`) → `BRANCH_NOT_ALLOWED`.
3. No period contains today: some period starts later → `MEMBERSHIP_NOT_STARTED`; otherwise
   `MEMBERSHIP_EXPIRED` (with the end date).
4. Otherwise `MEMBERSHIP_VALID` (with the end date of the current period).

Every check, allowed or not, is **logged** as an access entry (who, branch, time, result, reason
code). The web shows the last entries of the branch. Reason texts are Spanish and live in the web.

## 12. Day pass (*pase del día*)  (`features/dayPasses`)

- A one-time entry: no membership, no member row. Reception only confirms the payment method; **all
  customer data is optional** (document type and number, name, phone) and kept for future promotions.
- The **price is fixed per gym** (`gyms.day_pass_price`); when the gym has none the default is **15.00**.
  Only the gym's admin changes it (screen *Tarifas*). The sale uses the stored price; the
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
by Atmósfera from the console (the gym admin cannot change it). The logo is not served yet: it will come from the company profile in atm-auth-api.

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

- `GET /admin/clients` lists **clients** with their gyms (name, RUC when they have one, colour) and
  branches; it first records every company with Pulso enabled.
- The superadmin adds, renames and removes branches (a branch with history, people or that is the last one
  of its gym cannot be removed: `BRANCH_IN_USE`) and sets a gym's colour.

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
