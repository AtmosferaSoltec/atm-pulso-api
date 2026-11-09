-- +goose Up
-- People who work in a gym may have no Peruvian DNI: atm-auth-api also identifies them by a foreign resident card
-- (ce, 9 to 12 digits) or a passport (6 to 12 upper-case letters or digits). Existing rows are DNIs.
ALTER TABLE users
    DROP CONSTRAINT users_document_type_check,
    DROP CONSTRAINT users_document_number_check,
    ADD CONSTRAINT users_document_type_check CHECK (document_type IN ('dni', 'ce', 'passport')),
    ADD CONSTRAINT users_document_number_check CHECK (
        (document_type = 'dni' AND document_number ~ '^[0-9]{8}$') OR
        (document_type = 'ce' AND document_number ~ '^[0-9]{9,12}$') OR
        (document_type = 'passport' AND document_number ~ '^[A-Z0-9]{6,12}$')
    );

-- +goose Down
-- Only DNIs fit the old rule: while a person with another document works in a gym, this fails on purpose.
ALTER TABLE users
    DROP CONSTRAINT users_document_type_check,
    DROP CONSTRAINT users_document_number_check,
    ADD CONSTRAINT users_document_type_check CHECK (document_type = 'dni'),
    ADD CONSTRAINT users_document_number_check CHECK (document_number ~ '^[0-9]{8}$');
