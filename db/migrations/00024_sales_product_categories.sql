-- +goose Up
-- Product categories (Batidos, Suplementos, Material...): each gym has its own list, managed by its admin or
-- by Atmósfera's console on its behalf. A product may have one; deleting a category leaves its products
-- without one.
CREATE TABLE product_categories (
    id         uuid        PRIMARY KEY,
    gym_id     uuid        NOT NULL REFERENCES gyms (id) ON DELETE CASCADE,
    name       text        NOT NULL CHECK (btrim(name) <> '' AND char_length(name) <= 40),
    position   integer     NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT product_categories_id_gym_key UNIQUE (id, gym_id)
);
CREATE UNIQUE INDEX product_categories_name_key ON product_categories (gym_id, lower(name));

ALTER TABLE products ADD COLUMN category_id uuid;
ALTER TABLE products ADD CONSTRAINT products_category_fkey FOREIGN KEY (category_id, gym_id)
    REFERENCES product_categories (id, gym_id) ON DELETE SET NULL (category_id);
CREATE INDEX products_category_idx ON products (gym_id, category_id);

-- +goose Down
ALTER TABLE products DROP COLUMN category_id;
DROP TABLE product_categories;
