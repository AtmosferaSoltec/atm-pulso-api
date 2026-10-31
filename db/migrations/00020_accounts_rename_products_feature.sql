-- +goose Up
-- The optional module of product sales is called `sales` (it was `products` while in development). Rename the value in
-- the gyms that already had it switched on.
UPDATE gyms SET features = array_replace(features, 'products', 'sales') WHERE 'products' = ANY(features);

-- +goose Down
UPDATE gyms SET features = array_replace(features, 'sales', 'products') WHERE 'sales' = ANY(features);
