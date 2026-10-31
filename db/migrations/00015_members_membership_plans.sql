-- +goose Up
-- Plan 08: a gym sells several membership PLANS (e.g. "Baile", "Todo el gym"), each with its own price per
-- duration. A member may hold several current periods at once, one per plan. This replaces the single price
-- list kept in gyms.membership_prices (migration 00007).

CREATE TABLE membership_plans (
    id          uuid PRIMARY KEY,
    gym_id      uuid        NOT NULL REFERENCES gyms (id) ON DELETE CASCADE,
    name        text        NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 60),
    description text        CHECK (description IS NULL OR (btrim(description) <> '' AND char_length(description) <= 200)),
    is_active   boolean     NOT NULL DEFAULT true,
    position    integer     NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT membership_plans_id_gym_key UNIQUE (id, gym_id)
);
-- A name is unique inside a gym (case-insensitive), including deactivated plans.
CREATE UNIQUE INDEX membership_plans_name_key ON membership_plans (gym_id, lower(name));

CREATE TABLE membership_plan_prices (
    plan_id uuid           NOT NULL,
    gym_id  uuid           NOT NULL,
    months  integer        NOT NULL CHECK (months BETWEEN 1 AND 24),
    amount  numeric(14, 2) NOT NULL CHECK (amount >= 0 AND amount <= 1000000),
    PRIMARY KEY (plan_id, months),
    CONSTRAINT membership_plan_prices_plan_fkey FOREIGN KEY (plan_id, gym_id)
        REFERENCES membership_plans (id, gym_id) ON DELETE CASCADE
);

-- Every existing gym gets a default plan "Membresía" carrying its old price list.
INSERT INTO membership_plans (id, gym_id, name, position)
SELECT gen_random_uuid(), id, 'Membresía', 0 FROM gyms;

INSERT INTO membership_plan_prices (plan_id, gym_id, months, amount)
SELECT p.id, p.gym_id, (e ->> 'months')::int, (e ->> 'amount')::numeric
FROM membership_plans p
JOIN gyms g ON g.id = p.gym_id
CROSS JOIN LATERAL jsonb_array_elements(g.membership_prices) e;

-- Each period keeps its plan (id) and the plan's name as it was when sold, so old receipts never change.
ALTER TABLE memberships ADD COLUMN plan_id uuid, ADD COLUMN plan_name text;
UPDATE memberships ms SET plan_id = p.id, plan_name = p.name FROM membership_plans p WHERE p.gym_id = ms.gym_id;
ALTER TABLE memberships
    ALTER COLUMN plan_id SET NOT NULL,
    ALTER COLUMN plan_name SET NOT NULL,
    ADD CONSTRAINT memberships_plan_fkey FOREIGN KEY (plan_id, gym_id) REFERENCES membership_plans (id, gym_id);
CREATE INDEX memberships_plan_idx ON memberships (plan_id);

ALTER TABLE gyms DROP COLUMN membership_prices;

-- +goose Down
ALTER TABLE gyms ADD COLUMN membership_prices jsonb NOT NULL DEFAULT '[]'::jsonb;
-- Lossy by nature: only the first plan's prices survive.
UPDATE gyms g SET membership_prices = COALESCE((
    SELECT jsonb_agg(jsonb_build_object('months', pr.months, 'amount', to_char(pr.amount, 'FM9999999990.00')) ORDER BY pr.months)
    FROM membership_plan_prices pr
    WHERE pr.plan_id = (SELECT id FROM membership_plans WHERE gym_id = g.id ORDER BY position, created_at, id LIMIT 1)), '[]'::jsonb);
DROP INDEX memberships_plan_idx;
ALTER TABLE memberships DROP CONSTRAINT memberships_plan_fkey, DROP COLUMN plan_id, DROP COLUMN plan_name;
DROP TABLE membership_plan_prices;
DROP TABLE membership_plans;
