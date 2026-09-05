package postgres

import (
	"context"
	"fmt"
	"time"

	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

// progressScope is shared by every aggregate: one user's sets inside the
// window, excluding sets of cancelled workouts. The join on
// (id, user_id) is what keeps a foreign row out even in the presence of a bug.
const progressScope = `
	FROM workout_sets s
	JOIN workouts w ON w.id = s.workout_id AND w.user_id = s.user_id
	WHERE s.user_id = $1 AND s.created_at >= $2 AND s.created_at < $3
	  AND w.status <> 'cancelled'`

// ExerciseRecords aggregates strength records per exercise. Every value is
// computed by PostgreSQL; not a single set row travels into the service.
func (s *Store) ExerciseRecords(ctx context.Context, userID string, window store.ProgressWindow, limit int) ([]store.ExerciseRecord, error) {
	if !ids.IsUUID(userID) {
		return []store.ExerciseRecord{}, nil
	}
	if limit <= 0 {
		limit = 50
	}

	// The Epley estimate mirrors store.Estimated1RM exactly.
	const q = `
WITH scoped AS (
	SELECT s.exercise_id,
	       s.weight_kg::double precision                                        AS weight_kg,
	       s.repetitions,
	       s.created_at,
	       (s.weight_kg::double precision * (1 + s.repetitions / 30.0))          AS e1rm
	` + progressScope + `
),
agg AS (
	SELECT exercise_id,
	       count(*)                             AS sets,
	       sum(repetitions)                     AS repetitions,
	       sum(weight_kg * repetitions)         AS volume_kg,
	       max(created_at)                      AS last_performed_at
	FROM scoped GROUP BY exercise_id
),
heaviest AS (
	SELECT DISTINCT ON (exercise_id) exercise_id, weight_kg, repetitions, created_at
	FROM scoped ORDER BY exercise_id, weight_kg DESC, repetitions DESC, created_at ASC
),
strongest AS (
	SELECT DISTINCT ON (exercise_id) exercise_id, e1rm, weight_kg, repetitions, created_at
	FROM scoped ORDER BY exercise_id, e1rm DESC, created_at ASC
)
SELECT a.exercise_id, a.sets, a.repetitions, a.volume_kg,
       h.weight_kg, h.repetitions, h.created_at,
       st.e1rm, st.weight_kg, st.repetitions, st.created_at,
       a.last_performed_at
FROM agg a
JOIN heaviest  h  ON h.exercise_id = a.exercise_id
JOIN strongest st ON st.exercise_id = a.exercise_id
ORDER BY st.e1rm DESC, a.exercise_id
LIMIT $4`

	rows, err := s.pool.Query(ctx, q, userID, window.From, window.To, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: exercise records: %w", err)
	}
	defer rows.Close()

	out := []store.ExerciseRecord{}
	for rows.Next() {
		var r store.ExerciseRecord
		if err := rows.Scan(&r.ExerciseID, &r.Sets, &r.Repetitions, &r.VolumeKg,
			&r.BestWeightKg, &r.BestWeightReps, &r.BestWeightAt,
			&r.BestEstimated1RM, &r.Best1RMWeightKg, &r.Best1RMReps, &r.Best1RMAt,
			&r.LastPerformedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan exercise record: %w", err)
		}
		r.BestWeightAt = r.BestWeightAt.UTC()
		r.Best1RMAt = r.Best1RMAt.UTC()
		r.LastPerformedAt = r.LastPerformedAt.UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: exercise records: %w", err)
	}
	return out, nil
}

// WeeklyVolume aggregates volume per ISO week, grouped by the database.
func (s *Store) WeeklyVolume(ctx context.Context, userID string, window store.ProgressWindow) ([]store.WeeklyVolume, error) {
	if !ids.IsUUID(userID) {
		return []store.WeeklyVolume{}, nil
	}

	const q = `
SELECT date_trunc('week', s.created_at AT TIME ZONE 'UTC') AS week_start,
       count(*)                                            AS sets,
       sum(s.repetitions)                                  AS repetitions,
       sum(s.weight_kg::double precision * s.repetitions)   AS volume_kg,
       count(DISTINCT s.workout_id)                        AS workouts
` + progressScope + `
GROUP BY week_start
ORDER BY week_start`

	rows, err := s.pool.Query(ctx, q, userID, window.From, window.To)
	if err != nil {
		return nil, fmt.Errorf("postgres: weekly volume: %w", err)
	}
	defer rows.Close()

	out := []store.WeeklyVolume{}
	for rows.Next() {
		var v store.WeeklyVolume
		var week time.Time
		if err := rows.Scan(&week, &v.Sets, &v.Repetitions, &v.VolumeKg, &v.Workouts); err != nil {
			return nil, fmt.Errorf("postgres: scan weekly volume: %w", err)
		}
		v.WeekStart = time.Date(week.Year(), week.Month(), week.Day(), 0, 0, 0, 0, time.UTC)
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: weekly volume: %w", err)
	}
	return out, nil
}

// WeeklyAdherence counts how the workouts started in each ISO week ended.
func (s *Store) WeeklyAdherence(ctx context.Context, userID string, window store.ProgressWindow) ([]store.WeeklyAdherence, error) {
	if !ids.IsUUID(userID) {
		return []store.WeeklyAdherence{}, nil
	}

	const q = `
SELECT date_trunc('week', created_at AT TIME ZONE 'UTC')            AS week_start,
       count(*)                                                     AS started,
       count(*) FILTER (WHERE status = 'completed')                  AS completed,
       count(*) FILTER (WHERE status = 'cancelled')                  AS cancelled,
       count(*) FILTER (WHERE status IN ('active', 'paused'))        AS in_progress
FROM workouts
WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
GROUP BY week_start
ORDER BY week_start`

	rows, err := s.pool.Query(ctx, q, userID, window.From, window.To)
	if err != nil {
		return nil, fmt.Errorf("postgres: weekly adherence: %w", err)
	}
	defer rows.Close()

	out := []store.WeeklyAdherence{}
	for rows.Next() {
		var a store.WeeklyAdherence
		var week time.Time
		if err := rows.Scan(&week, &a.Started, &a.Completed, &a.Cancelled, &a.InProgress); err != nil {
			return nil, fmt.Errorf("postgres: scan weekly adherence: %w", err)
		}
		a.WeekStart = time.Date(week.Year(), week.Month(), week.Day(), 0, 0, 0, 0, time.UTC)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: weekly adherence: %w", err)
	}
	return out, nil
}
