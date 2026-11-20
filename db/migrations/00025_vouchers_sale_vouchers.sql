-- +goose Up
-- Proofs of payment (photo or PDF of a transfer, Yape/Plin or card receipt) of PRODUCT SALES, owned by the
-- vouchers module like payment_vouchers. Sales money is received either when a sale is created paid
-- (sales.method) or when pending sales are collected later (sale_collections.method), so a voucher proves
-- exactly one of the two: a sale paid on the spot or a collection. Optional, up to five per sale or per
-- collection, immutable. The files live in the private object store; this table keeps the key and the metadata.
-- Composite foreign keys keep every voucher inside the gym of its sale, collection and uploader.
CREATE TABLE sale_vouchers (
    id            uuid PRIMARY KEY,
    gym_id        uuid        NOT NULL REFERENCES gyms (id) ON DELETE CASCADE,
    sale_id       uuid,
    collection_id uuid,
    storage_key   text        NOT NULL,
    content_type  text        NOT NULL CHECK (content_type IN ('image/png', 'image/jpeg', 'image/webp', 'application/pdf')),
    size_bytes    integer     NOT NULL CHECK (size_bytes > 0),
    uploaded_by   uuid        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sale_vouchers_one_owner CHECK ((sale_id IS NULL) <> (collection_id IS NULL)),
    CONSTRAINT sale_vouchers_sale_fkey FOREIGN KEY (sale_id, gym_id) REFERENCES sales (id, gym_id) ON DELETE CASCADE,
    CONSTRAINT sale_vouchers_collection_fkey FOREIGN KEY (collection_id, gym_id)
        REFERENCES sale_collections (id, gym_id) ON DELETE CASCADE,
    CONSTRAINT sale_vouchers_uploaded_by_fkey FOREIGN KEY (uploaded_by, gym_id)
        REFERENCES users (id, gym_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX sale_vouchers_sale_idx ON sale_vouchers (sale_id, created_at) WHERE sale_id IS NOT NULL;
CREATE INDEX sale_vouchers_collection_idx ON sale_vouchers (collection_id, created_at) WHERE collection_id IS NOT NULL;

-- +goose Down
DROP TABLE sale_vouchers;
