-- +goose Up
DROP TABLE plan_agreements;

-- +goose Down
CREATE TABLE plan_agreements (
 id uuid PRIMARY KEY,
 organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('gym', 'branch')),
 gym_id uuid NOT NULL,
 branch_id uuid,
 agreed_by uuid NOT NULL REFERENCES users (id),
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY (gym_id, organization_id) REFERENCES gyms (id, organization_id) ON DELETE CASCADE,
 FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id) ON DELETE CASCADE,
 CHECK ((kind = 'gym' AND branch_id IS NULL) OR (kind = 'branch' AND branch_id IS NOT NULL))
);
CREATE UNIQUE INDEX plan_agreements_gym_key ON plan_agreements (gym_id) WHERE kind = 'gym';
CREATE UNIQUE INDEX plan_agreements_branch_key ON plan_agreements (branch_id) WHERE kind = 'branch';
