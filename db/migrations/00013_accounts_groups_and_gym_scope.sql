-- +goose Up
-- Product decision (plan 07): the figure of the owner disappears. A GROUP (formerly organization) is an
-- internal label the superadmin uses to bundle related gyms; a GYM is a brand and is now the tenant; a
-- branch is a physical location. Members, memberships, receipts, payments, access and day passes belong to
-- a gym (their old organization_id becomes gym_id, backfilled from each row's branch). Nothing is shared
-- between gyms; the `network` membership scope disappears. Receipt series become per gym.

-- Financial rows must never be silently altered: stop if a sale crosses two gyms (only possible with the
-- old `network` scope, which production never used because its data was wiped by migration 00012).
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM payments p JOIN branches b ON b.id = p.branch_id JOIN members m ON m.id = p.member_id
               WHERE m.home_branch_id IS NOT NULL AND b.gym_id <> (SELECT gym_id FROM branches WHERE id = m.home_branch_id))
    THEN
        RAISE EXCEPTION 'migration 00013: a payment was collected in a gym other than its member''s gym; resolve it by hand first';
    END IF;
END $$;
-- +goose StatementEnd

-- 1. organizations -> groups. The auth client moves to the gym (it is still checked on every token).
ALTER TABLE gyms ADD COLUMN auth_client_id uuid;
UPDATE gyms g SET auth_client_id = o.auth_client_id FROM organizations o WHERE o.id = g.organization_id;
ALTER TABLE gyms ALTER COLUMN auth_client_id SET NOT NULL;
CREATE INDEX gyms_auth_client_idx ON gyms (auth_client_id);

ALTER TABLE organizations RENAME TO groups;
ALTER INDEX organizations_pkey RENAME TO groups_pkey;
ALTER TABLE groups DROP COLUMN auth_client_id;

-- 2. Tenant tables: add gym_id, backfill, drop organization_id (CASCADE removes the old composite keys).
-- Parents first so children can read their gym.
ALTER TABLE user_branches ADD COLUMN gym_id uuid;
UPDATE user_branches ub SET gym_id = u.gym_id FROM users u WHERE u.id = ub.user_id;
-- A receptionist could only ever be linked to branches of its own gym; drop any stray link to be safe.
DELETE FROM user_branches ub USING branches b WHERE b.id = ub.branch_id AND b.gym_id <> ub.gym_id;
ALTER TABLE user_branches ALTER COLUMN gym_id SET NOT NULL;
ALTER TABLE user_branches DROP COLUMN organization_id CASCADE;

ALTER TABLE branches DROP COLUMN organization_id CASCADE;
ALTER TABLE users DROP COLUMN organization_id CASCADE;

ALTER TABLE members ADD COLUMN gym_id uuid;
UPDATE members m SET gym_id = b.gym_id FROM branches b WHERE b.id = m.home_branch_id;
ALTER TABLE members ALTER COLUMN gym_id SET NOT NULL;
ALTER TABLE members DROP COLUMN organization_id CASCADE;

ALTER TABLE memberships ADD COLUMN gym_id uuid;
UPDATE memberships ms SET gym_id = m.gym_id FROM members m WHERE m.id = ms.member_id;
ALTER TABLE memberships ALTER COLUMN gym_id SET NOT NULL;
ALTER TABLE memberships DROP COLUMN organization_id CASCADE;
-- `network` (every branch of the organization) no longer exists: those periods become `gym`.
ALTER TABLE memberships DROP CONSTRAINT memberships_access_scope_check;
UPDATE memberships SET access_scope = 'gym' WHERE access_scope = 'network';
ALTER TABLE memberships ADD CONSTRAINT memberships_access_scope_check CHECK (access_scope IN ('branch', 'gym'));

ALTER TABLE membership_branches ADD COLUMN gym_id uuid;
UPDATE membership_branches mb SET gym_id = ms.gym_id FROM memberships ms WHERE ms.id = mb.membership_id;
-- Branches of other gyms (opened by a former `network` period) are no longer reachable.
DELETE FROM membership_branches mb USING branches b WHERE b.id = mb.branch_id AND b.gym_id <> mb.gym_id;
ALTER TABLE membership_branches ALTER COLUMN gym_id SET NOT NULL;
ALTER TABLE membership_branches DROP COLUMN organization_id CASCADE;

ALTER TABLE receipts ADD COLUMN gym_id uuid;
UPDATE receipts r SET gym_id = b.gym_id FROM branches b WHERE b.id = r.branch_id;
ALTER TABLE receipts ALTER COLUMN gym_id SET NOT NULL;
ALTER TABLE receipts DROP COLUMN organization_id CASCADE;

-- Series per gym: one counter per (gym, kind), continuing from the highest number the gym has issued.
DROP TABLE receipt_counters;
CREATE TABLE receipt_counters (
    gym_id      uuid    NOT NULL REFERENCES gyms (id) ON DELETE CASCADE,
    kind        text    NOT NULL CHECK (kind IN ('membership', 'day_pass')),
    last_number integer NOT NULL,
    PRIMARY KEY (gym_id, kind)
);
INSERT INTO receipt_counters (gym_id, kind, last_number)
SELECT gym_id, kind, max(number) FROM receipts GROUP BY gym_id, kind;

ALTER TABLE payments ADD COLUMN gym_id uuid;
UPDATE payments p SET gym_id = b.gym_id FROM branches b WHERE b.id = p.branch_id;
ALTER TABLE payments ALTER COLUMN gym_id SET NOT NULL;
ALTER TABLE payments DROP COLUMN organization_id CASCADE;

ALTER TABLE day_passes ADD COLUMN gym_id uuid;
UPDATE day_passes d SET gym_id = b.gym_id FROM branches b WHERE b.id = d.branch_id;
ALTER TABLE day_passes ALTER COLUMN gym_id SET NOT NULL;
ALTER TABLE day_passes DROP COLUMN organization_id CASCADE;

ALTER TABLE access_entries ADD COLUMN gym_id uuid;
UPDATE access_entries e SET gym_id = b.gym_id FROM branches b WHERE b.id = e.branch_id;
-- An entry of a member of another gym (old `network` scope) keeps its name but loses the member link.
UPDATE access_entries e SET member_id = NULL FROM members m WHERE m.id = e.member_id AND m.gym_id <> e.gym_id;
ALTER TABLE access_entries ALTER COLUMN gym_id SET NOT NULL;
ALTER TABLE access_entries DROP COLUMN organization_id CASCADE;

ALTER TABLE payment_vouchers ADD COLUMN gym_id uuid;
UPDATE payment_vouchers v SET gym_id = p.gym_id FROM payments p WHERE p.id = v.payment_id;
ALTER TABLE payment_vouchers ALTER COLUMN gym_id SET NOT NULL;
ALTER TABLE payment_vouchers DROP COLUMN organization_id CASCADE;

-- 3. gyms belong to a group. A group is removed only when empty (RESTRICT), never taking business data with it.
ALTER TABLE gyms DROP CONSTRAINT gyms_organization_id_fkey;
ALTER TABLE gyms DROP CONSTRAINT gyms_id_organization_key;
ALTER TABLE gyms RENAME COLUMN organization_id TO group_id;
ALTER INDEX gyms_organization_idx RENAME TO gyms_group_idx;
ALTER TABLE gyms ADD CONSTRAINT gyms_group_id_fkey FOREIGN KEY (group_id) REFERENCES groups (id) ON DELETE RESTRICT;
ALTER TABLE groups ADD CONSTRAINT groups_name_not_blank CHECK (btrim(name) <> '');

-- 4. Composite keys on the gym: the database refuses any cross-gym link.
ALTER TABLE branches ADD CONSTRAINT branches_id_gym_key UNIQUE (id, gym_id);
ALTER TABLE users
    ADD CONSTRAINT users_id_gym_key UNIQUE (id, gym_id),
    ADD CONSTRAINT users_gym_id_fkey FOREIGN KEY (gym_id) REFERENCES gyms (id) ON DELETE CASCADE;

ALTER TABLE user_branches
    ADD CONSTRAINT user_branches_user_gym_fkey FOREIGN KEY (user_id, gym_id) REFERENCES users (id, gym_id) ON DELETE CASCADE,
    ADD CONSTRAINT user_branches_branch_gym_fkey FOREIGN KEY (branch_id, gym_id) REFERENCES branches (id, gym_id) ON DELETE CASCADE;

ALTER TABLE members
    ADD CONSTRAINT members_gym_id_fkey FOREIGN KEY (gym_id) REFERENCES gyms (id) ON DELETE CASCADE,
    -- The document is unique per gym: the same person may be a member of two gyms, even of one group.
    ADD CONSTRAINT members_document_key UNIQUE (gym_id, document_number),
    ADD CONSTRAINT members_id_gym_key UNIQUE (id, gym_id),
    ADD CONSTRAINT members_home_branch_fkey FOREIGN KEY (home_branch_id, gym_id) REFERENCES branches (id, gym_id),
    -- Reserved for the future `member` role (a person of atm-auth-api). Nothing reads or writes it yet.
    ADD COLUMN auth_user_id uuid;
CREATE UNIQUE INDEX members_gym_auth_user_key ON members (gym_id, auth_user_id) WHERE auth_user_id IS NOT NULL;
CREATE INDEX members_home_branch_idx ON members (gym_id, home_branch_id);

ALTER TABLE memberships
    ADD CONSTRAINT memberships_id_gym_key UNIQUE (id, gym_id),
    ADD CONSTRAINT memberships_member_fkey FOREIGN KEY (member_id, gym_id) REFERENCES members (id, gym_id) ON DELETE CASCADE;

ALTER TABLE membership_branches
    ADD CONSTRAINT membership_branches_membership_gym_fkey FOREIGN KEY (membership_id, gym_id) REFERENCES memberships (id, gym_id) ON DELETE CASCADE,
    ADD CONSTRAINT membership_branches_branch_gym_fkey FOREIGN KEY (branch_id, gym_id) REFERENCES branches (id, gym_id) ON DELETE CASCADE;

ALTER TABLE receipts
    ADD CONSTRAINT receipts_gym_id_fkey FOREIGN KEY (gym_id) REFERENCES gyms (id) ON DELETE CASCADE,
    ADD CONSTRAINT receipts_number_key UNIQUE (gym_id, kind, number),
    ADD CONSTRAINT receipts_id_gym_key UNIQUE (id, gym_id),
    ADD CONSTRAINT receipts_branch_gym_fkey FOREIGN KEY (branch_id, gym_id) REFERENCES branches (id, gym_id);

ALTER TABLE day_passes
    ADD CONSTRAINT day_passes_gym_id_fkey FOREIGN KEY (gym_id) REFERENCES gyms (id) ON DELETE CASCADE,
    ADD CONSTRAINT day_passes_id_gym_key UNIQUE (id, gym_id),
    ADD CONSTRAINT day_passes_branch_gym_fkey FOREIGN KEY (branch_id, gym_id) REFERENCES branches (id, gym_id),
    ADD CONSTRAINT day_passes_receipt_gym_fkey FOREIGN KEY (receipt_id, gym_id) REFERENCES receipts (id, gym_id) ON DELETE CASCADE;
CREATE INDEX day_passes_branch_idx ON day_passes (gym_id, branch_id, created_at DESC);

ALTER TABLE payments
    ADD CONSTRAINT payments_gym_id_fkey FOREIGN KEY (gym_id) REFERENCES gyms (id) ON DELETE CASCADE,
    ADD CONSTRAINT payments_id_gym_key UNIQUE (id, gym_id),
    ADD CONSTRAINT payments_receipt_gym_fkey FOREIGN KEY (receipt_id, gym_id) REFERENCES receipts (id, gym_id) ON DELETE CASCADE,
    ADD CONSTRAINT payments_branch_gym_fkey FOREIGN KEY (branch_id, gym_id) REFERENCES branches (id, gym_id),
    ADD CONSTRAINT payments_member_gym_fkey FOREIGN KEY (member_id, gym_id) REFERENCES members (id, gym_id) ON DELETE CASCADE,
    ADD CONSTRAINT payments_membership_gym_fkey FOREIGN KEY (membership_id, gym_id) REFERENCES memberships (id, gym_id) ON DELETE CASCADE,
    ADD CONSTRAINT payments_day_pass_fkey FOREIGN KEY (day_pass_id, gym_id) REFERENCES day_passes (id, gym_id) ON DELETE CASCADE;
CREATE INDEX payments_branch_date_idx ON payments (gym_id, branch_id, paid_on DESC);

ALTER TABLE access_entries
    ADD CONSTRAINT access_entries_gym_id_fkey FOREIGN KEY (gym_id) REFERENCES gyms (id) ON DELETE CASCADE,
    ADD CONSTRAINT access_entries_branch_gym_fkey FOREIGN KEY (branch_id, gym_id) REFERENCES branches (id, gym_id) ON DELETE CASCADE,
    ADD CONSTRAINT access_entries_member_gym_fkey FOREIGN KEY (member_id, gym_id) REFERENCES members (id, gym_id) ON DELETE SET NULL (member_id);

ALTER TABLE payment_vouchers
    ADD CONSTRAINT payment_vouchers_gym_id_fkey FOREIGN KEY (gym_id) REFERENCES gyms (id) ON DELETE CASCADE,
    ADD CONSTRAINT payment_vouchers_payment_fkey FOREIGN KEY (payment_id, gym_id) REFERENCES payments (id, gym_id) ON DELETE CASCADE;

-- +goose Down
-- Best effort: the old model is rebuilt from the gyms (each group becomes an organization again). It cannot
-- bring back `network` periods, and it fails if two gyms of one group issued the same receipt number
-- (the old series was per organization).
ALTER TABLE groups ADD COLUMN auth_client_id uuid;
UPDATE groups gr SET auth_client_id = x.auth_client_id FROM (
    SELECT DISTINCT ON (group_id) group_id, auth_client_id FROM gyms ORDER BY group_id, created_at, id) x
    WHERE x.group_id = gr.id;
UPDATE groups SET auth_client_id = gen_random_uuid() WHERE auth_client_id IS NULL;
UPDATE groups g SET auth_client_id = gen_random_uuid() FROM (
    SELECT id, row_number() OVER (PARTITION BY auth_client_id ORDER BY created_at, id) AS n FROM groups) d
    WHERE d.id = g.id AND d.n > 1;
ALTER TABLE groups ALTER COLUMN auth_client_id SET NOT NULL;
ALTER TABLE groups ADD CONSTRAINT organizations_auth_client_key UNIQUE (auth_client_id);
ALTER TABLE groups DROP CONSTRAINT groups_name_not_blank;
ALTER TABLE groups RENAME TO organizations;
ALTER INDEX groups_pkey RENAME TO organizations_pkey;

ALTER TABLE gyms DROP CONSTRAINT gyms_group_id_fkey;
ALTER TABLE gyms RENAME COLUMN group_id TO organization_id;
ALTER INDEX gyms_group_idx RENAME TO gyms_organization_idx;
ALTER TABLE gyms ADD CONSTRAINT gyms_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE CASCADE;
ALTER TABLE gyms ADD CONSTRAINT gyms_id_organization_key UNIQUE (id, organization_id);
DROP INDEX gyms_auth_client_idx;
ALTER TABLE gyms DROP COLUMN auth_client_id;

-- Give every tenant table its organization_id back (from the gym) and drop gym_id with its keys.
ALTER TABLE branches ADD COLUMN organization_id uuid;
UPDATE branches b SET organization_id = g.organization_id FROM gyms g WHERE g.id = b.gym_id;
ALTER TABLE branches ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE branches ADD CONSTRAINT branches_id_organization_key UNIQUE (id, organization_id);
ALTER TABLE branches ADD CONSTRAINT branches_gym_organization_fkey
    FOREIGN KEY (gym_id, organization_id) REFERENCES gyms (id, organization_id) ON DELETE CASCADE;

ALTER TABLE users ADD COLUMN organization_id uuid;
UPDATE users u SET organization_id = g.organization_id FROM gyms g WHERE g.id = u.gym_id;
ALTER TABLE users ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_id_organization_key UNIQUE (id, organization_id);
ALTER TABLE users ADD CONSTRAINT users_gym_organization_fkey
    FOREIGN KEY (gym_id, organization_id) REFERENCES gyms (id, organization_id) ON DELETE CASCADE;

DROP INDEX members_gym_auth_user_key;
DROP INDEX members_home_branch_idx;
DROP INDEX day_passes_branch_idx;
DROP INDEX payments_branch_date_idx;

-- Every other table: add organization_id, rebuild the keys on it, drop gym_id (CASCADE removes the gym keys).
ALTER TABLE user_branches ADD COLUMN organization_id uuid;
UPDATE user_branches t SET organization_id = g.organization_id FROM gyms g WHERE g.id = t.gym_id;
ALTER TABLE user_branches ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE user_branches DROP COLUMN gym_id CASCADE;
ALTER TABLE user_branches
    ADD CONSTRAINT user_branches_user_organization_fkey FOREIGN KEY (user_id, organization_id) REFERENCES users (id, organization_id) ON DELETE CASCADE,
    ADD CONSTRAINT user_branches_branch_organization_fkey FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id) ON DELETE CASCADE;

ALTER TABLE members DROP COLUMN auth_user_id;
ALTER TABLE members ADD COLUMN organization_id uuid;
UPDATE members t SET organization_id = g.organization_id FROM gyms g WHERE g.id = t.gym_id;
ALTER TABLE members ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE members DROP COLUMN gym_id CASCADE;

ALTER TABLE memberships ADD COLUMN organization_id uuid;
UPDATE memberships t SET organization_id = g.organization_id FROM gyms g WHERE g.id = t.gym_id;
ALTER TABLE memberships ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE memberships DROP COLUMN gym_id CASCADE;
ALTER TABLE memberships DROP CONSTRAINT memberships_access_scope_check;
ALTER TABLE memberships ADD CONSTRAINT memberships_access_scope_check CHECK (access_scope IN ('branch', 'gym', 'network'));

ALTER TABLE membership_branches ADD COLUMN organization_id uuid;
UPDATE membership_branches t SET organization_id = g.organization_id FROM gyms g WHERE g.id = t.gym_id;
ALTER TABLE membership_branches ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE membership_branches DROP COLUMN gym_id CASCADE;

ALTER TABLE receipts ADD COLUMN organization_id uuid;
UPDATE receipts t SET organization_id = g.organization_id FROM gyms g WHERE g.id = t.gym_id;
ALTER TABLE receipts ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE receipts DROP COLUMN gym_id CASCADE;

DROP TABLE receipt_counters;
CREATE TABLE receipt_counters (
    organization_id uuid    NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    kind            text    NOT NULL CHECK (kind IN ('membership', 'day_pass')),
    last_number     integer NOT NULL,
    PRIMARY KEY (organization_id, kind)
);
INSERT INTO receipt_counters (organization_id, kind, last_number)
SELECT organization_id, kind, max(number) FROM receipts GROUP BY organization_id, kind;

ALTER TABLE payments ADD COLUMN organization_id uuid;
UPDATE payments t SET organization_id = g.organization_id FROM gyms g WHERE g.id = t.gym_id;
ALTER TABLE payments ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE payments DROP COLUMN gym_id CASCADE;

ALTER TABLE day_passes ADD COLUMN organization_id uuid;
UPDATE day_passes t SET organization_id = g.organization_id FROM gyms g WHERE g.id = t.gym_id;
ALTER TABLE day_passes ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE day_passes DROP COLUMN gym_id CASCADE;

ALTER TABLE access_entries ADD COLUMN organization_id uuid;
UPDATE access_entries t SET organization_id = g.organization_id FROM gyms g WHERE g.id = t.gym_id;
ALTER TABLE access_entries ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE access_entries DROP COLUMN gym_id CASCADE;

ALTER TABLE payment_vouchers ADD COLUMN organization_id uuid;
UPDATE payment_vouchers t SET organization_id = g.organization_id FROM gyms g WHERE g.id = t.gym_id;
ALTER TABLE payment_vouchers ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE payment_vouchers DROP COLUMN gym_id CASCADE;

ALTER TABLE branches DROP CONSTRAINT branches_id_gym_key;
ALTER TABLE users DROP CONSTRAINT users_id_gym_key;
ALTER TABLE users DROP CONSTRAINT users_gym_id_fkey;

-- Keys on organization_id, as migrations 00004 to 00012 left them.
ALTER TABLE members
    ADD CONSTRAINT members_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE CASCADE,
    ADD CONSTRAINT members_document_key UNIQUE (organization_id, document_number),
    ADD CONSTRAINT members_id_organization_key UNIQUE (id, organization_id),
    ADD CONSTRAINT members_home_branch_fkey FOREIGN KEY (home_branch_id, organization_id) REFERENCES branches (id, organization_id);
CREATE INDEX members_home_branch_idx ON members (organization_id, home_branch_id);
ALTER TABLE memberships
    ADD CONSTRAINT memberships_id_organization_key UNIQUE (id, organization_id),
    ADD CONSTRAINT memberships_member_fkey FOREIGN KEY (member_id, organization_id) REFERENCES members (id, organization_id) ON DELETE CASCADE;
ALTER TABLE membership_branches
    ADD FOREIGN KEY (membership_id, organization_id) REFERENCES memberships (id, organization_id) ON DELETE CASCADE,
    ADD FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id) ON DELETE CASCADE;
ALTER TABLE receipts
    ADD CONSTRAINT receipts_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE CASCADE,
    ADD CONSTRAINT receipts_number_key UNIQUE (organization_id, kind, number),
    ADD CONSTRAINT receipts_id_organization_key UNIQUE (id, organization_id),
    ADD FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id);
ALTER TABLE day_passes
    ADD CONSTRAINT day_passes_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE CASCADE,
    ADD CONSTRAINT day_passes_id_organization_key UNIQUE (id, organization_id),
    ADD FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id),
    ADD FOREIGN KEY (receipt_id, organization_id) REFERENCES receipts (id, organization_id) ON DELETE CASCADE;
CREATE INDEX day_passes_branch_idx ON day_passes (organization_id, branch_id, created_at DESC);
ALTER TABLE payments
    ADD CONSTRAINT payments_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE CASCADE,
    ADD CONSTRAINT payments_id_organization_key UNIQUE (id, organization_id),
    ADD FOREIGN KEY (receipt_id, organization_id) REFERENCES receipts (id, organization_id) ON DELETE CASCADE,
    ADD FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id),
    ADD FOREIGN KEY (member_id, organization_id) REFERENCES members (id, organization_id) ON DELETE CASCADE,
    ADD FOREIGN KEY (membership_id, organization_id) REFERENCES memberships (id, organization_id) ON DELETE CASCADE,
    ADD CONSTRAINT payments_day_pass_fkey FOREIGN KEY (day_pass_id, organization_id) REFERENCES day_passes (id, organization_id) ON DELETE CASCADE;
CREATE INDEX payments_branch_date_idx ON payments (organization_id, branch_id, paid_on DESC);
ALTER TABLE access_entries
    ADD CONSTRAINT access_entries_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE CASCADE,
    ADD FOREIGN KEY (branch_id, organization_id) REFERENCES branches (id, organization_id) ON DELETE CASCADE,
    ADD FOREIGN KEY (member_id, organization_id) REFERENCES members (id, organization_id) ON DELETE SET NULL (member_id);
ALTER TABLE payment_vouchers
    ADD CONSTRAINT payment_vouchers_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES organizations (id) ON DELETE CASCADE,
    ADD CONSTRAINT payment_vouchers_payment_fkey FOREIGN KEY (payment_id, organization_id) REFERENCES payments (id, organization_id) ON DELETE CASCADE;
