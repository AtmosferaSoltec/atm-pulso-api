-- +goose Up
-- Settings of the kiosk module (plan 08). Phase 1 keeps one thing per gym: the hash of the exit PIN that
-- takes the access tablet out of kiosk mode. The PIN itself is never stored.
CREATE TABLE kiosk_settings (
    gym_id       uuid PRIMARY KEY REFERENCES gyms (id) ON DELETE CASCADE,
    pin_hash     text NOT NULL,
    updated_by   uuid NOT NULL REFERENCES users (id),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE kiosk_settings;
