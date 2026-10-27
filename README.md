# atm-pulso-api

Backend of **Pulso**, Atmósfera's gym administration product (Go + Gin + PostgreSQL). The web
client lives in `atm-pulso-web`. This project is independent: it is built the way
`atm-orbita-api` is, but shares no code or database with it.

Rules and architecture: [AGENTS.md](AGENTS.md). Roadmap: [docs/plans/00-roadmap.md](docs/plans/00-roadmap.md).
Contract: [docs/openapi.yaml](docs/openapi.yaml).

## What works today (plan 01)

| Endpoint | Who | What |
|---|---|---|
| `GET /api/v1/health`, `/health/ready` | public | liveness and database check |
| `POST /api/v1/auth/login` | public | DNI + password → access token (stateless, no sessions) |
| `GET /api/v1/me` | signed in | the user and where they work |
| `PATCH /api/v1/me` | signed in | edit own name and phone |
| `POST /api/v1/me/password` | signed in | change own password; answers a fresh token |
| `GET /api/v1/documents/persons/{dni}` | signed in | name of a DNI, through `atm-identity-api` |
| `GET /api/v1/documents/companies/{ruc}` | signed in | legal name and address of a RUC |

Carné de extranjería has no lookup (`atm-identity-api` only resolves DNI and RUC): the web keeps
it manual.

## Run it locally

You need Go (see `go.mod`) and the shared local PostgreSQL (`atmosfera-postgres`).

```sh
# 1. Create the role, database and schema (once)
docker exec -i atmosfera-postgres psql -v ON_ERROR_STOP=1 -U postgres \
  -v app_password="'a-local-password'" -d postgres < scripts/provision-db.sql

# 2. Configure
cp .env.example .env        # fill DATABASE_URL (same password) and JWT_ACCESS_SECRET
                            # (openssl rand -base64 48). Leave IDENTITY_API_KEY empty:
                            # lookups then answer invented data.

# 3. Migrate, load the demo accounts, run
scripts/dev.sh migrate up
go run ./scripts/first-superadmin     # prints the INSERT of your first user; run it in psql (see below)
scripts/dev.sh                        # http://localhost:3001/api/v1/health
```

There is **no seed**: the database starts empty. Create your first user with the tool below, then
everything else (clients, gyms, branches, team) is created through the API itself, as in production.

## The first superadmin (manual, on purpose)

The application has no seed and no "create superadmin" code. The first user is inserted by hand:

```sh
go run ./scripts/first-superadmin
```

It asks for the DNI, the full name and a password (hidden, twice) and **prints** an `INSERT` with the
argon2id hash; it never touches a database and the password never leaves your machine. Run the
statement yourself:

```sh
# local database
go run ./scripts/first-superadmin | docker exec -i atmosfera-postgres psql -v ON_ERROR_STOP=1 -U postgres -d pulso
# production (when the time comes)
go run ./scripts/first-superadmin | ssh atmosfera 'docker exec -i atm-postgres psql -v ON_ERROR_STOP=1 -U postgres -d pulso'
```

Then sign in: `POST /api/v1/auth/login` with that DNI and password.

## Tests

```sh
go test ./...                 # unit tests; integration tests are skipped without a database
TEST_DATABASE_URL='postgres://USER:PASS@127.0.0.1:5432/pulso_test?sslmode=disable' go test -race ./...
```

For the integration tests use a **throwaway** database, not your working one (they create and
delete their own rows, but they also load the demo accounts). For example:

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
   (`...@atm-postgres:5432/pulso?sslmode=disable&search_path=pulso`), a long `JWT_ACCESS_SECRET`,
   `CORS_ORIGIN=https://pulso.atmosferast.com`, `IDENTITY_API_URL=http://identity-api:8080` and the key.
3. Pulso's own key in `atm-identity-api` (printed once):
   `docker exec identity-api /app/identity-api create-client-system pulso "Pulso"`.
4. The nginx site `pulso-api.atmosferast.com` → `127.0.0.1:3081` (the wildcard origin certificate).

**After the first deploy**, insert the first superadmin by hand with `go run ./scripts/first-superadmin`
(see "The first superadmin" above). There is no command for it in the container.

The container is `pulso-api`, on the shared `atm-network`, published only on `127.0.0.1:3081`. The web
calls the API across origins; the token travels in the `Authorization` header, never in a cookie.
