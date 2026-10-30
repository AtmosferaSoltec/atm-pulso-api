-- +goose Up
-- Identity moves to atm-auth-api. An organization is an auth Client (the customer), a gym is an
-- auth Tenant (a company of that client) and users are the people auth lets into Pulso, so the
-- password machinery goes away. Nobody used the product yet: every row is disposable and is wiped.

TRUNCATE organizations, users CASCADE;

DROP TABLE user_gyms;

ALTER TABLE users DROP CONSTRAINT users_role_scope_check;
ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users DROP CONSTRAINT users_document_number_key;
ALTER TABLE users
    DROP COLUMN password_hash,
    DROP COLUMN password_version,
    DROP COLUMN must_change_password,
    ALTER COLUMN organization_id SET NOT NULL,
    -- One row per person per gym: the same person can work for several companies of a client,
    -- with a different role and branches in each. auth_user_id is the person in atm-auth-api.
    ADD COLUMN gym_id uuid NOT NULL,
    ADD COLUMN auth_user_id uuid NOT NULL,
    ADD CONSTRAINT users_role_check CHECK (role IN ('admin', 'receptionist')),
    ADD CONSTRAINT users_gym_organization_fkey FOREIGN KEY (gym_id, organization_id)
        REFERENCES gyms (id, organization_id) ON DELETE CASCADE,
    ADD CONSTRAINT users_gym_auth_user_key UNIQUE (gym_id, auth_user_id),
    ADD CONSTRAINT users_document_number_key UNIQUE (gym_id, document_number);
CREATE INDEX users_gym_idx ON users (gym_id);

ALTER TABLE organizations
    DROP COLUMN contact_document_number,
    DROP COLUMN contact_name,
    DROP COLUMN contact_email,
    DROP COLUMN contact_phone,
    ADD COLUMN auth_client_id uuid NOT NULL,
    ADD CONSTRAINT organizations_auth_client_key UNIQUE (auth_client_id);

ALTER TABLE gyms
    DROP COLUMN logo_key,
    ADD COLUMN auth_tenant_id uuid NOT NULL,
    ADD CONSTRAINT gyms_auth_tenant_key UNIQUE (auth_tenant_id);

-- +goose Down
ALTER TABLE gyms DROP CONSTRAINT gyms_auth_tenant_key, DROP COLUMN auth_tenant_id, ADD COLUMN logo_key text;

ALTER TABLE organizations
    DROP CONSTRAINT organizations_auth_client_key,
    DROP COLUMN auth_client_id,
    ADD COLUMN contact_document_number text,
    ADD COLUMN contact_name text,
    ADD COLUMN contact_email text,
    ADD COLUMN contact_phone text;

TRUNCATE organizations, users CASCADE;

DROP INDEX users_gym_idx;
ALTER TABLE users
    DROP CONSTRAINT users_document_number_key,
    DROP CONSTRAINT users_gym_auth_user_key,
    DROP CONSTRAINT users_gym_organization_fkey,
    DROP CONSTRAINT users_role_check,
    DROP COLUMN gym_id,
    DROP COLUMN auth_user_id,
    ALTER COLUMN organization_id DROP NOT NULL,
    ADD COLUMN password_hash text NOT NULL,
    ADD COLUMN password_version integer NOT NULL DEFAULT 1,
    ADD COLUMN must_change_password boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT users_document_number_key UNIQUE (document_number),
    ADD CONSTRAINT users_role_scope_check CHECK (
        (role = 'superadmin' AND organization_id IS NULL) OR
        (role IN ('owner', 'admin', 'receptionist') AND organization_id IS NOT NULL));
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('superadmin', 'owner', 'admin', 'receptionist'));

CREATE TABLE user_gyms (
 user_id uuid NOT NULL,
 gym_id uuid NOT NULL,
 organization_id uuid NOT NULL,
 PRIMARY KEY (user_id, gym_id),
 FOREIGN KEY (user_id, organization_id) REFERENCES users (id, organization_id) ON DELETE CASCADE,
 FOREIGN KEY (gym_id, organization_id) REFERENCES gyms (id, organization_id) ON DELETE CASCADE
);
CREATE INDEX user_gyms_gym_idx ON user_gyms (gym_id);
