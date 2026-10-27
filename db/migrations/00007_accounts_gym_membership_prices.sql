-- +goose Up
ALTER TABLE gyms ADD COLUMN membership_prices jsonb NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE gyms DROP COLUMN membership_prices;
