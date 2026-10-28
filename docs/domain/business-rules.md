# Business rules

The authoritative list of what Pulso does. Each rule says **where the web prototype shows it**
(paths under `atm-pulso-web/src/`) so you can see the intended behavior. When this file and the
prototype disagree, this file wins; if it is silent, ask the owner.

## 1. What Pulso is

A multi-tenant administration system for gyms. An **organization** is a client (its owner). It has
**gyms**, and each gym has **branches** (*sedes*). Members, payments, access control and day passes
belong to exactly one organization. Everything the user sees is in Spanish; identifiers are English.

## 2. Roles and scope  (`features/auth/services/permissions.ts`)

| Role | Created by | Organization | Reaches |
|---|---|---|---|
| `superadmin` | inserted by hand (first) / by another superadmin later | none | consults any organization, **read-only** |
| `owner` | superadmin, together with the client | its own | every gym and branch of the organization |
| `admin` | the owner | its own | one gym (`gym_id`) and its branches |
| `receptionist` | the owner, or an admin for its own gym | its own | only the branches assigned to it |

- A person can have only one user: the **DNI is the login** and is unique system-wide.
- Only the authorized superior may correct a user's DNI: superadmin for owners, owner for
  admins/receptionists, and admin for receptionists entirely within its gym. No self-correction.
  Correction invalidates existing tokens. If a mandatory password change is pending, the temporary
  password becomes the corrected DNI; otherwise the chosen password remains unchanged.
- **Only an owner creates admins. An admin only creates receptionists**, and only for branches of
  its own gym. Nobody edits their own role or scope; nobody except the superadmin touches an owner.
- Deactivating a user takes effect immediately (already guaranteed by `Authenticate`).
- **Superadmin is read-only.** It "inspects" one organization at a time: org-scoped endpoints accept
  the header `X-Organization-Id` **only from a superadmin and only on `GET`**; any write by a
  superadmin to org data is `403 FORBIDDEN`. This header is the single exception to "scope never
  comes from the request" and must be validated (the organization must exist).

### Who sees which screen (the web hides, the API enforces)

| Screen | owner | admin | receptionist | superadmin |
|---|---|---|---|---|
| Dashboard (*Resumen*) | yes | yes | no (lands on Access) | inspect |
| Members | yes | yes | yes | inspect |
| Access control | yes | yes | yes | inspect (no verify buttons) |
| Payments list | yes | yes | yes, **without KPIs** | inspect |
| Gyms and rules (*Gimnasios y reglas*) | all gyms | its gym | no | inspect |
| Team (*Usuarios*) | all | receptionists of its gym + itself | no | inspect |
| Prices (*Tarifas*) | all gyms | its gym | no | inspect |
| Identity (*Identidad*) | all gyms | its gym | no | inspect |
| Client panel (*Clientes y gimnasios*) | no | no | no | yes (its own home) |

## 3. Tenancy rules

- Every query on business data filters by `organization_id` taken from the authenticated principal.
- A resource of another organization addressed in a URL answers **404, never 403**. Foreign
  organization references in a request body answer **400 SCOPE_NOT_ALLOWED**; inaccessible scope
  within the same organization answers **403 SCOPE_NOT_ALLOWED**.
- Branch access: an owner reaches all branches of its organization; an admin the branches of its
  gym; a receptionist the assigned ones (`auth.Principal.CanAccessBranch`). **Every endpoint that
  takes a `branch_id` checks it with that method.**

## 4. Gyms and branches are created only by the superadmin  (`features/platform`)

- There are no plans, limits or extras in the product: any number of gyms and branches can exist.
  Commercial terms are handled by Atmósfera outside the system.
- Owners and admins **cannot** create gyms or branches. Only the superadmin does.
- The identity of a gym (display name, colour, logo) is also set **only by the superadmin**.

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
  Only the gym's admin or the owner changes it (screen *Tarifas*). The sale uses the stored price; the
  client cannot choose the amount.
- A sale, in one transaction, creates: the day pass, a receipt `D-NNNNNN`, a payment (kind
  `day_pass`) and an **allowed access entry** (reason `DAY_PASS`). No ticket is printed.
- Day passes count in the payments list, in the KPIs ("pases del día") and in the month revenue of the
  dashboard.
- Sold by any user who can access that branch (owner, admin, receptionist). Default method: cash.

## 13. Dashboard  (`features/dashboard`)

For the active branch: **active members** (`active + expiring`, members whose home branch is it),
**month revenue** (all payments collected in that branch in the current month, day passes included),
**expiring** count, and the first five expiring members with their quick actions.

## 14. Gym identity  (`features/branding`)

Per gym: `display_name`, `brand_color` (`#rrggbb`, six hex digits, lower case) and a logo. The color
tints the web (buttons, selected items, receipts). Branches of one gym share the identity. Managed by
the gym's admin or the owner. Logos and image uploads are deferred until all real functionality is implemented; future storage is Cloudflare R2.

## 15. Team  (`features/team`)

- Fields: `full_name`, `document_number` (DNI), `role`, `gym_id` (admins), `branch_ids` (receptionists),
  `is_active`, optional `phone` (the user edits it in their profile).
- A newly created user's initial password is their DNI, hashed with argon2id. Creation accepts no
  caller-selected password. `must_change_password` is true on creation and manager reset.
- There is no email recovery. An authorized manager resets the password to the user's DNI, bumps
  `password_version` to invalidate existing tokens, and requires another password change. The owner
  manages admins and receptionists; an admin manages receptionists entirely within its gym; the
  superadmin manages owners through platform operations.
- Login with the temporary password returns a restricted token and `must_change_password: true`.
  Until the password is changed, only `GET /me` and `POST /me/password` are available; every other
  authenticated endpoint returns `403 PASSWORD_CHANGE_REQUIRED`.
- Completing a required change clears the flag, invalidates all tokens and returns
  `{ "data": { "login_required": true } }` without a replacement token. The web returns to login;
  the user signs in with DNI and their new password before entering the application.
- The new password has at least 8 characters and differs from the current password. Ordinary
  voluntary profile password changes retain the existing fresh-token behavior.
- A receptionist's branches must all belong to gyms the creator controls and there must be at least one.
- An owner or admin can deactivate and reactivate users, never themselves. Deactivating never deletes
  history.
- Who is listed: an owner sees every user of its organization; an admin sees itself and the
  receptionists of its gym; nobody sees the superadmin.

## 16. Platform panel (superadmin)  (`features/platform`)

- Lists **clients** with: name, gyms (name, RUC) with their branches and whether the client is active; searchable by client, gym or RUC; metrics: clients,
  gyms, branches, users of clients.
- Creates a client: the owner (document, first and last names, email, phone, **DNI as temporary password**) and
  its gyms with branches. **The client's name is the owner's full name.** Adds gyms to a client and
  branches to a gym. See §4 for the plan limits and the agreement flag.
- Email and phone of the owner are contact data of the organization; they are not login data.

## 17. Profile  (`features/auth/components/ProfileDialog.tsx`)

Every user (including the superadmin) sees their data, edits **name and phone**, and changes their
password (current + new, minimum 8). The DNI, role and scope are read-only in the profile. Already built in API-01.

## 18. Formats and conventions

- Money: `NUMERIC(14,2)` in the database, **string** in JSON (`"100.00"`), maximum 1,000,000.00.
- Dates `YYYY-MM-DD` (Lima for business dates), timestamps RFC 3339 UTC.
- Pagination: `page` (from 1) and `page_size` (default 25, max 100); answer `meta {page, page_size, total}`.
- Creating endpoints accept an optional client-generated `id` (UUID): repeating the same request with
  the same `id` returns the original result instead of creating a duplicate.
- Errors: `{ "error": { "code", "message", "details" } }`, codes stable, messages Spanish.
