package storetest

import (
	"context"
	"math"
	"testing"
	"time"

	"athletica.ai/api/internal/store"
)

// testProgressAggregates pins the numbers the "Прогресс" screen shows, on both
// implementations: the strength record per exercise, the weekly volume, the
// weekly adherence — and the two exclusions that make them honest, namely
// another user's sets and the sets of a cancelled workout.
func testProgressAggregates(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	owner := mustUser(t, st, "owner@example.com")
	other := mustUser(t, st, "other@example.com")

	week1 := base                  // Wed 2026-03-04, week of Mon 2026-03-02
	week2 := base.AddDate(0, 0, 7) // Wed 2026-03-11, week of Mon 2026-03-09
	window := store.ProgressWindow{From: store.WeekStart(week1), To: store.WeekStart(week2).AddDate(0, 0, 7)}

	completed := mustWorkoutAt(t, st, owner.ID, store.StatusCompleted, week1)
	mustSetAt(t, st, owner.ID, completed.ID, "squat", 100, 5, "m1", week1)
	mustSetAt(t, st, owner.ID, completed.ID, "squat", 90, 10, "m2", week1.Add(time.Minute))
	mustSetAt(t, st, owner.ID, completed.ID, "bench", 60, 8, "m3", week1.Add(2*time.Minute))

	active := mustWorkoutAt(t, st, owner.ID, store.StatusActive, week2)
	mustSetAt(t, st, owner.ID, active.ID, "squat", 95, 5, "m4", week2)

	// A cancelled session must not produce records or volume.
	cancelled := mustWorkoutAt(t, st, owner.ID, store.StatusCancelled, week2.Add(time.Hour))
	mustSetAt(t, st, owner.ID, cancelled.ID, "squat", 300, 10, "m5", week2.Add(time.Hour))

	// Another athlete's heavier session must be invisible here.
	otherWorkout := mustWorkoutAt(t, st, other.ID, store.StatusCompleted, week1)
	mustSetAt(t, st, other.ID, otherWorkout.ID, "squat", 250, 5, "m6", week1)

	records, err := st.ExerciseRecords(ctx, owner.ID, window, 10)
	if err != nil {
		t.Fatalf("exercise records: %v", err)
	}
	byExercise := map[string]store.ExerciseRecord{}
	for _, record := range records {
		byExercise[record.ExerciseID] = record
	}
	if len(byExercise) != 2 {
		t.Fatalf("records cover %d exercises, want squat and bench only: %+v", len(byExercise), records)
	}

	squat := byExercise["squat"]
	if squat.Sets != 3 || squat.Repetitions != 20 {
		t.Fatalf("squat = %d sets / %d reps, want 3 / 20 (cancelled and foreign sets excluded)", squat.Sets, squat.Repetitions)
	}
	if !closeTo(squat.VolumeKg, 100*5+90*10+95*5) {
		t.Fatalf("squat volume = %v, want %v", squat.VolumeKg, 100*5+90*10+95*5)
	}
	if squat.BestWeightKg != 100 || squat.BestWeightReps != 5 {
		t.Fatalf("heaviest squat = %v kg x %d, want 100 x 5", squat.BestWeightKg, squat.BestWeightReps)
	}
	// Epley: 90 kg x 10 = 120 estimated, above 100 kg x 5 = 116.67.
	if !closeTo(squat.BestEstimated1RM, store.Estimated1RM(90, 10)) {
		t.Fatalf("best estimated 1RM = %v, want %v (90 kg x 10)", squat.BestEstimated1RM, store.Estimated1RM(90, 10))
	}
	if squat.Best1RMWeightKg != 90 || squat.Best1RMReps != 10 {
		t.Fatalf("best 1RM set = %v x %d, want 90 x 10", squat.Best1RMWeightKg, squat.Best1RMReps)
	}
	if !squat.LastPerformedAt.Equal(week2.UTC()) {
		t.Fatalf("last squat = %s, want %s", squat.LastPerformedAt, week2)
	}
	if records[0].ExerciseID != "squat" {
		t.Fatalf("records are ordered %s first, want the strongest lift first", records[0].ExerciseID)
	}

	volume, err := st.WeeklyVolume(ctx, owner.ID, window)
	if err != nil {
		t.Fatalf("weekly volume: %v", err)
	}
	if len(volume) != 2 {
		t.Fatalf("volume covers %d weeks, want 2: %+v", len(volume), volume)
	}
	if !volume[0].WeekStart.Equal(store.WeekStart(week1)) || !volume[1].WeekStart.Equal(store.WeekStart(week2)) {
		t.Fatalf("weeks start at %s and %s, want Mondays %s and %s",
			volume[0].WeekStart, volume[1].WeekStart, store.WeekStart(week1), store.WeekStart(week2))
	}
	if volume[0].Sets != 3 || volume[0].Workouts != 1 {
		t.Fatalf("week 1 = %d sets in %d workouts, want 3 in 1", volume[0].Sets, volume[0].Workouts)
	}
	if !closeTo(volume[0].VolumeKg, 100*5+90*10+60*8) {
		t.Fatalf("week 1 volume = %v, want %v", volume[0].VolumeKg, 100*5+90*10+60*8)
	}
	if volume[1].Sets != 1 || volume[1].Workouts != 1 {
		t.Fatalf("week 2 = %d sets in %d workouts, want 1 in 1 (the cancelled workout is excluded)", volume[1].Sets, volume[1].Workouts)
	}

	adherence, err := st.WeeklyAdherence(ctx, owner.ID, window)
	if err != nil {
		t.Fatalf("weekly adherence: %v", err)
	}
	if len(adherence) != 2 {
		t.Fatalf("adherence covers %d weeks, want 2: %+v", len(adherence), adherence)
	}
	if adherence[0].Started != 1 || adherence[0].Completed != 1 {
		t.Fatalf("week 1 adherence = %+v, want 1 started and 1 completed", adherence[0])
	}
	if adherence[1].Started != 2 || adherence[1].Cancelled != 1 || adherence[1].InProgress != 1 {
		t.Fatalf("week 2 adherence = %+v, want 2 started, 1 cancelled, 1 in progress", adherence[1])
	}

	// The other athlete's report contains only their own numbers.
	theirs, err := st.ExerciseRecords(ctx, other.ID, window, 10)
	if err != nil {
		t.Fatalf("other records: %v", err)
	}
	if len(theirs) != 1 || theirs[0].BestWeightKg != 250 {
		t.Fatalf("other athlete sees %+v, want only their own 250 kg squat", theirs)
	}

	// An empty window is an empty report, never an error.
	empty := store.ProgressWindow{From: window.To, To: window.To.AddDate(0, 0, 7)}
	records, err = st.ExerciseRecords(ctx, owner.ID, empty, 10)
	if err != nil || len(records) != 0 {
		t.Fatalf("empty window = (%d records, %v), want (0, nil)", len(records), err)
	}
}

func closeTo(got, want float64) bool { return math.Abs(got-want) < 0.01 }
