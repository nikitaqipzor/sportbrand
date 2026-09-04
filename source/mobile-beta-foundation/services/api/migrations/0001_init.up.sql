-- 0001_init: accounts, refresh tokens and user-scoped workout logging.
--
-- Every row that belongs to a person carries user_id, and the workout_sets
-- foreign key is composite (workout_id, user_id) so the database itself makes
-- it impossible to attach a set to somebody else's workout (audit finding H1).

CREATE TABLE users (
    id            uuid        PRIMARY KEY,
    email         text        NOT NULL,
    password_hash text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_email_key ON users (lower(email));

CREATE TABLE refresh_tokens (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash text        NOT NULL UNIQUE,
    issued_at  timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz
);

CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id);

CREATE TABLE workouts (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      text        NOT NULL DEFAULT '',
    status     text        NOT NULL DEFAULT 'active'
                           CHECK (status IN ('active', 'paused', 'cancelled', 'completed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workouts_id_user_id_key UNIQUE (id, user_id)
);

CREATE INDEX workouts_user_id_idx ON workouts (user_id, created_at DESC);

CREATE TABLE workout_sets (
    id                 uuid          PRIMARY KEY,
    user_id            uuid          NOT NULL,
    workout_id         uuid          NOT NULL,
    exercise_id        text          NOT NULL CHECK (char_length(exercise_id) BETWEEN 1 AND 128),
    set_number         integer       NOT NULL CHECK (set_number >= 1),
    weight_kg          numeric(6, 2) NOT NULL CHECK (weight_kg >= 0 AND weight_kg <= 1000),
    repetitions        integer       NOT NULL CHECK (repetitions BETWEEN 1 AND 100),
    rir                integer       NOT NULL CHECK (rir BETWEEN 0 AND 10),
    client_mutation_id text          NOT NULL CHECK (char_length(client_mutation_id) BETWEEN 1 AND 128),
    created_at         timestamptz   NOT NULL DEFAULT now(),
    CONSTRAINT workout_sets_owner_fkey
        FOREIGN KEY (workout_id, user_id) REFERENCES workouts (id, user_id) ON DELETE CASCADE
);

-- The idempotency guarantee for POST /workouts/{workoutId}/sets lives here, in
-- the database, not in application code.
CREATE UNIQUE INDEX workout_sets_user_mutation_key
    ON workout_sets (user_id, client_mutation_id);

CREATE INDEX workout_sets_workout_idx ON workout_sets (workout_id, set_number);
