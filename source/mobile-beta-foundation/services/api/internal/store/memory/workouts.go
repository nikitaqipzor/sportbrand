package memory

import (
	"context"
	"sort"
	"time"

	"athletica.ai/api/internal/store"
)

// ListWorkouts returns the caller's workouts, newest first, honouring the
// status/date filter and the keyset cursor. Rows of other users are not merely
// filtered out of the answer — they are never considered.
func (s *Store) ListWorkouts(_ context.Context, userID string, filter store.WorkoutFilter) ([]store.Workout, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	allowed := map[string]bool{}
	for _, status := range filter.Statuses {
		allowed[status] = true
	}

	out := []store.Workout{}
	for _, id := range s.workoutOrder {
		workout := s.workouts[id]
		if workout.UserID != userID {
			continue
		}
		if len(allowed) > 0 && !allowed[workout.Status] {
			continue
		}
		if filter.From != nil && workout.CreatedAt.Before(*filter.From) {
			continue
		}
		if filter.To != nil && !workout.CreatedAt.Before(*filter.To) {
			continue
		}
		if filter.Cursor != nil && !afterCursor(workout, *filter.Cursor) {
			continue
		}
		out = append(out, workout)
	}

	sort.SliceStable(out, func(i, j int) bool { return listLess(out[i], out[j]) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

// listLess implements ORDER BY created_at DESC, id DESC.
func listLess(a, b store.Workout) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID > b.ID
}

// afterCursor implements (created_at, id) < (cursor.created_at, cursor.id) in
// the descending list order.
func afterCursor(w store.Workout, cursor store.WorkoutCursor) bool {
	if !w.CreatedAt.Equal(cursor.CreatedAt) {
		return w.CreatedAt.Before(cursor.CreatedAt)
	}
	return w.ID < cursor.ID
}

// SetWorkoutStatus moves a workout between statuses under the same lock that
// reads it, so two concurrent transitions cannot both succeed — the PostgreSQL
// adapter gets the same guarantee from a single conditional UPDATE.
func (s *Store) SetWorkoutStatus(_ context.Context, userID, workoutID string, allowedFrom []string, next string, at time.Time) (store.Workout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workout, ok := s.workouts[workoutID]
	if !ok || workout.UserID != userID {
		return store.Workout{}, store.ErrNotFound
	}

	permitted := false
	for _, from := range allowedFrom {
		if workout.Status == from {
			permitted = true
			break
		}
	}
	if !permitted {
		return store.Workout{}, store.ErrConflict
	}

	if at.IsZero() {
		at = s.now().UTC()
	}
	workout.Status = next
	workout.UpdatedAt = at.UTC()
	if isTerminal(next) {
		ended := at.UTC()
		workout.EndedAt = &ended
	} else {
		workout.EndedAt = nil
	}
	s.workouts[workoutID] = workout
	return workout, nil
}
