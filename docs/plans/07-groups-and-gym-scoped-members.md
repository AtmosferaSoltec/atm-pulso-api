# API-07: groups, gym-scoped members

**State: implemented locally (migration 00013), waiting for the owner's review.**
**Replaces:** every mention of "organization", "owner", "client" and "network" in plans 01-06 and in the older
text of the domain documents. Where a plan disagrees with this file, this file wins.

## Decision of the product owner

- **Group** (*Grupo*) replaces organization / client / owner. An internal label the platform superadmin uses to
  bundle gyms that are related (e.g. one person with three brands). It is **not a user** and there is no
  owner figure.
- **Gym** is a brand (name, logo, colour, optional RUC) and belongs to one group. A group has many gyms.
- **Branch** (*sede*) is a physical location of a gym.
- The **member belongs to a gym**; document uniqueness is per gym. Members are shared between the branches
  of the same gym (`branch_sharing` switch and `branch`/`gym` membership scopes stay). Nothing is shared
  between gyms; **no sharing mechanism between gyms is built**, the group only reserves the place.
- Only the superadmin (console-web, `/api/v1/admin`) creates groups, gyms (recorded from atm-auth-api companies)
  and branches. Gym admins never do.
- Future, not active: a `member` role (atm-auth-api users who see their own data). An unknown role is still
  `403 NO_ACCESS`. `members.auth_user_id` (nullable, unused) is added because it is free.

## Technical decisions

| Question | Decision | Why |
|---|---|---|
| Rename `organization*` to `group*`? | **Yes**, migration 00013 renames the table and `gyms.organization_id` and rewrites the tenant key. | The tenant key of every business table had to change anyway (organization -> gym), so touching these identifiers cost nothing extra: about 450 occurrences in ~60 files, mechanical, done with the compiler and the tests as a safety net. Leaving `organization_id` meaning "gym" would have misled every future reader. |
| Tenant key of business tables | `gym_id` (composite FKs `(x_id, gym_id)`), backfilled from each row's branch. | A gym is the isolation unit now. |
| Receipt and day pass series | **Per gym.** | The gym is the brand that issues the receipt (own name, own RUC or none) and the isolation unit; the group is a mutable internal label, so a group-wide series would collide or renumber when the superadmin regroups gyms. Existing counters were split per gym from the real maximum. |
| Membership scope `network` | Removed from the CHECK and the API; old rows converted to `gym`. | It no longer has meaning. |
| `/admin/clients` | Replaced by `/admin/groups` (list, create, rename, delete-if-empty); `PATCH /admin/gyms/{id}` also moves a gym to another group. | Only the superadmin manages groups. |
| `/me` and `/catalog` | `organization` removed; the catalog lists only the current gym. | The group is internal. |
| Auto grouping | A gym seen for the first time joins the group of any gym with the same `auth_client_id`, else a new group named after the client. | Keeps today's behaviour as the default; the superadmin can change it. |

## Tests added

Isolation between two gyms of the same group (404 on every id, 400 `SCOPE_NOT_ALLOWED` in bodies), same
document in two gyms, document unique inside a gym, access check does not see members of a sibling gym, receipt
series independent per gym, `network` rejected, unknown role `member` still `NO_ACCESS`, database refuses
cross-gym links, superadmin group management and `GROUP_IN_USE`.
