package memory

import (
	"context"
	"sort"

	"athletica.ai/api/internal/store"
)

// progressSets returns the caller's sets inside the window that count towards
// progress: sets of cancelled workouts are excluded, exactly as the SQL does.
// The caller must already hold s.mu.
func (s *Store) progressSets(userID string, window store.ProgressWindow) []store.WorkoutSet {
	out := make([]store.WorkoutSet, 0, len(s.setOrder))
	for _, id := range s.setOrder {
		set := s.sets[id]
		if set.UserID != userID {
			continue
		}
		// A deleted set counts towards nothing: the athlete removed it, so it
		// must not survive in a personal record or a weekly volume figure.
		if !set.Live() {
			continue
		}
		if set.CreatedAt.Before(window.From) || !set.CreatedAt.Before(window.To) {
			continue
		}
		workout, ok := s.workouts[set.WorkoutID]
		if !ok || workout.UserID != userID || workout.Status == store.StatusCancelled {
			continue
		}
		out = append(out, set)
	}
	return out
}

// ExerciseRecords aggregates strength records per exercise.
func (s *Store) ExerciseRecords(_ context.Context, userID string, window store.ProgressWindow, limit int) ([]store.ExerciseRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byExercise := map[string]*store.ExerciseRecord{}
	for _, set := range s.progressSets(userID, window) {
		record, ok := byExercise[set.ExerciseID]
		if !ok {
			record = &store.ExerciseRecord{ExerciseID: set.ExerciseID}
			byExercise[set.ExerciseID] = record
		}
		record.Sets++
		record.Repetitions += set.Repetitions
		record.VolumeKg += set.WeightKg * float64(set.Repetitions)
		if set.CreatedAt.After(record.LastPerformedAt) {
			record.LastPerformedAt = set.CreatedAt
		}

		// Heaviest set: weight first, then reps, earliest occurrence wins so
		// the record keeps the date it was actually first achieved.
		if set.WeightKg > record.BestWeightKg ||
			(set.WeightKg == record.BestWeightKg && set.Repetitions > record.BestWeightReps) {
			record.BestWeightKg = set.WeightKg
			record.BestWeightReps = set.Repetitions
			record.BestWeightAt = set.CreatedAt
		}

		if e1rm := store.Estimated1RM(set.WeightKg, set.Repetitions); e1rm > record.BestEstimated1RM {
			record.BestEstimated1RM = e1rm
			record.Best1RMWeightKg = set.WeightKg
			record.Best1RMReps = set.Repetitions
			record.Best1RMAt = set.CreatedAt
		}
	}

	out := make([]store.ExerciseRecord, 0, len(byExercise))
	for _, record := range byExercise {
		out = append(out, *record)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BestEstimated1RM != out[j].BestEstimated1RM {
			return out[i].BestEstimated1RM > out[j].BestEstimated1RM
		}
		return out[i].ExerciseID < out[j].ExerciseID
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// WeeklyVolume aggregates volume per ISO week (Monday 00:00 UTC).
func (s *Store) WeeklyVolume(_ context.Context, userID string, window store.ProgressWindow) ([]store.WeeklyVolume, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type bucket struct {
		volume   store.WeeklyVolume
		workouts map[string]struct{}
	}
	buckets := map[int64]*bucket{}
	for _, set := range s.progressSets(userID, window) {
		week := store.WeekStart(set.CreatedAt)
		key := week.Unix()
		b, ok := buckets[key]
		if !ok {
			b = &bucket{volume: store.WeeklyVolume{WeekStart: week}, workouts: map[string]struct{}{}}
			buckets[key] = b
		}
		b.volume.Sets++
		b.volume.Repetitions += set.Repetitions
		b.volume.VolumeKg += set.WeightKg * float64(set.Repetitions)
		b.workouts[set.WorkoutID] = struct{}{}
	}

	out := make([]store.WeeklyVolume, 0, len(buckets))
	for _, b := range buckets {
		b.volume.Workouts = len(b.workouts)
		out = append(out, b.volume)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WeekStart.Before(out[j].WeekStart) })
	return out, nil
}

// WeeklyAdherence counts how the workouts started in each ISO week ended.
func (s *Store) WeeklyAdherence(_ context.Context, userID string, window store.ProgressWindow) ([]store.WeeklyAdherence, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	buckets := map[int64]*store.WeeklyAdherence{}
	for _, id := range s.workoutOrder {
		workout := s.workouts[id]
		if workout.UserID != userID {
			continue
		}
		if workout.CreatedAt.Before(window.From) || !workout.CreatedAt.Before(window.To) {
			continue
		}
		week := store.WeekStart(workout.CreatedAt)
		row, ok := buckets[week.Unix()]
		if !ok {
			row = &store.WeeklyAdherence{WeekStart: week}
			buckets[week.Unix()] = row
		}
		row.Started++
		switch workout.Status {
		case store.StatusCompleted:
			row.Completed++
		case store.StatusCancelled:
			row.Cancelled++
		default:
			row.InProgress++
		}
	}

	out := make([]store.WeeklyAdherence, 0, len(buckets))
	for _, row := range buckets {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WeekStart.Before(out[j].WeekStart) })
	return out, nil
}
