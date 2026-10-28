-- +goose Up
-- Photos or PDFs of the proof of a payment (transfer, Yape/Plin, card). Optional, up to five per
-- payment, immutable once uploaded: they are evidence for later claims. The files live in the
-- private object store; this table keeps the key and the metadata.
ALTER TABLE payments ADD CONSTRAINT payments_id_organization_key UNIQUE (id, organization_id);

CREATE TABLE payment_vouchers (
    id              uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    payment_id      uuid NOT NULL,
    storage_key     text NOT NULL,
    content_type    text NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/webp', 'application/pdf')),
    size_bytes      integer NOT NULL CHECK (size_bytes > 0),
    uploaded_by     uuid NOT NULL REFERENCES users (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT payment_vouchers_payment_fkey FOREIGN KEY (payment_id, organization_id)
        REFERENCES payments (id, organization_id) ON DELETE CASCADE
);
CREATE INDEX payment_vouchers_payment_idx ON payment_vouchers (payment_id, created_at);

-- +goose Down
DROP TABLE payment_vouchers;
ALTER TABLE payments DROP CONSTRAINT payments_id_organization_key;
