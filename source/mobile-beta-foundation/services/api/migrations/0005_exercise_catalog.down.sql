-- 0005_exercise_catalog rollback. The catalogue is shared content and is
-- restored by re-running `api seed-exercises`, so dropping it loses no user
-- data — workout_sets keeps its exercise_id strings either way, which is
-- exactly why no foreign key was ever put between them.
DROP TABLE IF EXISTS exercise_import;
DROP TABLE IF EXISTS exercise_code_link;
DROP TABLE IF EXISTS exercise;
DROP TABLE IF EXISTS exercise_code;
