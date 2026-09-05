package workouts

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"athletica.ai/api/internal/store"
)

// Correcting a logged set, and naming a workout after it was started.
//
// Both are mutations that arrive from the offline outbox exactly like the set
// write does, so both carry their own clientMutationId and both are settled by
// a unique index in the database rather than by a check in this file.
//
// What is deliberately *not* correctable: exerciseId and setNumber. The
// client's mutation ID is derived as `workoutId:exerciseId:setNumber`, so those
// two are the identity of the queued write; letting them move would make an
// already-spent ID name a different set. For the same reason the server never
// renumbers the remaining sets after one in the middle is deleted — the numbers
// are data the athlete produced, not positions in a list, and gaps are normal.

// SetUpdateInput is a validated request to correct one stored set.
//
// All three values are required rather than optional: a replay out of the
// outbox is then byte-identical to the mutation that was applied, and a partial
// patch can never silently reset the field it omitted.
type SetUpdateInput struct {
	WorkoutID        string
	SetID            string
	WeightKg         float64
	Repetitions      int
	RIR              int
	ClientMutationID string
}

// SetDeleteInput is a validated request to remove one stored set.
type SetDeleteInput struct {
	WorkoutID        string
	SetID            string
	ClientMutationID string
}

// RenameInput is a validated request to (re)name a workout. ClientMutationID
// is optional: a rename converges on the title it carries, so an unlabelled one
// is last-write-wins, while a labelled one is deduplicated like anything else
// coming out of the outbox.
type RenameInput struct {
	WorkoutID        string
	Title            string
	ClientMutationID string
}

// ValidateSetUpdate holds a correction to the very same domain bounds as the
// original write, so a set can never be edited out of range after the fact.
func ValidateSetUpdate(in SetUpdateInput) *ValidationError {
	var issues []Issue

	issues = appendIdentifierIssues(issues, "workoutId", in.WorkoutID)
	issues = appendIdentifierIssues(issues, "setId", in.SetID)
	issues = appendIdentifierIssues(issues, "clientMutationId", in.ClientMutationID)
	issues = appendValueIssues(issues, in.WeightKg, in.Repetitions, in.RIR)

	if len(issues) == 0 {
		return nil
	}
	return &ValidationError{Issues: issues}
}

// UpdateSet corrects one of the caller's sets.
//
//   - userID always comes from the access token;
//   - a missing set, a foreign set and a set belonging to another workout are
//     all store.ErrNotFound, so existence never leaks;
//   - replaying the same clientMutationId reports applied=false and returns the
//     set as it stands — the edit is not applied a second time.
func (s *Service) UpdateSet(ctx context.Context, userID string, in SetUpdateInput) (store.WorkoutSet, bool, error) {
	if strings.TrimSpace(userID) == "" {
		return store.WorkoutSet{}, false, errors.New("workouts: user id is required")
	}
	// Round to the stored precision before validating, so a value such as
	// 1000.004 cannot pass the bound check and then trip numeric(6,2).
	in.WeightKg = roundWeight(in.WeightKg)
	if verr := ValidateSetUpdate(in); verr != nil {
		return store.WorkoutSet{}, false, verr
	}

	return s.store.UpdateWorkoutSet(ctx, store.SetUpdate{
		UserID:           userID,
		WorkoutID:        in.WorkoutID,
		SetID:            in.SetID,
		WeightKg:         in.WeightKg,
		Repetitions:      in.Repetitions,
		RIR:              in.RIR,
		ClientMutationID: in.ClientMutationID,
		At:               s.now().UTC(),
	})
}

// DeleteSet removes one of the caller's sets. The deletion is soft: the row
// keeps its (user_id, client_mutation_id) slot so a replayed *creation* cannot
// bring it back, and it stops counting everywhere the athlete can see it.
func (s *Service) DeleteSet(ctx context.Context, userID string, in SetDeleteInput) (store.WorkoutSet, bool, error) {
	if strings.TrimSpace(userID) == "" {
		return store.WorkoutSet{}, false, errors.New("workouts: user id is required")
	}

	var issues []Issue
	issues = appendIdentifierIssues(issues, "workoutId", in.WorkoutID)
	issues = appendIdentifierIssues(issues, "setId", in.SetID)
	issues = appendIdentifierIssues(issues, "clientMutationId", in.ClientMutationID)
	if len(issues) > 0 {
		return store.WorkoutSet{}, false, &ValidationError{Issues: issues}
	}

	return s.store.DeleteWorkoutSet(ctx, store.SetDeletion{
		UserID:           userID,
		WorkoutID:        in.WorkoutID,
		SetID:            in.SetID,
		ClientMutationID: in.ClientMutationID,
		At:               s.now().UTC(),
	})
}

// Rename gives a workout its title — the session started offline with no name
// is the case this exists for.
func (s *Service) Rename(ctx context.Context, userID string, in RenameInput) (store.Workout, bool, error) {
	if strings.TrimSpace(userID) == "" {
		return store.Workout{}, false, errors.New("workouts: user id is required")
	}

	title := strings.TrimSpace(in.Title)
	var issues []Issue
	if len(title) > MaxWorkoutTitle {
		issues = append(issues, Issue{"title", fmt.Sprintf("must be at most %d characters", MaxWorkoutTitle)})
	}
	if mutation := strings.TrimSpace(in.ClientMutationID); mutation != "" && len(in.ClientMutationID) > MaxIdentifierLen {
		issues = append(issues, Issue{"clientMutationId", fmt.Sprintf("must be at most %d characters", MaxIdentifierLen)})
	}
	if len(issues) > 0 {
		return store.Workout{}, false, &ValidationError{Issues: issues}
	}

	return s.store.RenameWorkout(ctx, store.WorkoutRename{
		UserID:           userID,
		WorkoutID:        in.WorkoutID,
		Title:            title,
		ClientMutationID: strings.TrimSpace(in.ClientMutationID),
		At:               s.now().UTC(),
	})
}
