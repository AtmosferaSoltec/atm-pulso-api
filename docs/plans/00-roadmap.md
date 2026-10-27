# Roadmap

One plan at a time. When a plan is done: tests green, summary, and a review from the owner
before the next one starts (AGENTS.md section 10).

| # | Plan | Module(s) | State |
|---|---|---|---|
| 01 | Foundation and auth | `accounts`, `documents` | **done**, in review |
| 02 | Clients, gyms, branches and team | `accounts` (+ tenancy admin) | next |
| 03 | Members, enrollment, receipts and payments | `members`, `payments` | planned |
| 04 | Access control and day passes | `access`, `daypasses` | planned |
| 05 | Reports and exports | `payments` (queries) | planned |
| 06 | Connect `atm-pulso-web` to this API | web repo | planned |

The web prototype (`atm-pulso-web`) already shows every behavior below with fictional data
in memory. Its code is the reference for the rules; this API must enforce them **on the server**
(the web only hides buttons, it is never the guard).

## Plan 02: clients, gyms, branches and team

- **Superadmin**: create a client (organization + its owner user), add gyms and branches,
  list clients with their usage. Document lookup is already available for the form.
- **Free plan** (1 gym, 3 branches per gym): the limits live in one place. Going beyond them
  requires an explicit `agreed_extra: true` from the superadmin, stored with who and when, so
  the extra can be billed later. Owners and admins can never create gyms or branches.
- **Team**: owners create admins (choosing the gym) and receptionists (choosing branches);
  admins create receptionists inside their gym only; nobody edits themselves or an owner;
  deactivating a user takes effect immediately (already guaranteed by `Authenticate`).
- **Gym settings**: `branch_sharing` (the admin decides), `day_pass_price` (the admin decides),
  display name and brand color. Logo upload is out of scope until storage exists.

## Plan 03: members, enrollment, receipts and payments

Proposed tables (to be confirmed when the plan starts):

- `members`: `id`, `organization_id`, `home_branch_id`, `document_type` (`dni`|`ce`),
  `document_number`, `first_names`/`last_names` or `full_name`, `phone`. Unique
  `(organization_id, document_number)`. No email.
- `memberships`: one row per period (start, end, `months` 1..24, `amount`, `payment_method`),
  `access_scope` (`branch`|`gym`|`network`) and the branches it opens (`membership_branches`).
  The home branch is always included.
- `receipts`: `organization_id`, `kind` (`membership`|`day_pass`), `number`. The number is
  sequential per organization (`R-000001`, `D-000001`) and **allocated atomically**
  (`INSERT ... ON CONFLICT DO UPDATE ... RETURNING` on a counter row), so concurrent sales
  never duplicate it.
- `payments`: `receipt_id`, `branch_id` (where it was collected), `amount` (`NUMERIC(14,2)`),
  `method` (`yape_plin`, `cash`, `card`, `transfer`), `paid_on`.

Rules to enforce: months are whole and 1..24; the end date adds calendar months and clamps to the
last day of a shorter month; a duplicate document in an organization is refused; the shared
directory is every member of the organization; the membership only opens branches whose gym
allows `branch_sharing` for the member's home gym.

## Plan 04: access control and day passes

- `access_entries`: who tried to enter, where, when, allowed or not, and why. The decision is
  made on the server from the member's membership and branch rules (never trusted from the web).
  Reasons: valid, expiring, expired, not started yet, other branch, not registered.
- `day_passes`: a single-entry sale. The price is the gym's `day_pass_price` (fixed per gym),
  customer data (document, name, phone) is optional, and it creates its own receipt (`D-`),
  an access entry and a payment. No membership, no member row.
- Receptionists only reach their branches; the superadmin only consults.

## Plan 05: reports and exports

Payments filtered by date range and method, with pagination and the KPIs of the filtered set
(total, count, average ticket, day passes, total per payment method), and the CSV download.
Server-side pagination and aggregation.

## Plan 06: connect the web

Replace the demo store of `atm-pulso-web` with this API, behind the web's existing `services`
layer: login (token kept in memory only), the profile dialog, document lookups, then each
feature as its plan lands. The web calls `https://pulso-api.atmosferast.com/api/v1` directly
(CORS already allows `https://pulso.atmosferast.com`).
Keep the demo data as `seed-dev` for staging.

## Decisions already taken
- Go + Gin + pgx + goose, modular monolith, independent from every other project.
- PostgreSQL `pulso` database and schema on the shared server.
- Stateless access token, no refresh token, no session table (AGENTS.md section 6).
- DNI is the login. No email anywhere for users or members.
- Document lookups go through `atm-identity-api`; Pulso stores none of that data.
