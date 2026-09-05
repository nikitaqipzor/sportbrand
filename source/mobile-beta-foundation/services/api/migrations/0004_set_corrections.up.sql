-- 0004_set_corrections: correcting and removing a logged set, naming a workout
-- after it was started, and the ledger that makes both idempotent.
--
-- Nothing here weakens ownership: every new row carries user_id, every new
-- index leads with it, and the ledger's foreign key is on the user, so no
-- statement added by this migration can reach across accounts.

-- Corrections need a "last changed" stamp of their own; a set that was never
-- edited keeps updated_at = created_at.
ALTER TABLE workout_sets ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
UPDATE workout_sets SET updated_at = created_at;

-- Deletion is soft. The row keeps holding its slot in the unique index on
-- (user_id, client_mutation_id), so a replay of the *creation* out of the
-- offline outbox cannot resurrect a set the athlete removed, and a repeated
-- deletion stays a safe no-op instead of turning into a 404.
ALTER TABLE workout_sets ADD COLUMN deleted_at timestamptz;

-- The composite key a set-scoped foreign key can point at, mirroring
-- workouts_id_user_id_key. It also makes (id, user_id) the only way to address
-- a set, so a lookup cannot accidentally drop the owner from its WHERE clause.
ALTER TABLE workout_sets ADD CONSTRAINT workout_sets_id_user_id_key UNIQUE (id, user_id);

-- Reads that must skip deleted rows: the workout detail and every aggregate.
CREATE INDEX workout_sets_workout_live_idx
    ON workout_sets (workout_id, set_number) WHERE deleted_at IS NULL;
CREATE INDEX workout_sets_user_live_created_idx
    ON workout_sets (user_id, created_at) WHERE deleted_at IS NULL;
CREATE INDEX workout_sets_user_live_exercise_idx
    ON workout_sets (user_id, exercise_id, created_at) WHERE deleted_at IS NULL;

-- The idempotency ledger for mutations that do not insert a workout_sets row.
-- It is the same mechanism as the set write: a unique index decides whether a
-- queued mutation is new or a replay, in the database and not in Go.
CREATE TABLE client_mutations (
    id                 uuid        PRIMARY KEY,
    user_id            uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    client_mutation_id text        NOT NULL
                                   CHECK (char_length(client_mutation_id) BETWEEN 1 AND 128),
    kind               text        NOT NULL
                                   CHECK (kind IN ('set_update', 'set_delete', 'workout_rename')),
    target_id          uuid        NOT NULL,
    applied_at         timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX client_mutations_user_mutation_key
    ON client_mutations (user_id, client_mutation_id);

CREATE INDEX client_mutations_user_target_idx
    ON client_mutations (user_id, target_id);
