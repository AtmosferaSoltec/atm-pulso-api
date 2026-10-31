-- +goose Up
-- Corrections to a membership period that has not ended yet (PATCH /members/{id}/memberships/{id}). The
-- receipt number and the payment row are kept (a correction, never a delete); every change leaves one row
-- here with who made it and the values before and after, so the history of a receipt can be reconstructed.
ALTER TABLE memberships ADD COLUMN amended_at timestamptz;

CREATE TABLE membership_amendments (
    id            uuid PRIMARY KEY,
    gym_id        uuid        NOT NULL REFERENCES gyms (id) ON DELETE CASCADE,
    membership_id uuid        NOT NULL,
    payment_id    uuid,
    changed_by    uuid        NOT NULL REFERENCES users (id),
    changed_at    timestamptz NOT NULL DEFAULT now(),
    before        jsonb       NOT NULL,
    after         jsonb       NOT NULL,
    CONSTRAINT membership_amendments_membership_fkey FOREIGN KEY (membership_id, gym_id)
        REFERENCES memberships (id, gym_id) ON DELETE CASCADE
);
CREATE INDEX membership_amendments_membership_idx ON membership_amendments (membership_id, changed_at);

-- +goose Down
DROP TABLE membership_amendments;
ALTER TABLE memberships DROP COLUMN amended_at;
