-- +goose Up
ALTER TABLE pulso.users ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE pulso.users DROP COLUMN must_change_password;
