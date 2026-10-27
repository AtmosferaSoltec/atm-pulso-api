# API-06: hardening and release candidate

**Depends on:** API-02 to API-05. **Unlocks:** deploy (done by the assistant, not by you).

## Goal

Leave a version that is safe to deploy: complete contract, complete tests, clean lint, documented operations. You do **not** deploy;
you hand over a checklist.

## Tasks

1. **Contract**: `docs/openapi.yaml` describes every route, body, response and error code. Add a test that parses the file and fails if a
   registered Gin route is missing from it (and vice versa).
2. **Security review**: walk through every checklist in plans 02 to 05 and the list in AGENTS.md; fix gaps; add a regression test for each.
   Specifically verify: no endpoint trusts a client-sent `organization_id`; foreign ids answer 404; no personal data in logs; headers
   (`X-Content-Type-Options`, `Cache-Control: no-store` on authenticated responses); body size limit; request timeouts.
3. **Migrations**: run all of them up and down on an empty database and on a database that already has migrations 1 and 2 with one superadmin
   (this is the production state). Down migrations must work or be explicitly documented as irreversible.
4. **Indexes**: run `EXPLAIN` on list, search, counts and report queries with a generated volume (for example 20,000 members, 200,000 payments) and add the missing indexes.
5. **Limits**: memory under load (hash semaphore), connection pool sizes, rate limits per route group (login strict, writes moderate, reads lenient).
6. **Operations**: update README with environment variables (all of them), healthcheck, the manual first-superadmin step, backup advice and how to roll back.
7. **Full run**: `gofmt`, `go vet`, `golangci-lint run`, `go test ./... -race -count=1` with Postgres, all green and no leftover rows in the database.
8. **Hand-over report** in `docs/RELEASE.md`: what was built, migrations added (numbers and names), new environment variables (none expected), anything the deployer must do, open questions answered by the owner.

## Acceptance

All of the above, plus an end-to-end script (curl or Go test) that walks the owner journey: superadmin creates a client, owner logs in,
creates a receptionist, receptionist enrolls a member, checks them in, sells a day pass, owner reads payments and dashboard.

## Do not

Do not push, deploy, change workflows, touch the server or run anything against production. The deployer does that after the web is also finished.

## Completion — 2026-10-27

Done and described in `docs/RELEASE.md`: contract test (`TestOpenAPIListsExactlyTheRegisteredRoutes`), security headers, migrations up/down verified (empty and production state),
volume check with EXPLAIN-backed timings (member list optimized 13x), owner-journey test, README and release notes. Not done on purpose: deploy, push, workflow changes.
