-- +goose Up
-- Access checks and day passes (API-04), owned by the `access` module.

CREATE TABLE day_passes (
    id                       uuid PRIMARY KEY,
    organization_id          uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    branch_id                uuid        NOT NULL,
    receipt_id               uuid        NOT NULL,
    amount                   numeric(14, 2) NOT NULL CHECK (amount >= 0 AND amount <= 1000000),
    method                   text        NOT NULL CHECK (method IN ('yape_plin', 'cash', 'card', 'transfer')),
    -- Everything about the customer is optional; kept for future promotions (personal data).
    customer_document_type   text CHECK (customer_document_type IN ('dni', 'ce')),
    customer_document_number text,
    customer_name            text,
    customer_phone           text,
    created_by               uuid        NOT NULL REFERENCES users (id),
    created_at               timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT day_passes_id_organization_key UNIQUE (id, organization_id),
    FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id),
    FOREIGN KEY (receipt_id, organization_id) REFERENCES receipts (id, organization_id) ON DELETE CASCADE
);
CREATE INDEX day_passes_branch_idx ON day_passes (organization_id, branch_id, created_at DESC);

ALTER TABLE payments ADD CONSTRAINT payments_day_pass_fkey
    FOREIGN KEY (day_pass_id, organization_id) REFERENCES day_passes (id, organization_id) ON DELETE CASCADE;

CREATE TABLE access_entries (
    id                uuid PRIMARY KEY,
    organization_id   uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    branch_id         uuid        NOT NULL,
    member_id         uuid,
    display_name      text        NOT NULL,
    document_number   text,
    allowed           boolean     NOT NULL,
    reason_code       text        NOT NULL CHECK (reason_code IN
        ('MEMBERSHIP_VALID', 'MEMBERSHIP_EXPIRED', 'MEMBERSHIP_NOT_STARTED', 'BRANCH_NOT_ALLOWED', 'NOT_REGISTERED', 'DAY_PASS')),
    kind              text        NOT NULL CHECK (kind IN ('check', 'day_pass')),
    -- End date of the membership and days left when it is about to end, to repeat an answer.
    ends_on           date,
    expiring_in_days  integer,
    checked_by        uuid        NOT NULL REFERENCES users (id),
    checked_at        timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id) ON DELETE CASCADE,
    FOREIGN KEY (member_id, organization_id) REFERENCES members (id, organization_id) ON DELETE SET NULL (member_id)
);
CREATE INDEX access_entries_branch_idx ON access_entries (branch_id, checked_at DESC);

-- +goose Down
DROP TABLE access_entries;
ALTER TABLE payments DROP CONSTRAINT payments_day_pass_fkey;
DROP TABLE day_passes;
