package workouts_test

import (
	"math"
	"strings"
	"testing"

	"athletica.ai/api/internal/workouts"
)

func validInput() workouts.SetInput {
	return workouts.SetInput{
		WorkoutID:        "0f6c1c2a-1111-4111-8111-111111111111",
		ExerciseID:       "lat-pulldown",
		SetNumber:        2,
		WeightKg:         62.5,
		Repetitions:      10,
		RIR:              2,
		ClientMutationID: "mutation-1",
	}
}

// The bounds mirror validateSet() in packages/domain/src/workout.ts.
func TestValidateSetBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*workouts.SetInput)
		wantErr bool
		field   string
	}{
		{"valid", func(*workouts.SetInput) {}, false, ""},
		{"set number 1 is allowed", func(in *workouts.SetInput) { in.SetNumber = 1 }, false, ""},
		{"set number 0 is rejected", func(in *workouts.SetInput) { in.SetNumber = 0 }, true, "setNumber"},
		{"negative set number is rejected", func(in *workouts.SetInput) { in.SetNumber = -3 }, true, "setNumber"},
		{"weight 0 is allowed", func(in *workouts.SetInput) { in.WeightKg = 0 }, false, ""},
		{"weight 1000 is allowed", func(in *workouts.SetInput) { in.WeightKg = 1000 }, false, ""},
		{"weight above 1000 is rejected", func(in *workouts.SetInput) { in.WeightKg = 1000.01 }, true, "weightKg"},
		{"negative weight is rejected", func(in *workouts.SetInput) { in.WeightKg = -0.5 }, true, "weightKg"},
		{"NaN weight is rejected", func(in *workouts.SetInput) { in.WeightKg = math.NaN() }, true, "weightKg"},
		{"Inf weight is rejected", func(in *workouts.SetInput) { in.WeightKg = math.Inf(1) }, true, "weightKg"},
		{"1 repetition is allowed", func(in *workouts.SetInput) { in.Repetitions = 1 }, false, ""},
		{"100 repetitions are allowed", func(in *workouts.SetInput) { in.Repetitions = 100 }, false, ""},
		{"0 repetitions are rejected", func(in *workouts.SetInput) { in.Repetitions = 0 }, true, "repetitions"},
		{"101 repetitions are rejected", func(in *workouts.SetInput) { in.Repetitions = 101 }, true, "repetitions"},
		{"RIR 0 is allowed", func(in *workouts.SetInput) { in.RIR = 0 }, false, ""},
		{"RIR 10 is allowed", func(in *workouts.SetInput) { in.RIR = 10 }, false, ""},
		{"RIR -1 is rejected", func(in *workouts.SetInput) { in.RIR = -1 }, true, "rir"},
		{"RIR 11 is rejected", func(in *workouts.SetInput) { in.RIR = 11 }, true, "rir"},
		{"missing exercise is rejected", func(in *workouts.SetInput) { in.ExerciseID = "  " }, true, "exerciseId"},
		{"missing mutation id is rejected", func(in *workouts.SetInput) { in.ClientMutationID = "" }, true, "clientMutationId"},
		{"missing workout id is rejected", func(in *workouts.SetInput) { in.WorkoutID = "" }, true, "workoutId"},
		{"overlong mutation id is rejected", func(in *workouts.SetInput) { in.ClientMutationID = strings.Repeat("x", 129) }, true, "clientMutationId"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validInput()
			tc.mutate(&in)

			err := workouts.ValidateSet(in)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("unexpected validation error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !hasField(err.Issues, tc.field) {
				t.Fatalf("issues %+v do not mention field %q", err.Issues, tc.field)
			}
		})
	}
}

func TestValidateSetReportsEveryProblemAtOnce(t *testing.T) {
	err := workouts.ValidateSet(workouts.SetInput{})
	if err == nil {
		t.Fatal("an empty input must fail")
	}
	// weightKg=0 and rir=0 are legal zero values, so the empty input fails on
	// the three identifiers plus setNumber and repetitions.
	for _, field := range []string{"workoutId", "exerciseId", "clientMutationId", "setNumber", "repetitions"} {
		if !hasField(err.Issues, field) {
			t.Fatalf("issues %+v do not mention field %q", err.Issues, field)
		}
	}
}

func hasField(issues []workouts.Issue, field string) bool {
	for _, issue := range issues {
		if issue.Field == field {
			return true
		}
	}
	return false
}
