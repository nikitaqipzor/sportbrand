// Package workouts holds the training-log use cases and the domain validation
// shared with packages/domain/src/workout.ts on the client.
package workouts

import (
	"fmt"
	"math"
	"strings"
)

// Domain bounds, kept byte-for-byte in step with validateSet() in
// packages/domain/src/workout.ts.
const (
	MinSetNumber      = 1
	MinWeightKg       = 0.0
	MaxWeightKg       = 1000.0
	MinRepetitions    = 1
	MaxRepetitions    = 100
	MinRIR            = 0
	MaxRIR            = 10
	MaxIdentifierLen  = 128
	MaxWorkoutTitle   = 200
	weightKgPrecision = 2
)

// Issue is one field-level validation problem.
type Issue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError carries every issue found in a request.
type ValidationError struct {
	Issues []Issue
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		parts = append(parts, issue.Field+": "+issue.Message)
	}
	return "workouts: validation failed (" + strings.Join(parts, "; ") + ")"
}

// SetInput is a validated-on-entry request to log one set. It deliberately has
// no UserID field: the owner is passed separately, straight from the access
// token, so a request body can never influence it (audit finding H1).
type SetInput struct {
	WorkoutID        string
	ExerciseID       string
	SetNumber        int
	WeightKg         float64
	Repetitions      int
	RIR              int
	ClientMutationID string
}

// ValidateSet returns every problem with the input, or nil when it is sound.
func ValidateSet(in SetInput) *ValidationError {
	var issues []Issue

	issues = appendIdentifierIssues(issues, "workoutId", in.WorkoutID)
	issues = appendIdentifierIssues(issues, "exerciseId", in.ExerciseID)
	issues = appendIdentifierIssues(issues, "clientMutationId", in.ClientMutationID)

	if in.SetNumber < MinSetNumber {
		issues = append(issues, Issue{"setNumber", fmt.Sprintf("set number must be an integer >= %d", MinSetNumber)})
	}
	issues = appendValueIssues(issues, in.WeightKg, in.Repetitions, in.RIR)

	if len(issues) == 0 {
		return nil
	}
	return &ValidationError{Issues: issues}
}

// appendValueIssues holds the three measured values to the domain bounds. It is
// shared by the original write and by a later correction, so a set can never be
// edited out of a range it could not have been created in.
func appendValueIssues(issues []Issue, weightKg float64, repetitions, rir int) []Issue {
	if math.IsNaN(weightKg) || math.IsInf(weightKg, 0) || weightKg < MinWeightKg || weightKg > MaxWeightKg {
		issues = append(issues, Issue{"weightKg", fmt.Sprintf("weight must be between %.0f and %.0f kg", MinWeightKg, MaxWeightKg)})
	}
	if repetitions < MinRepetitions || repetitions > MaxRepetitions {
		issues = append(issues, Issue{"repetitions", fmt.Sprintf("repetitions must be between %d and %d", MinRepetitions, MaxRepetitions)})
	}
	if rir < MinRIR || rir > MaxRIR {
		issues = append(issues, Issue{"rir", fmt.Sprintf("RIR must be between %d and %d", MinRIR, MaxRIR)})
	}
	return issues
}

func appendIdentifierIssues(issues []Issue, field, value string) []Issue {
	switch {
	case strings.TrimSpace(value) == "":
		return append(issues, Issue{field, "required identifier missing"})
	case len(value) > MaxIdentifierLen:
		return append(issues, Issue{field, fmt.Sprintf("must be at most %d characters", MaxIdentifierLen)})
	}
	return issues
}
