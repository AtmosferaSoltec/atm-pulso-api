-- +goose Up
-- The foreign keys between these tables are DEFERRABLE INITIALLY DEFERRED so that deleting a gym (everything cascades from it) is
-- checked once all the cascades ran, whatever their order.
-- Module products (plan 08, piece 3): a short catalog per gym, quick sales of units and pending sales
-- (pending sales) that a member pays later. NOT receipts, NOT invoices, NOT inventory: no R-/D- series and no
-- payments rows. Everything is separate from the membership income.
CREATE TABLE products (
    id         uuid PRIMARY KEY,
    gym_id     uuid           NOT NULL REFERENCES gyms (id) ON DELETE CASCADE,
    name       text           NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 60),
    price      numeric(14, 2) NOT NULL CHECK (price > 0 AND price <= 10000),
    is_active  boolean        NOT NULL DEFAULT true,
    position   integer        NOT NULL DEFAULT 0,
    created_at timestamptz    NOT NULL DEFAULT now(),
    updated_at timestamptz    NOT NULL DEFAULT now(),
    CONSTRAINT products_id_gym_key UNIQUE (id, gym_id)
);
CREATE UNIQUE INDEX products_name_key ON products (gym_id, lower(name));

-- One row per collection action ("Cobrar"): the member paid the listed sales (whole sales) in one go.
CREATE TABLE sale_collections (
    id           uuid PRIMARY KEY,
    gym_id       uuid           NOT NULL REFERENCES gyms (id) ON DELETE CASCADE,
    member_id    uuid           NOT NULL,
    branch_id    uuid           NOT NULL,
    method       text           NOT NULL CHECK (method IN ('yape_plin', 'cash', 'card', 'transfer')),
    amount       numeric(14, 2) NOT NULL CHECK (amount > 0 AND amount <= 1000000),
    collected_by   uuid           NOT NULL REFERENCES users (id) DEFERRABLE INITIALLY DEFERRED,
    collected_at   timestamptz    NOT NULL DEFAULT now(),
    collected_on   date           NOT NULL,
    CONSTRAINT sale_collections_id_gym_key UNIQUE (id, gym_id),
    CONSTRAINT sale_collections_member_fkey FOREIGN KEY (member_id, gym_id) REFERENCES members (id, gym_id) DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT sale_collections_branch_fkey FOREIGN KEY (branch_id, gym_id) REFERENCES branches (id, gym_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX sale_collections_member_idx ON sale_collections (gym_id, member_id, collected_at DESC);

-- status: 'paid' (collected, at the sale or later by a collection) or 'pending' (pending sale still owed).
-- was_pending keeps that the sale was born as a pending sale after it is collected.
CREATE TABLE sales (
    id            uuid PRIMARY KEY,
    gym_id        uuid           NOT NULL REFERENCES gyms (id) ON DELETE CASCADE,
    branch_id     uuid           NOT NULL,
    sold_by       uuid           NOT NULL REFERENCES users (id) DEFERRABLE INITIALLY DEFERRED,
    member_id     uuid,
    status        text           NOT NULL CHECK (status IN ('paid', 'pending')),
    was_pending      boolean        NOT NULL DEFAULT false,
    method        text           CHECK (method IN ('yape_plin', 'cash', 'card', 'transfer')),
    total         numeric(14, 2) NOT NULL CHECK (total > 0 AND total <= 100000),
    units         integer        NOT NULL CHECK (units > 0),
    sold_at       timestamptz    NOT NULL DEFAULT now(),
    sold_on       date           NOT NULL,
    collection_id uuid,
    CONSTRAINT sales_id_gym_key UNIQUE (id, gym_id),
    CONSTRAINT sales_branch_fkey FOREIGN KEY (branch_id, gym_id) REFERENCES branches (id, gym_id) DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT sales_member_fkey FOREIGN KEY (member_id, gym_id) REFERENCES members (id, gym_id) DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT sales_collection_fkey FOREIGN KEY (collection_id, gym_id) REFERENCES sale_collections (id, gym_id) DEFERRABLE INITIALLY DEFERRED,
    -- A pending sale always names the member; a sale paid on the spot carries its method; a collected pending sale is 'paid'
    -- and points to its collection; an open pending sale points to none.
    CONSTRAINT sales_shape CHECK (
        (status = 'paid' AND NOT was_pending AND method IS NOT NULL AND collection_id IS NULL) OR
        (status = 'paid' AND was_pending AND member_id IS NOT NULL AND collection_id IS NOT NULL) OR
        (status = 'pending' AND was_pending AND member_id IS NOT NULL AND method IS NULL AND collection_id IS NULL))
);
CREATE INDEX sales_day_idx ON sales (gym_id, sold_on DESC, branch_id);
CREATE INDEX sales_pending_idx ON sales (gym_id, member_id) WHERE status = 'pending';

CREATE TABLE sale_items (
    id           uuid PRIMARY KEY,
    sale_id      uuid           NOT NULL,
    gym_id       uuid           NOT NULL,
    product_id   uuid           NOT NULL,
    product_name text           NOT NULL,
    quantity     integer        NOT NULL CHECK (quantity BETWEEN 1 AND 99),
    unit_price   numeric(14, 2) NOT NULL CHECK (unit_price > 0 AND unit_price <= 10000),
    CONSTRAINT sale_items_sale_fkey FOREIGN KEY (sale_id, gym_id) REFERENCES sales (id, gym_id) ON DELETE CASCADE,
    CONSTRAINT sale_items_product_fkey FOREIGN KEY (product_id, gym_id) REFERENCES products (id, gym_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX sale_items_sale_idx ON sale_items (sale_id);
CREATE INDEX sale_items_product_idx ON sale_items (gym_id, product_id);

-- +goose Down
DROP TABLE sale_items;
DROP TABLE sales;
DROP TABLE sale_collections;
DROP TABLE products;
