-- 0004_set_corrections rollback.
DROP TABLE IF EXISTS client_mutations;

DROP INDEX IF EXISTS workout_sets_user_live_exercise_idx;
DROP INDEX IF EXISTS workout_sets_user_live_created_idx;
DROP INDEX IF EXISTS workout_sets_workout_live_idx;

ALTER TABLE workout_sets DROP CONSTRAINT IF EXISTS workout_sets_id_user_id_key;
ALTER TABLE workout_sets DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE workout_sets DROP COLUMN IF EXISTS updated_at;
