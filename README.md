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
scripts/dev.sh seed-dev     # DNI 10000001..10000005, password Pulso2026! (same as the web demo)
scripts/dev.sh              # http://localhost:3001/api/v1/health
```

```sh
curl -s localhost:3001/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"document_number":"10000002","password":"Pulso2026!"}'
```

The demo DNIs match the web prototype: `10000001` superadmin, `10000002` owner of Grupo Atlas,
`10000005` admin of Atlas Fitness, `10000003` receptionist, `10000004` owner of Gimnasio Central.

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

## Deploying

Pushing to `production` (after `develop` → `main`) runs `.github/workflows/deploy.yml`: check →
build image on GitHub (GHCR) → the VPS pulls it. It uses the same repository secrets as the
other projects (`SSH_HOST`, `SSH_USER`, `SSH_KEY`).

**One-time setup on the VPS**, before the first deploy:

1. Create the database (as the PostgreSQL superuser):
   ```sh
   docker exec -i atm-postgres psql -v ON_ERROR_STOP=1 -U postgres \
     -v app_password="'<PULSO_DB_PASSWORD>'" -d postgres < scripts/provision-db.sql
   ```
2. Create the folder: `mkdir -p /opt/pulso-api && chown deploy:deploy /opt/pulso-api`.
3. Create `/opt/pulso-api/.env` from `.env.example` with production values:
   `APP_ENV=production`, `DATABASE_URL=postgres://pulso_app:…@atm-postgres:5432/pulso?sslmode=disable&search_path=pulso`,
   a new `JWT_ACCESS_SECRET`, `CORS_ORIGIN` empty, `IDENTITY_API_URL=http://identity-api:8080` and the key from step 4.
4. Give Pulso its own key in `atm-identity-api` (printed once):
   `docker exec identity-api /app/identity-api create-client-system pulso "Pulso"`
   (use the binary path and command your identity deploy documents).
5. Create the superadmin (there is no registration). The password is read from an
   environment variable, never from the command line, so it stays out of the shell history:
   ```sh
   cd /opt/pulso-api && docker compose run --rm -e SUPERADMIN_PASSWORD='<a long password>' \
     app create-superadmin <dni> "<full name>"
   ```
   (Locally: `SUPERADMIN_PASSWORD=... scripts/dev.sh create-superadmin <dni> "<full name>"`.)

The container is `pulso-api`, on the shared `atm-network`, published only on `127.0.0.1:3081`.
`pulso-web`'s nginx will proxy `/api/` to `http://pulso-api:3001` (same origin: no CORS, and the
token is never stored in a cookie).
