# Roadmap

Goal: replace the in-memory prototype (`atm-pulso-web`) with this API, one vertical slice at a time, **all locally**. The assistant that
owns deployment will deploy only when both repositories are finished.

| Plan | Scope | Web counterpart |
|---|---|---|
| 01 foundation-and-auth | Login, profile, lookups, middleware, tests (**done**) | WEB-01 |
| 02 clients-gyms-team | Catalog, clients, gyms, branches, team, gym settings, plan limits | WEB-02 |
| 03 members-and-payments | Members, enrollment, renewals, receipts, payments | WEB-03 |
| 04 access-and-day-passes | Access checks, history, day passes | WEB-04 |
| 05 reports | Payments report and export, dashboard | WEB-05 |
| 06 hardening-and-release | Contract test, security review, indexes, hand-over | WEB-06 |
| 07 groups-and-gym-scoped-members | Groups replace organizations/owners; members and series belong to a gym | WEB follow-up |

Web plans live in `../../../atm-pulso-web/docs/plans/`. Work in order; the web slice N can start as soon as API plan N is accepted.

## Fixed decisions

- Go modular monolith, hexagonal, one Postgres schema `pulso`; modules never import each other (see AGENTS.md and `internal/archtest`).
- Stateless auth: JWT with `pv`; the user is reloaded on every request; no sessions table.
- Multi-tenant by `gym_id` with composite foreign keys (was `organization_id` until plan 07); foreign resources answer 404. A **group** is only an internal label that bundles gyms.
- **No seed data and no superadmin creation in the backend.** The first superadmin is inserted by hand with `scripts/first-superadmin`.
- **No plans or limits in the product.** Only the superadmin creates gyms and branches and sets a gym's identity (name, colour, logo); commercial terms are handled outside the system.
- Identity lookups use `atm-identity-api`; in development it can be pointed at the deployed instance (read-only lookups), otherwise use `identity.Fake`.
- Gym logos are done (business-rules section 14: company profile + atm-storage-api links). Out of scope for now: billing, notifications, email, password recovery.
