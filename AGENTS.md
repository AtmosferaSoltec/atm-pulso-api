# atm-pulso-api: project rules

Read this file fully before changing anything. It is the single source of truth for every
AI agent (Claude Code, Codex, Gemini, ...) and for humans. `CLAUDE.md` and `GEMINI.md` only
point here. When a rule changes, change it here.

## 1. What this service is

The backend of **Pulso**, Atmósfera's gym administration product. The web client is
`atm-pulso-web` (React, a separate repo). This API is **independent**: it follows the way
`atm-orbita-api` is built, but shares no code and no database with it or with any other project.

Pulso serves many gyms at once (**multi-tenant**). Identity belongs to **atm-auth-api**: an auth *client*
(the customer) is an *organization*, and each of its *companies* with Pulso enabled is a *gym*. Gyms have
*branches*. Members, payments, access control and day passes always belong to one organization.

Business rules that shape the code:

- **There are no plans or limits in the API.** Gyms are companies created in console-web; branches are
  created, renamed and removed **only by the platform superadmin** from console-web (`/api/v1/admin`),
  because Atmósfera charges per branch. A gym's identity (name, RUC, colour, later the logo) is set from
  the console too. Gym admins never create gyms or branches. Anything commercial is handled outside the system.
- The member directory is shared by every gym and branch of the same organization. Access between
  branches is a per-gym rule set by that gym's admin.
- Only two roles exist, `admin` and `receptionist` (the `app_role` of the token). An admin creates every
  user of the current gym (admins and reception); there is no owner and no Pulso superadmin.

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
| administrador / recepción | `admin` / `receptionist` |

A document lookup (identity-api) answers split names. Pulso stores a person's name as one
`full_name`, exactly as the web captures it (members, users, day pass customers).

## 3. Stack

Go (version of `go.mod`), **Gin** (HTTP), **pgx v5** (`pgxpool`), **goose** (SQL migrations),
`log/slog` JSON logs, PostgreSQL (shared container `atm-postgres` on the VPS and
`atmosfera-postgres` locally; own database `pulso`, own schema `pulso`, own role `pulso_app`).
`golang-jwt/jwt/v5` verifies the RS256 tokens of atm-auth-api (JWKS). There is no password code here.
`testify` for tests. No ORM. Add a dependency only when needed and say why.

Queries are plain SQL with pgx in `adapters/postgres`. Move to **sqlc** (as `atm-orbita-api`
does) when a module has enough queries to justify it; do not mix both inside one module.

## 4. Architecture: modular monolith, hexagonal

One binary, one deploy. Each module is independent as if it were a service.

```
cmd/api/main.go          wiring only: config, modules, routes (subcommands: migrate, healthcheck)
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

**Identity is atm-auth-api's.** This API has no login, no password, no refresh token and no session.

- The web signs in at atm-auth-api (`POST /v1/login` with `system: "pulso"`), refreshes there, and sends
  the access token as `Authorization: Bearer`. The API verifies it locally: RS256 only, keys from
  `AUTH_API_URL/.well-known/jwks.json` (cached 10 minutes, refetched on an unknown key id, throttled),
  `iss == AUTH_ISSUER`, expiry.
- A **person's** token (`role: user`) must carry `system == AUTH_SYSTEM_CODE` (`pulso`), a `tenant` (the
  company = the current gym) and a `client`; its `app_role` must be `admin` or `receptionist`. Otherwise
  `403 NO_ACCESS`. `tenant_ruc` is informational and never required (a company may have no RUC).
- The **platform superadmin's** token (`role: superadmin`, no system, no company) is accepted only on
  `/api/v1/admin/*`; a person's token is `403 FORBIDDEN` there, and the superadmin's token is
  `403 FORBIDDEN` everywhere else. The console is the only client of `/admin`.
- On every request the `auth.Principal` (user, organization, gym, role, branches) is rebuilt from the
  verified token and Pulso's own records, **never from a body, URL or query**: the organization by
  `organizations.auth_client_id`, the gym by `gyms.auth_tenant_id` (both created on first sight, the gym with
  a default branch), the user by `(gym, auth_user_id)` (created on first sight; the role follows the token).
  `users.is_active = false` answers `403 USER_INACTIVE`. Deactivating or removing access in the console shows
  at the next token refresh.
- Roles: `admin` reaches every branch of the **current gym** and manages its team and rules;
  `receptionist` reaches the branches in `user_branches`. Switching gym means asking atm-auth-api for a token
  of another company. The member directory stays organization-wide.
- The team page calls atm-auth-api's **service members API** with Pulso's service token
  (`AUTH_CLIENT_SECRET`); Pulso decides who may do what (admins only), atm-auth-api executes. Nothing in
  Pulso hashes or stores a password.

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
- **There is no seed and no user-creation logic in the application, on purpose.** People and companies are
  created in atm-auth-api (console-web); Pulso records them when their tokens arrive. Never add code that
  creates users with known passwords, and never commit one.
- Tests build their own data (see `docs/testing.md`) and delete it afterwards.

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
- Tests never call the real identity or auth API; they use `identity.Fake`, `authapi.Fake` or `httptest`, and mint RS256 tokens with `testsupport.Tokens`.
- Tests create the data they need with helpers (`internal/integration/fixtures_test.go` is the
  model) and remove it when they end; nothing is shared and nothing is seeded.
- Keep changes small and in the layout above. Do not reopen decisions in this file without asking the owner.

## 11. Implementation plans and who does what

The plans that turn this API (and the web) into the real product are in `docs/`. **Start at
[docs/README.md](docs/README.md)**: it says what to read, in what order, how to work and when a
plan is done.

Boundaries for whoever implements them (human or AI):

- Work **locally only**: local PostgreSQL, local API, local web. Never connect to the production
  server, never read or write its `.env`, never push to `production` and never deploy. When all
  the plans are done, the owner's assistant deploys (docs/plans/06-hardening-and-release.md).
- Commit to `main` locally in small Conventional Commits. Do not push unless the owner asks.
- Migrations `00001` and `00002` are already applied in production: **never edit them**; every
  schema change is a new migration file.
- If a rule is missing or ambiguous, ask the owner; do not invent business rules.
