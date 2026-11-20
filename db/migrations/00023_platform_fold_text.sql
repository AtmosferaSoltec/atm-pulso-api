-- +goose Up
-- fold_text makes name searches ignore case and Spanish accents ("ramirez" finds "Ramírez" and back).
-- A plain SQL function instead of the unaccent extension: it needs no extension or superuser privilege,
-- so the app role (pulso_app) that runs the migrations can create it. Accented capitals are mapped
-- explicitly before lower(), so the result does not depend on the database's ctype.
-- +goose StatementBegin
CREATE FUNCTION fold_text(value text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
AS $$
  SELECT lower(translate(value,
    'ÁÀÂÄÃÉÈÊËÍÌÎÏÓÒÔÖÕÚÙÛÜÑÇáàâäãéèêëíìîïóòôöõúùûüñç',
    'AAAAAEEEEIIIIOOOOOUUUUNCaaaaaeeeeiiiiooooouuuunc'))
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION fold_text(text);
