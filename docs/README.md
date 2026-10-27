# Pulso: documentation and implementation plans

This folder is written for **whoever implements the real product** (a person or another AI). It
says what to build, in what order, what to read first, how to verify and when something is done.
The same is mirrored for the web in `atm-pulso-web/docs/`.

## The two repositories

| Repo | Local path | What it is |
|---|---|---|
| **API** (this repo) | `/Users/jmaldonado/Documents/Atmosfera/atm-pulso-api` | Go + Gin + PostgreSQL backend. **Source of truth for the contract and the rules.** |
| **Web** | `/Users/jmaldonado/Documents/Atmosfera/atm-pulso-web` | React + TypeScript. Today it is a **prototype that runs on fictional data in memory**; the plans replace that with real calls to this API. |

The web prototype is the **reference for behavior and screens**: its code (and the screenshots the
owner has seen) show how each feature must work. The API must enforce every rule on the server;
the web only hides buttons, it is never the guard.

## Read first, in this order

1. `AGENTS.md` of **each** repo you touch. Mandatory, it overrides your habits.
2. [domain/business-rules.md](domain/business-rules.md): every rule of the product, with where the
   prototype implements it.
3. [domain/data-model.md](domain/data-model.md): the schema that exists and the one to build.
4. [testing.md](testing.md): the local environment, the fixtures and what every plan must test.
5. The plan you are about to do (below) and its counterpart in the other repo.
6. `docs/openapi.yaml`: the contract. You change it in the same commit as the endpoint.

## Order of work

| Order | API plan | Web plan | Depends on |
|---|---|---|---|
| 1 | [API-02 Clients, gyms, branches, team](plans/02-clients-gyms-team.md) | [WEB-01 Foundation, login, profile](../../atm-pulso-web/docs/plans/01-foundation-auth-profile.md) | API-01 (done) |
| 2 | | [WEB-02 Superadmin, gyms, team, rules, prices](../../atm-pulso-web/docs/plans/02-platform-gyms-team.md) | API-02 |
| 3 | [API-03 Members, enrollment, receipts, payments](plans/03-members-and-payments.md) | [WEB-03 Members and receipts](../../atm-pulso-web/docs/plans/03-members-and-receipts.md) | API-02 |
| 4 | [API-04 Access control and day passes](plans/04-access-and-day-passes.md) | [WEB-04 Access and day passes](../../atm-pulso-web/docs/plans/04-access-and-day-passes.md) | API-03 |
| 5 | [API-05 Reports, dashboard and export](plans/05-reports.md) | [WEB-05 Payments and dashboard](../../atm-pulso-web/docs/plans/05-payments-and-dashboard.md) | API-04 |
| 6 | [API-06 Hardening and release](plans/06-hardening-and-release.md) | [WEB-06 Cleanup and release](../../atm-pulso-web/docs/plans/06-cleanup-and-release.md) | everything |

WEB-01 can start right away (login, profile and document lookup already exist in the API). Every
other web plan needs its API plan finished and merged first, never the other way round. API-01 is
done: see [plans/01-foundation-and-auth.md](plans/01-foundation-and-auth.md).

## How to work (rules for the implementer)

1. **One plan at a time.** Finish it (tests green, docs updated), summarize what was done and what
   you decided, and **ask the owner for review** before starting the next one.
2. **Local only.** Local PostgreSQL, local API, local web. Never connect to the production server,
   never read or write its `.env`, never push to `production`, never deploy. The owner's assistant
   deploys when everything is finished (see API-06).
3. **No seed, no demo users, no "create superadmin" code.** The first superadmin is inserted by hand
   (`scripts/first-superadmin`, README). Tests create and delete their own data.
4. **Do not invent business rules.** If something is missing or two documents disagree, stop and
   ask the owner. Each plan lists "Questions for the owner" with the recommended default: use the
   default only if the owner told you to proceed.
5. **Commits:** Conventional Commits in English, small, one logical change each, on `main`. Do not
   push unless the owner asks. Never commit a secret or a `.env`.
6. **Migrations are append-only.** `00001` and `00002` are already applied in production: never
   edit them. Every schema change is a new numbered file with `Up` and `Down`.
7. **Language:** all code, comments and docs in English; Spanish only for what an end user reads
   (the web's UI text, and the API error `message`).
8. **Security is part of every plan**, not a last step: tenant isolation, role checks on the
   server, no secrets or personal data in logs. Each plan has a security checklist.

## Definition of done (every plan)

- [ ] `gofmt -l .` empty, `go vet ./...`, `golangci-lint run` (0 issues), `go test -race ./...` green
      **with a real PostgreSQL** (see testing.md). The architecture tests (`internal/archtest`) pass.
- [ ] Every endpoint of the plan is in `docs/openapi.yaml` with its errors; the module README is updated.
- [ ] Tests cover the plan's list, **including the tenant-isolation and role-matrix tests**.
- [ ] A manual smoke run against the local API with `curl` (commands in the plan) works.
- [ ] The web counterpart can be built against the contract without asking anything else.
- [ ] A short summary for the owner: what was built, decisions taken, anything left open.
