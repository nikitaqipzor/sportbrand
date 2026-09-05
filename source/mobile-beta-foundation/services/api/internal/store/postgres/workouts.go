package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

// ListWorkouts pages the caller's workouts with a keyset cursor.
//
// The WHERE clause always starts with user_id, and the cursor comparison is the
// row-wise `(created_at, id) < ($cursor)`, which matches the composite index
// exactly and cannot skip or repeat a row while new workouts are being created.
func (s *Store) ListWorkouts(ctx context.Context, userID string, filter store.WorkoutFilter) ([]store.Workout, error) {
	if !ids.IsUUID(userID) {
		return []store.Workout{}, nil
	}

	args := []any{userID}
	var where strings.Builder
	where.WriteString(`user_id = $1`)

	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if len(filter.Statuses) > 0 {
		where.WriteString(` AND status = ANY(` + next(filter.Statuses) + `)`)
	}
	if filter.From != nil {
		where.WriteString(` AND created_at >= ` + next(*filter.From))
	}
	if filter.To != nil {
		where.WriteString(` AND created_at < ` + next(*filter.To))
	}
	if filter.Cursor != nil {
		where.WriteString(` AND (created_at, id) < (` + next(filter.Cursor.CreatedAt) + `, ` + next(filter.Cursor.ID) + `::uuid)`)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT ` + workoutColumns + ` FROM workouts WHERE ` + where.String() +
		` ORDER BY created_at DESC, id DESC LIMIT ` + next(limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list workouts: %w", err)
	}
	defer rows.Close()

	out := []store.Workout{}
	for rows.Next() {
		workout, err := scanWorkout(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan workout: %w", err)
		}
		out = append(out, workout)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list workouts: %w", err)
	}
	return out, nil
}

// SetWorkoutStatus applies a transition in one conditional UPDATE: the
// allowed-from check and the write happen in the same statement, so two
// concurrent requests can never both observe "active" and both win.
func (s *Store) SetWorkoutStatus(ctx context.Context, userID, workoutID string, allowedFrom []string, next string, at time.Time) (store.Workout, error) {
	if !ids.IsUUID(workoutID) || !ids.IsUUID(userID) {
		return store.Workout{}, store.ErrNotFound
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}

	var endedAt *time.Time
	if next == store.StatusCompleted || next == store.StatusCancelled {
		ended := at.UTC()
		endedAt = &ended
	}

	const q = `UPDATE workouts
	              SET status = $3, updated_at = $4, ended_at = $5
	            WHERE id = $1 AND user_id = $2 AND status = ANY($6)
	        RETURNING ` + workoutColumns

	workout, err := scanWorkout(s.pool.QueryRow(ctx, q, workoutID, userID, next, at.UTC(), endedAt, allowedFrom))
	switch {
	case err == nil:
		return workout, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Nothing was updated: either the workout is missing/foreign, or it
		// exists but sits in a status this transition does not allow. The two
		// answers differ (404 vs 409) but neither reveals a foreign workout.
		if _, lookupErr := s.WorkoutForUser(ctx, userID, workoutID); lookupErr != nil {
			return store.Workout{}, lookupErr
		}
		return store.Workout{}, store.ErrConflict
	default:
		return store.Workout{}, fmt.Errorf("postgres: set workout status: %w", err)
	}
}
