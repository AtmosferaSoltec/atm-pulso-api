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

## Password onboarding

`must_change_password` is loaded from the live user record and returned in the profile.
Required changes return `data.login_required = true` without a token. Authentication middleware
blocks all other routes except reading the profile and changing the password with
`403 PASSWORD_CHANGE_REQUIRED`. Password updates use an expected version to avoid overwriting
concurrent manager resets. Product rules are in `docs/domain/business-rules.md` §15.

## Directory and tenancy

`GET /catalog` returns the shared organization directory with effective day-pass prices and
principal-derived branch accessibility. Superadmin GET inspection requires a validated
`X-Organization-Id`; other roles cannot override their live organization. `Module.Branches()`
exposes a structural `BranchInfo` port for future modules. Composite foreign keys enforce branch
and receptionist tenant isolation. Hash and verification operations share four concurrency slots.

## Gym identity and logo

The identity of a gym (`display_name`, `brand_color`, logo) is set **only by the superadmin**, who may
change nothing else of the gym; owners and admins change prices and branch sharing but get `403` if
they send an identity field. The logo is one per gym (repeat the upload for each gym that shares it).
`PUT /gyms/{id}/logo` takes the raw image (PNG, JPEG or WebP, up to 512 KB, checked by content; SVG is
refused) and stores it in Cloudflare R2 through the `app.LogoStore` port, implemented by
`platform/objectstore` through `adapters/logos`. The bucket is private. The database keeps only `gyms.logo_key`;
the catalog answers `logo_url`, a path like `/gyms/{id}/logo?v=<version>`, and `GET /gyms/{id}/logo` (public, no
token, because an `<img>` cannot send one) reads the file from R2 and streams it, `immutable` when `v` is the
current version. Every upload gets a new key (`logos/<gym id>/<uuid>.<ext>`), so files never change, and the
previous file is deleted afterwards (a failed delete only leaves an orphan file and is logged). Without the
R2 variables (development) the endpoints answer `503 STORAGE_UNAVAILABLE`.
