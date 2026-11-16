-- +goose Up
-- The platform superadmin can suspend a gym (for example, for lack of payment): nobody of that gym may use
-- Pulso until it is reactivated. Nothing is deleted. The kiosk becomes a base module every gym has, so it no
-- longer needs to be stored among the switched-on optional modules.
ALTER TABLE gyms ADD COLUMN status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended'));
UPDATE gyms SET features = array_remove(features, 'kiosk') WHERE 'kiosk' = ANY(features);

-- +goose Down
UPDATE gyms SET features = array_append(features, 'kiosk') WHERE NOT ('kiosk' = ANY(features));
ALTER TABLE gyms DROP COLUMN status;
