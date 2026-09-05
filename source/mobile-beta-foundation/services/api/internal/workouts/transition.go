package workouts

import (
	"context"
	"errors"
	"strings"

	"athletica.ai/api/internal/store"
)

// Detail is a workout together with its own sets — the payload the "Итоги"
// screen renders.
type Detail struct {
	Workout store.Workout
	Sets    []store.WorkoutSet
}

// Workout returns one of the caller's workouts with its sets. A workout owned
// by somebody else answers store.ErrNotFound, exactly like a missing one.
func (s *Service) Workout(ctx context.Context, userID, workoutID string) (Detail, error) {
	workout, err := s.store.WorkoutForUser(ctx, userID, workoutID)
	if err != nil {
		return Detail{}, err
	}
	sets, err := s.store.ListWorkoutSets(ctx, userID, workoutID)
	if err != nil {
		return Detail{}, err
	}
	return Detail{Workout: workout, Sets: sets}, nil
}

// Transition moves one of the caller's workouts to next.
//
// Three outcomes matter to the client and each has its own error:
//
//   - ErrUnknownStatus  — next is not a domain status (422);
//   - store.ErrNotFound — the workout is missing or foreign (404, identical);
//   - ErrInvalidTransition — the workout exists but its current status forbids
//     the move, e.g. completing a cancelled session (409, never 500).
//
// Asking for the status the workout already holds is a no-op that succeeds, so
// a client retrying a request whose response it never saw is not punished with
// a 409 for work it already did.
func (s *Service) Transition(ctx context.Context, userID, workoutID, next string) (store.Workout, error) {
	next = strings.ToLower(strings.TrimSpace(next))
	if !IsStatus(next) {
		return store.Workout{}, ErrUnknownStatus
	}
	if strings.TrimSpace(userID) == "" {
		return store.Workout{}, errors.New("workouts: user id is required")
	}

	// allowedFrom includes next itself, which makes a repeated request
	// idempotent; the store applies the check and the write atomically.
	allowedFrom := append(StatusesReaching(next), next)

	workout, err := s.store.SetWorkoutStatus(ctx, userID, workoutID, allowedFrom, next, s.now().UTC())
	if errors.Is(err, store.ErrConflict) {
		return store.Workout{}, ErrInvalidTransition
	}
	return workout, err
}
