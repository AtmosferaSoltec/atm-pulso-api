-- +goose Up
-- Optional modules a gym has switched on by the platform superadmin (plan 08). The closed list of valid
-- names lives in code (internal/platform/auth/features.go) so a new module needs no migration.
ALTER TABLE gyms ADD COLUMN features text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE gyms DROP COLUMN features;
