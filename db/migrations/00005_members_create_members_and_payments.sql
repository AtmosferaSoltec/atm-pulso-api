-- +goose Up
-- Members, their membership periods, receipts and payments (API-03). The tables of
-- members/memberships belong to the `members` module; receipts, counters and payments
-- are written through internal/platform/receipts by every module that sells something.

CREATE TABLE members (
    id              uuid PRIMARY KEY,
    organization_id uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    home_branch_id  uuid        NOT NULL,
    document_type   text        NOT NULL CHECK (document_type IN ('dni', 'ce')),
    document_number text        NOT NULL,
    full_name       text        NOT NULL CHECK (btrim(full_name) <> ''),
    phone           text        NOT NULL CHECK (phone ~ '^9[0-9]{8}$'),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT members_document_format CHECK (
        (document_type = 'dni' AND document_number ~ '^[0-9]{8}$') OR
        (document_type = 'ce' AND document_number ~ '^[0-9]{9,12}$')),
    CONSTRAINT members_document_key UNIQUE (organization_id, document_number),
    CONSTRAINT members_id_organization_key UNIQUE (id, organization_id),
    CONSTRAINT members_home_branch_fkey FOREIGN KEY (home_branch_id, organization_id)
        REFERENCES branches (id, organization_id)
);
CREATE INDEX members_home_branch_idx ON members (organization_id, home_branch_id);

CREATE TABLE memberships (
    id              uuid PRIMARY KEY,
    organization_id uuid        NOT NULL,
    member_id       uuid        NOT NULL,
    months          integer     NOT NULL CHECK (months BETWEEN 1 AND 24),
    starts_on       date        NOT NULL,
    ends_on         date        NOT NULL,
    amount          numeric(14, 2) NOT NULL CHECK (amount >= 0 AND amount <= 1000000),
    access_scope    text        NOT NULL CHECK (access_scope IN ('branch', 'gym', 'network')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_on > starts_on),
    CONSTRAINT memberships_id_organization_key UNIQUE (id, organization_id),
    CONSTRAINT memberships_member_fkey FOREIGN KEY (member_id, organization_id)
        REFERENCES members (id, organization_id) ON DELETE CASCADE
);
CREATE INDEX memberships_member_idx ON memberships (member_id, ends_on);

-- Branches a period opens. Always includes the member's home branch.
CREATE TABLE membership_branches (
    membership_id   uuid NOT NULL,
    branch_id       uuid NOT NULL,
    organization_id uuid NOT NULL,
    PRIMARY KEY (membership_id, branch_id),
    FOREIGN KEY (membership_id, organization_id) REFERENCES memberships (id, organization_id) ON DELETE CASCADE,
    FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id) ON DELETE CASCADE
);

CREATE TABLE receipt_counters (
    organization_id uuid    NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    kind            text    NOT NULL CHECK (kind IN ('membership', 'day_pass')),
    last_number     integer NOT NULL,
    PRIMARY KEY (organization_id, kind)
);

CREATE TABLE receipts (
    id              uuid PRIMARY KEY,
    organization_id uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    kind            text        NOT NULL CHECK (kind IN ('membership', 'day_pass')),
    number          integer     NOT NULL CHECK (number > 0),
    branch_id       uuid        NOT NULL,
    issued_on       date        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT receipts_number_key UNIQUE (organization_id, kind, number),
    CONSTRAINT receipts_id_organization_key UNIQUE (id, organization_id),
    FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id)
);

CREATE TABLE payments (
    id              uuid PRIMARY KEY,
    organization_id uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    receipt_id      uuid        NOT NULL UNIQUE,
    branch_id       uuid        NOT NULL,
    kind            text        NOT NULL CHECK (kind IN ('membership', 'day_pass')),
    method          text        NOT NULL CHECK (method IN ('yape_plin', 'cash', 'card', 'transfer')),
    amount          numeric(14, 2) NOT NULL CHECK (amount >= 0 AND amount <= 1000000),
    paid_on         date        NOT NULL,
    member_id       uuid,
    membership_id   uuid,
    day_pass_id     uuid,
    created_by      uuid        NOT NULL REFERENCES users (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (receipt_id, organization_id) REFERENCES receipts (id, organization_id) ON DELETE CASCADE,
    FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id),
    FOREIGN KEY (member_id, organization_id) REFERENCES members (id, organization_id) ON DELETE CASCADE,
    FOREIGN KEY (membership_id, organization_id) REFERENCES memberships (id, organization_id) ON DELETE CASCADE
);
CREATE INDEX payments_branch_date_idx ON payments (organization_id, branch_id, paid_on DESC);
CREATE INDEX payments_member_idx ON payments (member_id);

-- +goose Down
DROP TABLE payments;
DROP TABLE receipts;
DROP TABLE receipt_counters;
DROP TABLE membership_branches;
DROP TABLE memberships;
DROP TABLE members;
