# atm-pulso-api

Backend of **Pulso**, Atmósfera's gym administration product (Go + Gin + PostgreSQL). The web
client lives in `atm-pulso-web`. This project is independent: it is built the way
`atm-orbita-api` is, but shares no code or database with it.

Rules and architecture: [AGENTS.md](AGENTS.md). Roadmap: [docs/plans/00-roadmap.md](docs/plans/00-roadmap.md).
Contract: [docs/openapi.yaml](docs/openapi.yaml).

## Identity: atm-auth-api

Pulso has **no login, no passwords and no sessions**. People sign in at `atm-auth-api` (the web calls it
directly: `POST /v1/login` with `system: "pulso"`) and send the access token (RS256) to this API as
`Authorization: Bearer`. The API only verifies it with the public keys at
`AUTH_API_URL/.well-known/jwks.json` (cached 10 minutes), checks issuer and expiry, and rebuilds who is
asking on every request.

| In atm-auth-api (console) | In Pulso |
|---|---|
| **Client** (the customer) | Only a hint: its companies start in the same **Group** (an internal label of the superadmin, not a user, no owner) |
| **Company** (tenant) of that client with Pulso enabled | **Gym** (a brand); the token's company is the *current gym*. Members, payments and receipts belong to the gym; nothing is shared between gyms |
| Person with access to the company, role `admin` or `receptionist` | **User** of that gym (one record per person per gym) |
| Platform superadmin | `/admin/*` only: groups, gym look and group, branches |

Groups, gyms (with a default branch "Sede principal") and users are recorded the first time a
token for them is seen; the superadmin's `GET /admin/groups` also syncs every company with Pulso
enabled. Branches are created **only by the superadmin from console-web** (Atmósfera charges per branch);
a gym admin can never add one. Roles: `admin` (every branch of the current gym, team, prices) and
`receptionist` (assigned branches). The team page uses atm-auth-api's *service* members API with Pulso's own
credential (`AUTH_CLIENT_SECRET`), so Pulso decides that only admins manage people and no `manage_users`
flag is needed; switching gym means asking atm-auth-api for a token of another company.

## What works today

The complete list, with bodies, responses and error codes, is [docs/openapi.yaml](docs/openapi.yaml);
a test fails if the code and the contract differ. By area:

| Area | Endpoints | Who |
|---|---|---|
| Health | `GET /health`, `/health/ready` | public |
| Profile | `GET/PATCH /me` | signed in |
| Lookups | `GET /documents/persons/{dni}`, `/documents/companies/{ruc}` | signed in (through `atm-identity-api`) |
| Directory | `GET /catalog` | admin, reception |
| Gym settings | `PATCH /gyms/{id}` (day pass price, membership prices, branch sharing) | admin of that gym |
| Team | `/users` and `/users/{id}`, `/activate`, `/deactivate`, `/password` | admin |
| Members | `/members`, `/members/{id}`, `/members/{id}/renewals`, `/members/{id}/receipts`, `/receipts/{id}` | admin, reception |
| Access | `POST /access/checks`, `GET /access/events`, `GET /access/summary` | admin, reception |
| Day passes | `/day-passes`, `/day-passes/{id}` | admin, reception |
| Reports | `GET /payments`, `/payments/export`, `/dashboard` | admin (reception: the list without totals) |
| Platform | `GET`/`POST /admin/groups`, `PATCH`/`DELETE /admin/groups/{id}`, `PATCH /admin/gyms/{id}`, `POST /admin/gyms/{id}/branches`, `PATCH`/`DELETE /admin/branches/{id}` | superadmin (console-web) |

Carné de extranjería has no lookup (`atm-identity-api` only resolves DNI and RUC): the web keeps
it manual.

## Run it locally

You need Go (see `go.mod`), the shared local PostgreSQL (`atmosfera-postgres`) and, to sign in for
real, a local `atm-auth-api` with the system `pulso`, a client, a company and a person.

```sh
# 1. Create the role, database and schema (once)
docker exec -i atmosfera-postgres psql -v ON_ERROR_STOP=1 -U postgres \
  -v app_password="'a-local-password'" -d postgres < scripts/provision-db.sql

# 2. Configure
cp .env.example .env        # fill DATABASE_URL (same password), AUTH_API_URL and AUTH_ISSUER of your
                            # local atm-auth-api. Leave IDENTITY_API_KEY and AUTH_CLIENT_SECRET empty:
                            # lookups answer invented data and company names fall back to the RUC.
                            # With AUTH_CLIENT_SECRET set, STORAGE_API_URL (atm-storage-api) lets /me, /catalog and
                            # receipts carry logo_url; `scripts/dev-all.sh start` brings the whole local stack up
                            # (auth :3130, storage :8084, identity :8086, pulso-api :3001, web :5173, console :5174).

# 3. Migrate and run
scripts/dev.sh migrate up
scripts/dev.sh                        # http://localhost:3001/api/v1/health
```

There is **no seed and no user table to fill**: the database starts empty and groups, gyms and
people appear when atm-auth-api's tokens reach the API. To try the API without atm-auth-api, mint tokens
the way the integration tests do (`internal/platform/testsupport/tokens.go`).

## Tests

```sh
go test ./...                 # unit tests; integration tests are skipped without a database
TEST_DATABASE_URL='postgres://USER:PASS@127.0.0.1:5432/pulso_test?sslmode=disable' go test -race ./...
```

For the integration tests use a **throwaway** database, not your working one (they create and
delete their own rows). For example:

```sh
docker run -d --name pulso-test-pg -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:17-alpine
docker exec pulso-test-pg psql -U postgres -c "CREATE DATABASE pulso_test"
export TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:55432/pulso_test?sslmode=disable'
```

Also: `gofmt -l .`, `go vet ./...` and `golangci-lint run` (the CI runs all of them).

## Domains and branches

| What | Where |
|---|---|
| Web | https://pulso.atmosferast.com |
| API | https://pulso-api.atmosferast.com (`/api/v1`) |

Two branches only: `main` (where the work happens) and `production`. **Pushing to `production`
runs `.github/workflows/deploy.yml`**, which refuses a commit that is not already in `main`:
check (format, vet, lint, tests with a real PostgreSQL) → build the image on GitHub (GHCR) → the
VPS pulls it, applies the migrations and restarts. It uses the same repository secrets as the
other projects (`SSH_HOST`, `SSH_USER`, `SSH_KEY`).

## Deploying

**The server is already prepared** (database, folder, `.env`, identity key and nginx). For the record,
this is what exists there and how to rebuild it if it is ever lost:

1. Database, schema and role `pulso_app`, from `scripts/provision-db.sql` (as the PostgreSQL superuser):
   ```sh
   docker exec -i atm-postgres psql -v ON_ERROR_STOP=1 -U postgres \
     -v app_password="'<PULSO_DB_PASSWORD>'" -d postgres < scripts/provision-db.sql
   ```
2. `/opt/pulso-api` owned by `deploy`, with `/opt/pulso-api/.env` (mode 600, never in git) holding the
   variables of `.env.example` with production values: `APP_ENV=production`, the database URL
   (`...@atm-postgres:5432/pulso?sslmode=disable&search_path=pulso`), `AUTH_API_URL=http://auth-api:8080`,
   `AUTH_ISSUER` (the `iss` of atm-auth-api's tokens), `AUTH_SYSTEM_CODE=pulso`, `AUTH_CLIENT_SECRET`
   (console → system `pulso` → "Generar credencial"), `CORS_ORIGIN=https://pulso.atmosferast.com`,
   `IDENTITY_API_URL=http://identity-api:8080` and the key.
3. Pulso's own key in `atm-identity-api` (printed once):
   `docker exec identity-api /app/identity-api create-client-system pulso "Pulso"`.
4. The nginx site `pulso-api.atmosferast.com` → `127.0.0.1:3081` (the wildcard origin certificate).

**After the first deploy**, in console-web: create the system `pulso` and its roles `admin` and
`receptionist`, enable it for each company, and give the people their access. Migration `00012` wipes every Pulso row: it was applied before anybody used the product.
console-web reaches this API through `deploy/host-nginx/console-pulso-api.location`
(`console.atmosferast.com/pulso-api/`), with the superadmin's token.

The container is `pulso-api`, on the shared `atm-network`, published only on `127.0.0.1:3081`. The web
calls the API across origins; the token travels in the `Authorization` header, never in a cookie.
