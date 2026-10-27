# Plan 01: foundation and auth

**State: done, waiting for the owner's review.**

## Scope
The project skeleton, the shared platform, the tenancy and user tables, stateless login with
the user's own profile, and the document lookups through `atm-identity-api`.

## Delivered
- Skeleton in the shape of `atm-orbita-api` (no shared code): `cmd/api`, `internal/platform`,
  `internal/integrations`, `internal/modules/{accounts,documents}`, `db/migrations`, tests that
  enforce the architecture (`internal/archtest`).
- Config validated at startup (the app does not start with a bad or incomplete `.env`);
  JSON logs with `request_id`; graceful shutdown; per-IP rate limit; panic recovery.
- Schema: `organizations`, `gyms`, `branches`, `users`, `user_branches` with database-level
  constraints for tenancy and roles.
- Auth: `POST /auth/login`, `GET|PATCH /me`, `POST /me/password`. Stateless (no sessions, no
  refresh token); user loaded live on every request; password version kills old tokens.
- Documents: `GET /documents/persons/{dni}` and `/documents/companies/{ruc}` through identity-api,
  with a deterministic fake for development.
- Subcommands: `migrate`, `healthcheck`. (`seed-dev` and `create-superadmin` existed at first and were removed: the first superadmin is inserted by hand with `scripts/first-superadmin`.)
- Deploy: Dockerfile (~50 MB image), compose with a `migrate` step, GitHub workflow
  (check with a real PostgreSQL → build → deploy), `provision-db.sql`.

## Acceptance (all checked)
- [x] `gofmt`, `go vet`, `golangci-lint` clean; `go test -race ./...` green with a real PostgreSQL.
- [x] Login for the five demo roles returns the right profile and scope.
- [x] Unknown DNI, wrong password and inactive user are indistinguishable; five failures lock the DNI.
- [x] Deactivating a user and changing a password cut access immediately, with no table of sessions.
- [x] The database refuses a cross-organization admin, a role without its scope, a bad DNI and a duplicate login.
- [x] The production image starts, migrates, passes its healthcheck and refuses `seed-dev` and an incomplete `.env`.

## Pending on the server (the owner)
Everything the server needs was prepared (see "Deploying" in the README): the database and its
role, `/opt/pulso-api/.env`, Pulso's key in `atm-identity-api` and the `pulso-api.atmosferast.com`
nginx site. What is left is creating the superadmin after the first deploy.
