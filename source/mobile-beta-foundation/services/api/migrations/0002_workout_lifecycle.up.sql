-- 0002_workout_lifecycle: workout status transitions, list/paging and progress.
--
-- Nothing here loosens ownership: every new index leads with user_id, so the
-- planner can only ever walk one user's rows (audit finding H1).

ALTER TABLE workouts
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN ended_at   timestamptz;

-- Existing rows predate the column; make them consistent instead of "now".
UPDATE workouts SET updated_at = created_at;
UPDATE workouts SET ended_at = created_at WHERE status IN ('completed', 'cancelled');

-- ended_at exists exactly for the terminal statuses, which keeps a half-applied
-- transition impossible to represent.
ALTER TABLE workouts
    ADD CONSTRAINT workouts_ended_at_matches_status
    CHECK ((status IN ('completed', 'cancelled')) = (ended_at IS NOT NULL));

-- GET /workouts pages by (created_at DESC, id DESC); the id keeps the order
-- total, so a cursor can neither skip nor repeat a row.
CREATE INDEX workouts_user_created_id_idx ON workouts (user_id, created_at DESC, id DESC);
CREATE INDEX workouts_user_status_created_idx ON workouts (user_id, status, created_at DESC, id DESC);

-- Progress aggregates: strength records per exercise, volume per week.
CREATE INDEX workout_sets_user_exercise_idx ON workout_sets (user_id, exercise_id, created_at);
CREATE INDEX workout_sets_user_created_idx ON workout_sets (user_id, created_at);

-- The refresh-token sweep deletes by expiry and by revocation time.
CREATE INDEX refresh_tokens_expires_at_idx ON refresh_tokens (expires_at);
CREATE INDEX refresh_tokens_revoked_at_idx ON refresh_tokens (revoked_at) WHERE revoked_at IS NOT NULL;
