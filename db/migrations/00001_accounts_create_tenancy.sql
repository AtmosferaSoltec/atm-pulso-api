-- +goose Up
-- Tenancy model: an organization is a client (its owner), with gyms, and each gym
-- with branches. The module `accounts` owns these tables.

CREATE TABLE organizations (
    id                      uuid PRIMARY KEY,
    name                    text        NOT NULL CHECK (btrim(name) <> ''),
    contact_document_number text,
    contact_name            text,
    contact_email           text,
    contact_phone           text,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE gyms (
    id              uuid PRIMARY KEY,
    organization_id uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name            text        NOT NULL CHECK (btrim(name) <> ''),
    display_name    text,
    -- RUC. May repeat between gyms: a client can run several gyms under one RUC.
    tax_id          text,
    brand_color     text        NOT NULL DEFAULT '#17677b' CHECK (brand_color ~ '^#[0-9a-f]{6}$'),
    -- Price of a single-entry pass, set by the gym's administrator. NULL = default price.
    day_pass_price  numeric(14, 2) CHECK (day_pass_price >= 0),
    -- Whether members of this gym may enter its other branches (the admin decides).
    branch_sharing  boolean     NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    -- Lets other tables prove a gym belongs to the same organization (composite FK).
    CONSTRAINT gyms_id_organization_key UNIQUE (id, organization_id)
);
CREATE INDEX gyms_organization_idx ON gyms (organization_id);

CREATE TABLE branches (
    id         uuid PRIMARY KEY,
    gym_id     uuid        NOT NULL REFERENCES gyms (id) ON DELETE CASCADE,
    name       text        NOT NULL CHECK (btrim(name) <> ''),
    address    text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX branches_gym_idx ON branches (gym_id);

-- +goose Down
DROP TABLE branches;
DROP TABLE gyms;
DROP TABLE organizations;
