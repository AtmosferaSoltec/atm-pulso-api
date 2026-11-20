-- +goose Up
-- Retention of the voucher files: after VOUCHER_RETENTION_DAYS the vouchers module deletes the file from the
-- object store and stamps purged_at. The row stays (counts and history remain); only its file is gone.
-- Additive: existing rows keep purged_at NULL until the purge reaches them.
ALTER TABLE payment_vouchers ADD COLUMN purged_at timestamptz;
ALTER TABLE sale_vouchers ADD COLUMN purged_at timestamptz;
-- The purge looks only for files still stored, oldest first; once purged a row leaves these indexes.
CREATE INDEX payment_vouchers_purgeable_idx ON payment_vouchers (created_at, id) WHERE purged_at IS NULL;
CREATE INDEX sale_vouchers_purgeable_idx ON sale_vouchers (created_at, id) WHERE purged_at IS NULL;

-- +goose Down
DROP INDEX sale_vouchers_purgeable_idx;
DROP INDEX payment_vouchers_purgeable_idx;
ALTER TABLE sale_vouchers DROP COLUMN purged_at;
ALTER TABLE payment_vouchers DROP COLUMN purged_at;
