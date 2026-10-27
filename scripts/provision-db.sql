-- Provisions the dedicated role, database and schema for atm-pulso-api.
-- Run once per PostgreSQL instance (the local shared container or the VPS), as the
-- superuser. It is not idempotent on purpose: if the role or the database already
-- exist it fails instead of overwriting them.
--
--   docker exec -i atmosfera-postgres psql -v ON_ERROR_STOP=1 -U postgres \
--     -v app_password="'<PULSO_DB_PASSWORD>'" -d postgres < scripts/provision-db.sql
--
-- On the VPS the container is `atm-postgres`.
--
-- Same confinement as the other Atmosfera projects: own login role, own `pulso`
-- schema, no access to `public`.

CREATE ROLE pulso_app WITH LOGIN PASSWORD :app_password;
CREATE DATABASE pulso OWNER pulso_app;

REVOKE CONNECT ON DATABASE pulso FROM PUBLIC;
GRANT CONNECT ON DATABASE pulso TO pulso_app;

\connect pulso

CREATE SCHEMA IF NOT EXISTS pulso AUTHORIZATION pulso_app;

ALTER SCHEMA public OWNER TO postgres;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
REVOKE ALL ON SCHEMA public FROM pulso_app;

ALTER ROLE pulso_app IN DATABASE pulso SET search_path TO pulso;
