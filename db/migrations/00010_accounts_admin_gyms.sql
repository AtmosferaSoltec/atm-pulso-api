-- +goose Up
-- An admin manages one or more gyms of its organization (the owner decides which), so the
-- single users.gym_id becomes the user_gyms link, like user_branches for reception.
-- That an admin has at least one gym is an application rule; the database only guarantees
-- that every link stays inside the user's organization.
CREATE TABLE user_gyms (
 user_id uuid NOT NULL,
 gym_id uuid NOT NULL,
 organization_id uuid NOT NULL,
 PRIMARY KEY (user_id, gym_id),
 FOREIGN KEY (user_id, organization_id) REFERENCES users (id, organization_id) ON DELETE CASCADE,
 FOREIGN KEY (gym_id, organization_id) REFERENCES gyms (id, organization_id) ON DELETE CASCADE
);
CREATE INDEX user_gyms_gym_idx ON user_gyms (gym_id);

INSERT INTO user_gyms (user_id, gym_id, organization_id)
SELECT id, gym_id, organization_id FROM users WHERE role = 'admin' AND gym_id IS NOT NULL;

ALTER TABLE users DROP CONSTRAINT users_role_scope_check;
ALTER TABLE users DROP CONSTRAINT users_gym_same_organization_fkey;
ALTER TABLE users DROP COLUMN gym_id;
ALTER TABLE users ADD CONSTRAINT users_role_scope_check CHECK (
 (role = 'superadmin' AND organization_id IS NULL) OR
 (role IN ('owner', 'admin', 'receptionist') AND organization_id IS NOT NULL)
);

-- +goose Down
-- Lossy by nature: an admin with several gyms keeps only the first one.
ALTER TABLE users ADD COLUMN gym_id uuid;
UPDATE users u SET gym_id = (SELECT ug.gym_id FROM user_gyms ug WHERE ug.user_id = u.id ORDER BY ug.gym_id LIMIT 1)
 WHERE u.role = 'admin';
ALTER TABLE users DROP CONSTRAINT users_role_scope_check;
ALTER TABLE users ADD CONSTRAINT users_gym_same_organization_fkey
 FOREIGN KEY (gym_id, organization_id) REFERENCES gyms (id, organization_id) ON DELETE CASCADE;
ALTER TABLE users ADD CONSTRAINT users_role_scope_check CHECK (
 (role = 'superadmin'    AND organization_id IS NULL     AND gym_id IS NULL) OR
 (role = 'owner'         AND organization_id IS NOT NULL AND gym_id IS NULL) OR
 (role = 'admin'         AND organization_id IS NOT NULL AND gym_id IS NOT NULL) OR
 (role = 'receptionist'  AND organization_id IS NOT NULL AND gym_id IS NULL)
);
DROP TABLE user_gyms;
