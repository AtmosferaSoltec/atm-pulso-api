-- +goose Up
-- Module body_tracking (plan 08, piece 4): dated body measurements and routines of a member, plus an access log.
-- SENSITIVE PERSONAL DATA (Peruvian law 29733): only the module's endpoints read these tables; they never feed lists,
-- CSV exports or general reports. The foreign keys are DEFERRABLE INITIALLY DEFERRED so that deleting a gym (everything
-- cascades from it) is checked once all the cascades ran.

-- One row per measurement session. Only the date is mandatory, plus at least one value. Soft delete (deleted_at).
CREATE TABLE body_measurements (
    id           uuid PRIMARY KEY,
    gym_id       uuid          NOT NULL REFERENCES gyms (id) ON DELETE CASCADE,
    member_id    uuid          NOT NULL,
    measured_on  date          NOT NULL,
    weight_kg    numeric(4, 1) CHECK (weight_kg BETWEEN 20 AND 400),
    height_cm    numeric(4, 1) CHECK (height_cm BETWEEN 50 AND 250),
    body_fat_pct numeric(3, 1) CHECK (body_fat_pct BETWEEN 2 AND 70),
    waist_cm     numeric(4, 1) CHECK (waist_cm BETWEEN 20 AND 300),
    chest_cm     numeric(4, 1) CHECK (chest_cm BETWEEN 20 AND 300),
    hip_cm       numeric(4, 1) CHECK (hip_cm BETWEEN 20 AND 300),
    notes        text          NOT NULL DEFAULT '' CHECK (char_length(notes) <= 500),
    created_by   uuid          NOT NULL REFERENCES users (id) DEFERRABLE INITIALLY DEFERRED,
    created_at   timestamptz   NOT NULL DEFAULT now(),
    updated_by   uuid          NOT NULL REFERENCES users (id) DEFERRABLE INITIALLY DEFERRED,
    updated_at   timestamptz   NOT NULL DEFAULT now(),
    deleted_by   uuid          REFERENCES users (id) DEFERRABLE INITIALLY DEFERRED,
    deleted_at   timestamptz,
    CONSTRAINT body_measurements_id_gym_key UNIQUE (id, gym_id),
    CONSTRAINT body_measurements_member_fkey FOREIGN KEY (member_id, gym_id) REFERENCES members (id, gym_id) DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT body_measurements_has_value CHECK (num_nonnulls(weight_kg, height_cm, body_fat_pct, waist_cm, chest_cm, hip_cm) >= 1),
    CONSTRAINT body_measurements_deleted_pair CHECK ((deleted_at IS NULL) = (deleted_by IS NULL))
);
CREATE INDEX body_measurements_member_idx ON body_measurements (gym_id, member_id, measured_on DESC) WHERE deleted_at IS NULL;

-- Routines: free text, dated. is_current marks the ones in force (several may be; the list shows them first).
CREATE TABLE body_routines (
    id         uuid PRIMARY KEY,
    gym_id     uuid         NOT NULL REFERENCES gyms (id) ON DELETE CASCADE,
    member_id  uuid         NOT NULL,
    started_on date         NOT NULL,
    title      text         NOT NULL CHECK (btrim(title) <> '' AND char_length(title) <= 80),
    content    text         NOT NULL DEFAULT '' CHECK (char_length(content) <= 5000),
    notes      text         NOT NULL DEFAULT '' CHECK (char_length(notes) <= 500),
    is_current boolean      NOT NULL DEFAULT true,
    created_by uuid         NOT NULL REFERENCES users (id) DEFERRABLE INITIALLY DEFERRED,
    created_at timestamptz  NOT NULL DEFAULT now(),
    updated_by uuid         NOT NULL REFERENCES users (id) DEFERRABLE INITIALLY DEFERRED,
    updated_at timestamptz  NOT NULL DEFAULT now(),
    deleted_by uuid         REFERENCES users (id) DEFERRABLE INITIALLY DEFERRED,
    deleted_at timestamptz,
    CONSTRAINT body_routines_id_gym_key UNIQUE (id, gym_id),
    CONSTRAINT body_routines_member_fkey FOREIGN KEY (member_id, gym_id) REFERENCES members (id, gym_id) DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT body_routines_deleted_pair CHECK ((deleted_at IS NULL) = (deleted_by IS NULL))
);
CREATE INDEX body_routines_member_idx ON body_routines (gym_id, member_id, is_current DESC, started_on DESC) WHERE deleted_at IS NULL;

-- Who read or wrote the health data of a member. Ids and the action only: NEVER the values. Append-only.
-- resource_id is the record written (null for a read of the whole list).
CREATE TABLE body_access_log (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    gym_id      uuid        NOT NULL REFERENCES gyms (id) ON DELETE CASCADE,
    member_id   uuid        NOT NULL,
    user_id     uuid        NOT NULL REFERENCES users (id) DEFERRABLE INITIALLY DEFERRED,
    action      text        NOT NULL CHECK (action IN ('read', 'create', 'update', 'delete')),
    resource    text        NOT NULL CHECK (resource IN ('measurements', 'routines')),
    resource_id uuid,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT body_access_log_member_fkey FOREIGN KEY (member_id, gym_id) REFERENCES members (id, gym_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX body_access_log_member_idx ON body_access_log (gym_id, member_id, occurred_at DESC);

-- +goose Down
DROP TABLE body_access_log;
DROP TABLE body_routines;
DROP TABLE body_measurements;
