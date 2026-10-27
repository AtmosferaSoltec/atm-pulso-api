-- +goose Up
-- Users sign in with their DNI and a password. There is no session table: the
-- access token is a stateless JWT and it is never stored (see docs/plans/01).

CREATE TABLE users (
    id                  uuid PRIMARY KEY,
    organization_id     uuid REFERENCES organizations (id) ON DELETE CASCADE,
    -- The gym an admin manages. NULL for every other role.
    gym_id              uuid,
    document_type       text        NOT NULL DEFAULT 'dni' CHECK (document_type = 'dni'),
    document_number     text        NOT NULL CHECK (document_number ~ '^[0-9]{8}$'),
    full_name           text        NOT NULL CHECK (btrim(full_name) <> ''),
    phone               text        CHECK (phone ~ '^9[0-9]{8}$'),
    role                text        NOT NULL CHECK (role IN ('superadmin', 'owner', 'admin', 'receptionist')),
    password_hash       text        NOT NULL,
    -- Travels inside the access token. Changing the password bumps it, so every older
    -- token stops working at once (no session table needed).
    password_version    integer     NOT NULL DEFAULT 1,
    is_active           boolean     NOT NULL DEFAULT true,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),

    -- The DNI is the login: unique across the whole system.
    CONSTRAINT users_document_number_key UNIQUE (document_number),

    -- An admin's gym must belong to the admin's own organization.
    CONSTRAINT users_gym_same_organization_fkey
        FOREIGN KEY (gym_id, organization_id) REFERENCES gyms (id, organization_id) ON DELETE CASCADE,

    -- What each role must (and must not) have.
    CONSTRAINT users_role_scope_check CHECK (
        (role = 'superadmin'    AND organization_id IS NULL     AND gym_id IS NULL) OR
        (role = 'owner'         AND organization_id IS NOT NULL AND gym_id IS NULL) OR
        (role = 'admin'         AND organization_id IS NOT NULL AND gym_id IS NOT NULL) OR
        (role = 'receptionist'  AND organization_id IS NOT NULL AND gym_id IS NULL)
    )
);
CREATE INDEX users_organization_idx ON users (organization_id);

-- Branches a receptionist works in.
CREATE TABLE user_branches (
    user_id   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    branch_id uuid NOT NULL REFERENCES branches (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, branch_id)
);
CREATE INDEX user_branches_branch_idx ON user_branches (branch_id);

-- +goose Down
DROP TABLE user_branches;
DROP TABLE users;
