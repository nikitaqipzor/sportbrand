-- 0002_workout_lifecycle rollback.
DROP INDEX IF EXISTS refresh_tokens_revoked_at_idx;
DROP INDEX IF EXISTS refresh_tokens_expires_at_idx;
DROP INDEX IF EXISTS workout_sets_user_created_idx;
DROP INDEX IF EXISTS workout_sets_user_exercise_idx;
DROP INDEX IF EXISTS workouts_user_status_created_idx;
DROP INDEX IF EXISTS workouts_user_created_id_idx;

ALTER TABLE workouts DROP CONSTRAINT IF EXISTS workouts_ended_at_matches_status;
ALTER TABLE workouts DROP COLUMN IF EXISTS ended_at;
ALTER TABLE workouts DROP COLUMN IF EXISTS updated_at;
