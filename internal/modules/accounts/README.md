# accounts module

Owns **who can sign in and what they can see**: organizations (clients), gyms, branches and
users, plus login and the user's own profile.

## Tables (it owns them)
`organizations`, `gyms`, `branches`, `users`, `user_branches`. The database enforces the rules
that must never be broken: an admin's gym belongs to the admin's organization (composite FK),
every role has exactly the scope it needs (`CHECK`), and a DNI is unique system-wide.

## Use cases (`app.Service`)
`Login`, `Authenticate` (implements `auth.Authenticator` for every private route), `Me`,
`UpdateProfile`, `ChangePassword`.

## Ports (`app/ports.go`)
`UserRepository`, `PasswordHasher`, `TokenIssuer`, `TokenVerifier`. Implemented by
`adapters/postgres` and `adapters/security` (argon2id and JWT HS256).

## Errors
| code | status | when |
|---|---|---|
| `INVALID_CREDENTIALS` | 401 | unknown DNI, wrong password or inactive user (indistinguishable on purpose) |
| `TOO_MANY_ATTEMPTS` | 429 | 5 failures in 15 minutes for a DNI; `Retry-After` says how long |
| `CURRENT_PASSWORD_INCORRECT` | 400 | wrong current password when changing it (400, not 401) |
| `VALIDATION_FAILED` | 400 | bad fields; `details` lists `{field, code}` |
| `UNAUTHORIZED` | 401 | missing, invalid or expired token; inactive user; old password version |

## Not here yet
Creating and managing clients, gyms, branches and team members (plan 02).
