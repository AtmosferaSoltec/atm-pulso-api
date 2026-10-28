-- +goose Up
-- Key of the gym's logo in the object store (Cloudflare R2); NULL when it has none.
ALTER TABLE gyms ADD COLUMN logo_key text;

-- +goose Down
ALTER TABLE gyms DROP COLUMN logo_key;
