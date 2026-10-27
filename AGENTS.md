# atm-pulso-api: project rules

Read this file fully before changing anything. It is the single source of truth for every
AI agent (Claude Code, Codex, Gemini, ...) and for humans. `CLAUDE.md` and `GEMINI.md` only
point here. When a rule changes, change it here.

## 1. What this service is

The backend of **Pulso**, Atmósfera's gym administration product. The web client is
`atm-pulso-web` (React, a separate repo). This API is **independent**: it follows the way
`atm-orbita-api` is built, but shares no code and no database with it or with any other project.

Pulso serves many gyms at once (**multi-tenant**): an *organization* is a client (its owner),
it owns *gyms*, and each gym has *branches*. Members, payments, access control and day passes
(to come, see `docs/plans/00-roadmap.md`) always belong to one organization.

Business rules that shape the code:

- **Pulso is free within limits**: 1 gym and up to 3 branches per gym per client. Anything
  beyond is agreed with Atmósfera, which creates it (and charges for it). Owners and admins
  can never create gyms or branches themselves. The limits live in one place, in code, when
  that module is built.
- The member directory is shared by every gym and branch of the same owner. Access between
  branches is a per-gym rule set by that gym's administrator.
- Only an owner creates admins; an admin creates reception staff.

## 2. Language: everything in English

Code, identifiers, tables, columns, folders, files, commits, comments, docs, log messages,
error codes: **English only**. The only Spanish allowed is what an end user reads: the
`message` of an API error (a readable fallback; the web picks its own text from the stable
`code`), and future emails or PDFs. Peruvian concepts keep a neutral English name:

| Concept | Name used |
|---|---|
| DNI / RUC / carné de extranjería | `document_number` (+ `document_type` where needed) |
| nombres / apellidos | `first_names` / `paternal_surname`, `maternal_surname` |
| razón social | `legal_name` |
| boleta | `receipt` |
| sede | `branch` |
| propietario / administrador / recepción | `owner` / `admin` / `receptionist` |

Names are always stored split, never as one `full_name` for people from a lookup; a `full_name`
may be computed in a response. (A user's own display name is stored as `full_name`.)

## 3. Stack

Go (version of `go.mod`), **Gin** (HTTP), **pgx v5** (`pgxpool`), **goose** (SQL migrations),
`log/slog` JSON logs, PostgreSQL (shared container `atm-postgres` on the VPS and
`atmosfera-postgres` locally; own database `pulso`, own schema `pulso`, own role `pulso_app`).
JWT HS256 (`golang-jwt/jwt/v5`) and argon2id (`golang.org/x/crypto/argon2`) for auth.
`testify` for tests. No ORM. Add a dependency only when needed and say why.

Queries are plain SQL with pgx in `adapters/postgres`. Move to **sqlc** (as `atm-orbita-api`
does) when a module has enough queries to justify it; do not mix both inside one module.

## 4. Architecture: modular monolith, hexagonal

One binary, one deploy. Each module is independent as if it were a service.

```
cmd/api/main.go          wiring only: config, modules, routes (subcommands: migrate, healthcheck, seed-dev)
internal/
  platform/              shared infrastructure with NO business rules
    config database httpserver logger clock ids middleware apierror auth health testsupport
  integrations/          external services behind an interface (identity: atm-identity-api client)
  modules/<module>/
    domain/              entities, value objects, rules. Plain Go: no gin, pgx, net/http
    app/                 use cases + the ports (interfaces) they need
    adapters/http        Gin handlers + DTOs; translate HTTP <-> use cases
    adapters/postgres    implement the ports with SQL
    adapters/<other>     crypto, external clients...
    module.go            builds the module by hand and exposes its routes
    README.md            what it does, its ports and its errors
  archtest/              tests that enforce these rules
db/migrations/           goose files: 000NN_<module>_<what>.sql (Up and Down)
docs/                    openapi.yaml, plans/
```

Rules (enforced by `internal/archtest`; do not negotiate them):

1. `domain` imports nothing from the project except its own module's `domain`, nor gin, pgx, net/http.
2. `app` depends on `domain` and on interfaces it declares; never on adapters, gin or pgx.
3. Adapters depend on `app`/`domain`, never the reverse. Dependency injection is by hand in
   `module.go` and `main.go`: no DI framework.
4. **A module never imports another module.** If `payments` needs something from `members`,
   `members` exposes a small interface and `main.go` wires it.
5. Each module owns its tables. No `JOIN` to another module's tables. Foreign keys across
   modules are limited to what is indispensable (ids of tenancy).
6. Handlers are thin: parse, call the use case, answer. No business rules.
7. `context.Context` first in everything that touches the database or the network. Wrap errors
   with `%w`; the domain defines typed errors and the HTTP adapter translates them.
8. Only `platform/apierror` writes JSON. Never call `c.JSON` in a handler.
9. Platform and integrations know nothing of the modules.

Isolation: a panic in a handler returns 500 and is logged, it never takes the process down.
Timeouts on the HTTP server, queries and external calls. Graceful shutdown. External
services never block the critical path of a request without a timeout.

## 5. API contract

- Prefix `/api/v1`. JSON, `snake_case`. Dates `YYYY-MM-DD`, timestamps RFC 3339 UTC.
- Success `{ "data": ... }`; lists `{ "data": [...], "meta": { "page", "page_size", "total" } }`.
- Error `{ "error": { "code", "message", "details" } }`. `code` is stable, English,
  `SCREAMING_SNAKE_CASE`: add, never rename. `message` is Spanish.
- **Money travels as text** (`"1245.80"`), never as a JSON number. `NUMERIC(14,2)` in the database.
- Validation errors are `400 VALIDATION_FAILED` with `details: [{ "field", "code" }]`.
- Keep `docs/openapi.yaml` current in the same change as the endpoint.

## 6. Authentication and authorization (decided with the owner)

**Stateless, deliberately simple.**

- `POST /auth/login` with `document_number` (DNI) and `password`. It answers an **access token**.
- **No refresh token, no sessions table, nothing about sessions in the database.** The web keeps
  the token **in memory only**; closing or reloading the page loses it and the user signs in
  again. The token lasts `ACCESS_TOKEN_TTL_SECONDS` (default 8 h, max 24 h).
- The token carries only `sub` (user id), `pv` (password version), `iat`, `exp`, `iss`.
  **Role, organization, gym and branches are never in the token**: `auth.Require` loads the
  user from the database on every request, so deactivating a user, changing a role or changing
  a password takes effect at once.
- Changing the password bumps `users.password_version`, which kills every older token, and
  answers a fresh token so the user stays signed in.
- Login failures are indistinguishable (unknown DNI, wrong password, inactive user all answer
  `INVALID_CREDENTIALS`), and an unknown DNI still costs a hash check. After 5 failures in 15
  minutes a DNI is locked (in memory, not in the database), on top of the per-IP rate limit.
- Passwords: argon2id, minimum 8 characters.

**Scope.** The `auth.Principal` (user, organization, role, gym, branches) comes only from the
verified token and the live user record: **never from a body, URL or query**. The roles mirror
the web's `permissions.ts`:

| Role | Organization | Reaches |
|---|---|---|
| `superadmin` | none | everything, read-only |
| `owner` | its own | every gym and branch of the organization |
| `admin` | its own | one gym (`gym_id`) and its branches |
| `receptionist` | its own | the branches in `user_branches` |

**Tenant isolation is non-negotiable.** Every query that touches business data filters by
`organization_id`, taken from the Principal. Repositories take the organization as an explicit
parameter. The database backs this up with composite foreign keys and `CHECK` constraints.

## 7. Database rules

- Tables and columns: English, `snake_case`, plural table names. `TIMESTAMPTZ` in UTC.
  `NULL` over empty strings. UUID v7 ids (clients may supply them on create so a retry does
  not duplicate).
- Migrations are the only way to change the schema: one file per change, goose `Up` and `Down`,
  never edit an applied migration. The app role has no access to `public`.
- Local DB: `atmosfera-postgres`; create role/database/schema with `scripts/provision-db.sql`.

## 8. Security and privacy

- Secrets only in `.env` (git-ignored). Keep `.env.example` in sync, with no real values.
  Never put a secret in the repo, in a commit or in a log.
- Document numbers and names are personal data (Peru Law 29733): never log a full document
  number, never log request bodies or headers (they carry tokens and passwords).
- The superadmin account and its password are created by hand on the server. `seed-dev`
  (fictional accounts with a known password) **refuses to run in production**.

## 9. Branches and deployment

Exactly two branches: `main` (all work happens here; it is what is functional) and
`production` (what is deployed). **A push to `production` triggers the deploy workflow.**
`production` only ever receives commits that are already in `main` (the workflow refuses
anything else), so the flow is always `main` → `production`, and promoting is the owner's decision.

The workflow on `production` has three jobs: **check** (format, vet, lint, tests with a real
PostgreSQL, build; also verifies the commit is already in `main`), **build** (the image is built
on GitHub and published to GHCR, never on the VPS) and **deploy** (the VPS pulls it). The server
needs `/opt/pulso-api` and its `.env` (see README).

Domains: the web is `pulso.atmosferast.com` and the API is `pulso-api.atmosferast.com`, both
behind the host's nginx with the wildcard origin certificate. The browser calls the API across
origins, so `CORS_ORIGIN` must list the web's origin (`https://pulso.atmosferast.com`); the token
is sent in the `Authorization` header, never in a cookie.

## 10. Working agreements

- **One plan at a time** (`docs/plans/`). When one is done: run its tests, summarize what was
  done and ask for review before starting the next. If something is ambiguous, ask; do not
  invent business rules.
- Conventional Commits, in English, small and one per logical change.
- Update the affected documents in the same change (the plan, the module README, `openapi.yaml`).
- Verify before saying "done": `gofmt -l .`, `go vet ./...`, `go build ./...`, `go test ./...`.
  Integration tests need a migrated PostgreSQL and are skipped without `TEST_DATABASE_URL`;
  run them with: `TEST_DATABASE_URL='postgres://…/pulso_test?sslmode=disable&search_path=pulso' go test -race ./...`
- Tests never call the real identity API; they use `identity.Fake` or `httptest`.
- Keep changes small and in the layout above. Do not reopen decisions in this file without asking the owner.
