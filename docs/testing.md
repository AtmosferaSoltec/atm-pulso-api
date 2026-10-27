# Testing and the local environment

All development and verification is **local**. Nothing here needs, or may use, the production server.

## Local environment

You need Go (see `go.mod`), Docker, and for the web Node and pnpm.

```sh
# Working database (data you keep between runs). Uses the shared local PostgreSQL if it is running.
docker exec -i atmosfera-postgres psql -v ON_ERROR_STOP=1 -U postgres \
  -v app_password="'a-local-password'" -d postgres < scripts/provision-db.sql
cp .env.example .env     # DATABASE_URL, JWT_ACCESS_SECRET (openssl rand -base64 48). Leave IDENTITY_API_KEY empty.
scripts/dev.sh migrate up
go run ./scripts/first-superadmin | docker exec -i atmosfera-postgres psql -v ON_ERROR_STOP=1 -U postgres -d pulso
scripts/dev.sh           # API on http://localhost:3001

# Throwaway database for the tests (they create and delete their own rows; never point them at data you care about)
docker run -d --name pulso-test-pg -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:17-alpine
docker exec pulso-test-pg psql -U postgres -c "CREATE DATABASE pulso_test"
docker exec pulso-test-pg psql -U postgres -d pulso_test -c "CREATE SCHEMA pulso"
export TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:55432/pulso_test?sslmode=disable&search_path=pulso'
go test -race ./...
```

With no `IDENTITY_API_KEY` the document lookups answer invented data from `identity.Fake`
(`00000000` and `00000000000` are "not found"). **Never test against the real identity service.** The
real lookup is checked once, by the deployer, after release.

## What kinds of tests exist

| Kind | Where | Needs a database |
|---|---|---|
| Domain rules | `modules/<m>/domain/*_test.go` | no |
| Use cases with fakes | `modules/<m>/app/*_test.go` | no |
| Adapters (SQL, crypto, clients) | `modules/<m>/adapters/**` | when SQL |
| End to end through the real router | `internal/integration/*_test.go` | yes |
| Architecture rules | `internal/archtest` | no |

Integration tests are skipped without `TEST_DATABASE_URL`; the CI runs them with a real PostgreSQL, so
**a plan is not done until they pass locally with the database up**.

## Fixtures: tests build their own data

There is no seed. `internal/integration/fixtures_test.go` is the model: `newTenant` inserts an organization
with two gyms, three branches and one user per role, and removes it when the test ends (cascade);
`newSuperadmin` does the same for a superadmin; `uniqueDocument()` hands out DNI-shaped numbers nobody else
uses. **Extend these helpers for each new module** (members, payments...) instead of inserting by hand in
every test. After a full run the test database must be empty: `select count(*) from pulso.organizations`
is 0. Check it.

Fix "today" in tests with `clock.Fixed` (Lima date) so status, month revenue and "this month" are stable.

## What every plan must test

1. **Happy path** of each endpoint through the real router, checking the exact JSON shape.
2. **Validation**: every field, with the error `code` and `details`.
3. **Role matrix**: a table-driven test with every role (superadmin, owner, admin, receptionist) against
   every endpoint of the plan, asserting allowed or the exact 403/404. The superadmin must be refused on
   every write.
4. **Tenant isolation**: create two tenants; with a user of A, try to read and write every resource of B
   by id (path, body, `branch_id`, `gym_id`, `member_id`...): the answer is **404** (or 400 for a body
   reference) and **nothing changed**. A test per endpoint that takes an id.
5. **Scope inside a tenant**: an admin cannot touch another gym of the same organization; a receptionist
   cannot touch an unassigned branch.
6. **Idempotency** where the plan says so: repeat a create with the same `id`.
7. **Concurrency** where numbering or uniqueness is involved: run N goroutines against the same endpoint
   and assert there are no duplicates and no gaps.
8. **Database constraints**: a test that inserts directly (as `TestDatabaseEnforcesTenancyAndRoleRules`)
   and proves the database rejects the invalid state, independent of the application.
9. **No sensitive data in logs**: the access log middleware must not print bodies, tokens or document
   numbers (assert on a captured logger for the new endpoints).
10. **Migrations**: all migrations apply on an empty database and `down` works for the new ones.

## Before you say "done"

```sh
gofmt -l .                      # no output
go vet ./...
golangci-lint run               # or: go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...
go test -race -count=1 ./...    # with TEST_DATABASE_URL set
docker exec pulso-test-pg psql -U postgres -d pulso_test -Atc "select count(*) from pulso.organizations"   # 0
```
