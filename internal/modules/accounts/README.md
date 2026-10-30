# accounts module

Owns **who works where**: organizations (the customers of atm-auth-api), gyms (their companies with Pulso
enabled), branches and the people of each gym. **Identity is not here**: login, passwords and sessions belong
to atm-auth-api (see AGENTS.md section 6).

## Tables (it owns them)
`organizations` (`auth_client_id`), `gyms` (`auth_tenant_id`), `branches`, `users` (one row per person per
gym, `auth_user_id`), `user_branches`. The database enforces the rules that must never be broken: a user's
gym and a receptionist's branches belong to the user's organization (composite FKs), the role is `admin` or
`receptionist` (`CHECK`), and a DNI is unique inside a gym.

## Use cases (`app.Service`)
- `Resolve` (implements `auth.Resolver` for every private route): token claims -> organization, gym and user
  (created on first sight), `ErrNoAccess` / `ErrInactive`.
- `Me`, `UpdateProfile` (name and phone), `Catalog`, `UpdateGym` (day pass price, branch sharing, membership
  prices; a gym admin cannot touch the identity).
- Team (`ListUsers`, `CreateUser`, `UpdateUser`, `SetUserActive`, `ResetUserPassword`): admin only, current gym.
  The account lives in atm-auth-api, reached through the `Directory` port.
- Superadmin (`ListClients` with sync, `UpdateGymIdentity`, `AddBranch`, `UpdateBranch`, `DeleteBranch`).

## Ports (`app/ports.go`)
`TenancyRepository`, `UserRepository`, `TeamRepository`, `GymRepository`, `CatalogRepository`,
`AdminRepository` (all implemented by `adapters/postgres`) and `Directory` (atm-auth-api, implemented by
`adapters/authdir` on top of `integrations/authapi`; every failure becomes a domain error).

## Errors
| code | status | when |
|---|---|---|
| `UNAUTHORIZED` | 401 | missing, invalid or expired token |
| `NO_ACCESS` | 403 | the token does not open Pulso (other system, unknown role, no company or client) |
| `USER_INACTIVE` | 403 | the person was deactivated in Pulso |
| `FORBIDDEN` | 403 | wrong role, wrong kind of token, or identity fields sent by a gym admin |
| `SCOPE_NOT_ALLOWED` | 403 / 400 | a gym or branch outside the caller's reach (400 when it is not in their organization) |
| `ROLE_NOT_ASSIGNABLE` | 403 | role other than admin/receptionist, or refused by atm-auth-api |
| `CANNOT_MODIFY_SELF` | 403 | an admin acting on themselves in the team |
| `USER_DOCUMENT_TAKEN` | 409 | the DNI already belongs to someone in the gym |
| `USER_IN_OTHER_COMPANY` | 409 | the person works for another company: only Atmósfera can add them |
| `BRANCH_IN_USE` | 409 | removing a branch with history, people, or the last of its gym |
| `AUTH_UNAVAILABLE` | 503 | atm-auth-api did not answer or is not configured |
| `VALIDATION_FAILED` | 400 | bad fields; `details` lists `{field, code}` |

## Team and `manage_users`
The team page calls atm-auth-api's service members API (`/v1/service/members?tenant_id=`) with Pulso's service
token, so Pulso decides who may manage the team (admins) and no `manage_users` flag is needed for that.
