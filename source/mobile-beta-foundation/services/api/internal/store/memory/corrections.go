package memory

import (
	"context"
	"strings"

	"athletica.ai/api/internal/store"
)

// This file is the in-memory twin of postgres/corrections.go. Everything here
// happens under the single write lock, which is the in-process equivalent of
// the one transaction the SQL adapter uses: claiming the mutation ID and
// applying the change can never be observed half-done.

// claimMutation reserves (userID, clientMutationID) for kind/targetID.
//
// It returns claimed=false when the ID was already spent on exactly this
// change — that is a replay — and store.ErrMutationReused when it was spent on
// something else. The caller must already hold s.mu for writing.
func (s *Store) claimMutation(userID, clientMutationID, kind, targetID string) (bool, error) {
	if strings.TrimSpace(clientMutationID) == "" {
		// An unlabelled mutation is not deduplicated; the caller decided that
		// its effect converges (a rename), so it simply applies.
		return true, nil
	}
	key := mutationKey{userID: userID, clientMutationID: clientMutationID}
	if existing, spent := s.mutations[key]; spent {
		if existing.kind != kind || existing.targetID != targetID {
			return false, store.ErrMutationReused
		}
		return false, nil
	}
	s.mutations[key] = mutationRecord{kind: kind, targetID: targetID}
	return true, nil
}

// setForUpdate resolves a set inside one of the caller's workouts, deleted rows
// included. The caller must already hold s.mu.
//
// Ownership is checked on both the workout and the set, and a set that is not
// part of the addressed workout is reported as missing rather than as a
// mismatch, so a probe cannot learn that the ID exists elsewhere.
func (s *Store) setForUpdate(userID, workoutID, setID string) (store.WorkoutSet, store.Workout, error) {
	workout, ok := s.workouts[workoutID]
	if !ok || workout.UserID != userID {
		return store.WorkoutSet{}, store.Workout{}, store.ErrNotFound
	}
	set, ok := s.sets[setID]
	if !ok || set.UserID != userID || set.WorkoutID != workoutID {
		return store.WorkoutSet{}, store.Workout{}, store.ErrNotFound
	}
	return set, workout, nil
}

// UpdateWorkoutSet corrects the three mistypeable values of a stored set.
func (s *Store) UpdateWorkoutSet(_ context.Context, in store.SetUpdate) (store.WorkoutSet, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	set, workout, err := s.setForUpdate(in.UserID, in.WorkoutID, in.SetID)
	if err != nil {
		return store.WorkoutSet{}, false, err
	}
	// A cancelled session was thrown away; its sets already count towards
	// nothing, and rewriting them would only add noise to a discarded log.
	if workout.Status == store.StatusCancelled {
		return store.WorkoutSet{}, false, store.ErrConflict
	}
	if !set.Live() {
		return store.WorkoutSet{}, false, store.ErrGone
	}

	claimed, err := s.claimMutation(in.UserID, in.ClientMutationID, store.MutationSetUpdate, in.SetID)
	if err != nil {
		return store.WorkoutSet{}, false, err
	}
	if !claimed {
		// The edit was already applied. Hand back the set as it stands, which
		// is what the first application left behind.
		return set, false, nil
	}

	at := in.At
	if at.IsZero() {
		at = s.now()
	}
	set.WeightKg = in.WeightKg
	set.Repetitions = in.Repetitions
	set.RIR = in.RIR
	set.UpdatedAt = at.UTC()
	s.sets[set.ID] = set
	return set, true, nil
}

// DeleteWorkoutSet soft-deletes a set. Repeating it is always safe.
func (s *Store) DeleteWorkoutSet(_ context.Context, in store.SetDeletion) (store.WorkoutSet, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	set, workout, err := s.setForUpdate(in.UserID, in.WorkoutID, in.SetID)
	if err != nil {
		return store.WorkoutSet{}, false, err
	}
	if workout.Status == store.StatusCancelled {
		return store.WorkoutSet{}, false, store.ErrConflict
	}
	// An already-deleted set is the end state a deletion asks for, so a repeat
	// converges instead of failing — whatever mutation ID it carries.
	if !set.Live() {
		return set, false, nil
	}

	claimed, err := s.claimMutation(in.UserID, in.ClientMutationID, store.MutationSetDelete, in.SetID)
	if err != nil {
		return store.WorkoutSet{}, false, err
	}
	if !claimed {
		return set, false, nil
	}

	at := in.At
	if at.IsZero() {
		at = s.now()
	}
	deleted := at.UTC()
	set.DeletedAt = &deleted
	set.UpdatedAt = deleted
	s.sets[set.ID] = set
	return set, true, nil
}

// RenameWorkout gives a workout its title after the fact.
func (s *Store) RenameWorkout(_ context.Context, in store.WorkoutRename) (store.Workout, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workout, ok := s.workouts[in.WorkoutID]
	if !ok || workout.UserID != in.UserID {
		return store.Workout{}, false, store.ErrNotFound
	}

	claimed, err := s.claimMutation(in.UserID, in.ClientMutationID, store.MutationWorkoutRename, in.WorkoutID)
	if err != nil {
		return store.Workout{}, false, err
	}
	if !claimed {
		return workout, false, nil
	}

	at := in.At
	if at.IsZero() {
		at = s.now()
	}
	workout.Title = in.Title
	workout.UpdatedAt = at.UTC()
	s.workouts[in.WorkoutID] = workout
	return workout, true, nil
}
