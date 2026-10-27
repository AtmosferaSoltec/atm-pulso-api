-- +goose Up
ALTER TABLE branches ADD COLUMN organization_id uuid;
UPDATE branches b SET organization_id = g.organization_id FROM gyms g WHERE g.id = b.gym_id;
ALTER TABLE branches ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE branches ADD CONSTRAINT branches_id_organization_key UNIQUE (id, organization_id);
ALTER TABLE branches ADD CONSTRAINT branches_gym_organization_fkey
 FOREIGN KEY (gym_id, organization_id) REFERENCES gyms (id, organization_id) ON DELETE CASCADE;
ALTER TABLE users ADD CONSTRAINT users_id_organization_key UNIQUE (id, organization_id);
ALTER TABLE user_branches ADD COLUMN organization_id uuid;
UPDATE user_branches ub SET organization_id = u.organization_id FROM users u WHERE u.id = ub.user_id;
ALTER TABLE user_branches ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE user_branches ADD CONSTRAINT user_branches_user_organization_fkey
 FOREIGN KEY (user_id, organization_id) REFERENCES users (id, organization_id) ON DELETE CASCADE;
ALTER TABLE user_branches ADD CONSTRAINT user_branches_branch_organization_fkey
 FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id) ON DELETE CASCADE;
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

-- +goose Down
DROP TABLE plan_agreements;
ALTER TABLE user_branches DROP CONSTRAINT user_branches_branch_organization_fkey;
ALTER TABLE user_branches DROP CONSTRAINT user_branches_user_organization_fkey;
ALTER TABLE user_branches DROP COLUMN organization_id;
ALTER TABLE users DROP CONSTRAINT users_id_organization_key;
ALTER TABLE branches DROP CONSTRAINT branches_gym_organization_fkey;
ALTER TABLE branches DROP CONSTRAINT branches_id_organization_key;
ALTER TABLE branches DROP COLUMN organization_id;
