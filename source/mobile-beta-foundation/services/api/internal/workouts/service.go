package workouts

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

// StatusActive is the state a freshly created workout starts in.
const StatusActive = store.StatusActive

// Service implements the workout-logging use cases.
type Service struct {
	store store.Store
	now   func() time.Time
}

// NewService wires the workouts service. now is injectable so tests can drive
// the lifecycle timestamps deterministically.
func NewService(st store.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: st, now: now}
}

// CreateWorkout starts a session owned by userID.
func (s *Service) CreateWorkout(ctx context.Context, userID, title string) (store.Workout, error) {
	title = strings.TrimSpace(title)
	if len(title) > MaxWorkoutTitle {
		return store.Workout{}, &ValidationError{Issues: []Issue{{"title", fmt.Sprintf("must be at most %d characters", MaxWorkoutTitle)}}}
	}
	return s.store.CreateWorkout(ctx, store.Workout{
		ID:     ids.NewUUID(),
		UserID: userID,
		Title:  title,
		Status: StatusActive,
	})
}

// LogSet writes one set idempotently.
//
// Contract:
//   - userID always comes from the caller's access token;
//   - a workout that does not exist and a workout owned by somebody else are
//     both reported as store.ErrNotFound, so existence never leaks;
//   - a repeat of (userID, clientMutationID) returns the stored row with
//     created=false and never produces a second row — that is guaranteed by the
//     unique index in the database, not by this function.
func (s *Service) LogSet(ctx context.Context, userID string, in SetInput) (store.WorkoutSet, bool, error) {
	if strings.TrimSpace(userID) == "" {
		return store.WorkoutSet{}, false, errors.New("workouts: user id is required")
	}
	// Round to the stored precision *before* validating, so a value such as
	// 1000.004 cannot pass the bound check and then trip the numeric(6,2)
	// CHECK constraint in the database.
	in.WeightKg = roundWeight(in.WeightKg)
	if verr := ValidateSet(in); verr != nil {
		return store.WorkoutSet{}, false, verr
	}
	// Ownership is checked before anything is written, and the composite
	// foreign key (workout_id, user_id) backs the check up in the database.
	if _, err := s.store.WorkoutForUser(ctx, userID, in.WorkoutID); err != nil {
		return store.WorkoutSet{}, false, err
	}

	return s.store.InsertWorkoutSet(ctx, store.WorkoutSet{
		ID:               ids.NewUUID(),
		UserID:           userID,
		WorkoutID:        in.WorkoutID,
		ExerciseID:       in.ExerciseID,
		SetNumber:        in.SetNumber,
		WeightKg:         in.WeightKg,
		Repetitions:      in.Repetitions,
		RIR:              in.RIR,
		ClientMutationID: in.ClientMutationID,
	})
}

// ListSets returns the sets of one of the caller's workouts.
func (s *Service) ListSets(ctx context.Context, userID, workoutID string) ([]store.WorkoutSet, error) {
	return s.store.ListWorkoutSets(ctx, userID, workoutID)
}

// roundWeight matches the numeric(6,2) column so the value the API echoes back
// is the value the database stores.
func roundWeight(kg float64) float64 {
	return math.Round(kg*100) / 100
}
