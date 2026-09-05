package storetest

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

// Conformance for correcting and removing a set, and for naming a workout
// afterwards. Both implementations run exactly these cases, so the in-memory
// store cannot quietly disagree with the SQL one about what a replay means.

// testSetCorrectionIsIdempotent is the central claim of the feature: an edit
// arriving twice out of the offline outbox is applied once.
func testSetCorrectionIsIdempotent(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")
	workout := mustWorkout(t, st, user.ID)

	original, _, err := st.InsertWorkoutSet(ctx, sampleSet(user.ID, workout.ID, "create-1"))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	edit := store.SetUpdate{
		UserID: user.ID, WorkoutID: workout.ID, SetID: original.ID,
		WeightKg: 70, Repetitions: 8, RIR: 1,
		ClientMutationID: "edit-1", At: base.Add(time.Minute),
	}
	first, applied, err := st.UpdateWorkoutSet(ctx, edit)
	if err != nil || !applied {
		t.Fatalf("first edit = (%+v, %v, %v), want applied", first, applied, err)
	}
	if first.WeightKg != 70 || first.Repetitions != 8 || first.RIR != 1 {
		t.Fatalf("edit stored %+v, want 70kg × 8 @ RIR 1", first)
	}
	if !first.UpdatedAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("updatedAt = %s, want the moment the correction was applied (%s)",
			first.UpdatedAt, base.Add(time.Minute))
	}
	// The set keeps its identity: the same row, the same number, the same
	// creation mutation ID the outbox derived from workoutId:exerciseId:number.
	if first.ID != original.ID || first.SetNumber != original.SetNumber || first.ClientMutationID != original.ClientMutationID {
		t.Fatalf("an edit must not re-identify the set: %+v vs %+v", first, original)
	}

	// The replay carries different values on purpose: applying it a second time
	// would be visible, and must not happen.
	replay := edit
	replay.WeightKg = 999
	replay.Repetitions = 1
	second, applied, err := st.UpdateWorkoutSet(ctx, replay)
	if err != nil {
		t.Fatalf("replayed edit: %v", err)
	}
	if applied {
		t.Fatal("a replayed clientMutationId must not apply the edit a second time")
	}
	if second.WeightKg != 70 || second.Repetitions != 8 {
		t.Fatalf("the replay changed the row to %+v, want the first edit's 70kg × 8", second)
	}
	if !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("the replay moved updatedAt from %s to %s", first.UpdatedAt, second.UpdatedAt)
	}

	// A *fresh* mutation ID is a genuinely new correction and does apply.
	edit.ClientMutationID = "edit-2"
	edit.WeightKg = 72.5
	edit.At = base.Add(2 * time.Minute)
	third, applied, err := st.UpdateWorkoutSet(ctx, edit)
	if err != nil || !applied {
		t.Fatalf("second correction = (%+v, %v, %v), want applied", third, applied, err)
	}
	if third.WeightKg != 72.5 {
		t.Fatalf("second correction stored %v kg, want 72.5", third.WeightKg)
	}
}

// testConcurrentCorrectionAppliesOnce is the edit's twin of the concurrent
// replay test on the set write: sixteen retries of one outbox entry apply once.
func testConcurrentCorrectionAppliesOnce(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")
	workout := mustWorkout(t, st, user.ID)
	original, _, err := st.InsertWorkoutSet(ctx, sampleSet(user.ID, workout.ID, "create-1"))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	const attempts = 16
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		applied int
		failed  error
	)
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := st.UpdateWorkoutSet(ctx, store.SetUpdate{
				UserID: user.ID, WorkoutID: workout.ID, SetID: original.ID,
				WeightKg: 80, Repetitions: 5, RIR: 0,
				ClientMutationID: "racy-edit", At: base.Add(time.Minute),
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = err
				return
			}
			if ok {
				applied++
			}
		}()
	}
	wg.Wait()

	if failed != nil {
		t.Fatalf("concurrent edit failed: %v", failed)
	}
	if applied != 1 {
		t.Fatalf("%d of %d concurrent replays applied the edit, want exactly 1", applied, attempts)
	}
}

// testSetDeletionIsSoftAndRepeatable pins the deletion contract: the set stops
// being visible, the row keeps its idempotency slot, and repeating the deletion
// is never an error.
func testSetDeletionIsSoftAndRepeatable(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")
	workout := mustWorkout(t, st, user.ID)
	original, _, err := st.InsertWorkoutSet(ctx, sampleSet(user.ID, workout.ID, "create-1"))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	deletion := store.SetDeletion{
		UserID: user.ID, WorkoutID: workout.ID, SetID: original.ID,
		ClientMutationID: "delete-1", At: base.Add(time.Minute),
	}
	deleted, applied, err := st.DeleteWorkoutSet(ctx, deletion)
	if err != nil || !applied {
		t.Fatalf("first deletion = (%+v, %v, %v), want applied", deleted, applied, err)
	}
	if deleted.Live() {
		t.Fatal("a deleted set must carry deletedAt")
	}

	// The same mutation ID again.
	repeat, applied, err := st.DeleteWorkoutSet(ctx, deletion)
	if err != nil {
		t.Fatalf("replayed deletion must be safe, got %v", err)
	}
	if applied {
		t.Fatal("a replayed deletion must not apply twice")
	}
	if repeat.DeletedAt == nil || !repeat.DeletedAt.Equal(*deleted.DeletedAt) {
		t.Fatalf("the replay moved deletedAt from %v to %v", deleted.DeletedAt, repeat.DeletedAt)
	}

	// A *different* mutation ID deleting an already-deleted set is equally safe:
	// the state it asks for is the state that already holds.
	deletion.ClientMutationID = "delete-2"
	deletion.At = base.Add(2 * time.Minute)
	again, applied, err := st.DeleteWorkoutSet(ctx, deletion)
	if err != nil {
		t.Fatalf("second deletion with a fresh mutation id must be safe, got %v", err)
	}
	if applied {
		t.Fatal("deleting an already-deleted set must not write again")
	}
	if !again.DeletedAt.Equal(*deleted.DeletedAt) {
		t.Fatalf("deletedAt moved to %v", again.DeletedAt)
	}

	// It is gone from the workout detail...
	sets, err := st.ListWorkoutSets(ctx, user.ID, workout.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sets) != 0 {
		t.Fatalf("a deleted set is still listed: %+v", sets)
	}

	// ...but its row still holds the creation mutation ID, so a replay of the
	// *creation* out of the outbox cannot resurrect it as a second set.
	resurrect := sampleSet(user.ID, workout.ID, "create-1")
	stored, created, err := st.InsertWorkoutSet(ctx, resurrect)
	if err != nil {
		t.Fatalf("replayed creation: %v", err)
	}
	if created {
		t.Fatal("replaying the creation of a deleted set must not insert a second row")
	}
	if stored.Live() {
		t.Fatal("the replayed creation must report the set as deleted, not as live")
	}
}

// testDeletedSetLeavesAggregates is the reason deletion exists: the "Прогресс"
// screen must stop counting a set the athlete removed.
func testDeletedSetLeavesAggregates(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")
	workout := mustWorkoutAt(t, st, user.ID, store.StatusCompleted, base)

	// A mistyped 300 kg squat next to an honest 100 kg one.
	mustSetAt(t, st, user.ID, workout.ID, "squat", 100, 5, "keep", base)
	wrong, _, err := st.InsertWorkoutSet(ctx, store.WorkoutSet{
		ID: ids.NewUUID(), UserID: user.ID, WorkoutID: workout.ID,
		ExerciseID: "squat", SetNumber: 2, WeightKg: 300, Repetitions: 5, RIR: 2,
		ClientMutationID: "typo", CreatedAt: base.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("insert typo: %v", err)
	}

	window := store.ProgressWindow{From: base.AddDate(0, 0, -7), To: base.AddDate(0, 0, 7)}
	before, err := st.ExerciseRecords(ctx, user.ID, window, 10)
	if err != nil || len(before) != 1 {
		t.Fatalf("records before = (%+v, %v)", before, err)
	}
	if before[0].BestWeightKg != 300 || before[0].Sets != 2 {
		t.Fatalf("before deletion the typo must be in the record: %+v", before[0])
	}

	if _, applied, err := st.DeleteWorkoutSet(ctx, store.SetDeletion{
		UserID: user.ID, WorkoutID: workout.ID, SetID: wrong.ID,
		ClientMutationID: "delete-typo", At: base.Add(2 * time.Minute),
	}); err != nil || !applied {
		t.Fatalf("delete typo = (%v, %v)", applied, err)
	}

	after, err := st.ExerciseRecords(ctx, user.ID, window, 10)
	if err != nil || len(after) != 1 {
		t.Fatalf("records after = (%+v, %v)", after, err)
	}
	if after[0].BestWeightKg != 100 {
		t.Fatalf("the deleted set still holds the record at %v kg", after[0].BestWeightKg)
	}
	if after[0].Sets != 1 || after[0].Repetitions != 5 {
		t.Fatalf("the deleted set still counts: %+v", after[0])
	}
	if after[0].VolumeKg != 500 {
		t.Fatalf("volume = %v, want 500 (only the surviving set)", after[0].VolumeKg)
	}

	volume, err := st.WeeklyVolume(ctx, user.ID, window)
	if err != nil {
		t.Fatalf("weekly volume: %v", err)
	}
	for _, week := range volume {
		if week.Sets != 1 || week.VolumeKg != 500 {
			t.Fatalf("weekly volume still counts the deleted set: %+v", week)
		}
	}
}

// testCorrectedSetIsReflectedInAggregates is the other half of the story: after
// a correction the record follows the corrected value, not the mistyped one.
func testCorrectedSetIsReflectedInAggregates(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")
	workout := mustWorkoutAt(t, st, user.ID, store.StatusCompleted, base)

	wrong, _, err := st.InsertWorkoutSet(ctx, store.WorkoutSet{
		ID: ids.NewUUID(), UserID: user.ID, WorkoutID: workout.ID,
		ExerciseID: "deadlift", SetNumber: 1, WeightKg: 1000, Repetitions: 5, RIR: 2,
		ClientMutationID: "typo", CreatedAt: base,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	if _, applied, err := st.UpdateWorkoutSet(ctx, store.SetUpdate{
		UserID: user.ID, WorkoutID: workout.ID, SetID: wrong.ID,
		WeightKg: 100, Repetitions: 5, RIR: 2,
		ClientMutationID: "fix-typo", At: base.Add(time.Minute),
	}); err != nil || !applied {
		t.Fatalf("correction = (%v, %v)", applied, err)
	}

	window := store.ProgressWindow{From: base.AddDate(0, 0, -7), To: base.AddDate(0, 0, 7)}
	records, err := st.ExerciseRecords(ctx, user.ID, window, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("records = (%+v, %v)", records, err)
	}
	if records[0].BestWeightKg != 100 || records[0].VolumeKg != 500 {
		t.Fatalf("aggregates still carry the mistyped value: %+v", records[0])
	}
	want := store.Estimated1RM(100, 5)
	if records[0].BestEstimated1RM != want {
		t.Fatalf("estimated 1RM = %v, want %v", records[0].BestEstimated1RM, want)
	}
}

// testCorrectionsAreUserScoped is the ownership half: another user's set can be
// neither corrected nor deleted, and the answer is the plain not-found that a
// missing set gets.
func testCorrectionsAreUserScoped(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	owner := mustUser(t, st, "owner@example.com")
	intruder := mustUser(t, st, "intruder@example.com")
	intruderWorkout := mustWorkout(t, st, intruder.ID)
	workout := mustWorkout(t, st, owner.ID)
	set, _, err := st.InsertWorkoutSet(ctx, sampleSet(owner.ID, workout.ID, "create-1"))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	missingSet := ids.NewUUID()
	missingWorkout := ids.NewUUID()

	cases := []struct {
		name      string
		userID    string
		workoutID string
		setID     string
	}{
		{"foreign set through its real workout", intruder.ID, workout.ID, set.ID},
		{"foreign set through the intruder's own workout", intruder.ID, intruderWorkout.ID, set.ID},
		{"missing set", owner.ID, workout.ID, missingSet},
		{"missing workout", owner.ID, missingWorkout, set.ID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := st.UpdateWorkoutSet(ctx, store.SetUpdate{
				UserID: tc.userID, WorkoutID: tc.workoutID, SetID: tc.setID,
				WeightKg: 5, Repetitions: 1, RIR: 0,
				ClientMutationID: "intrusion-edit-" + tc.name, At: base,
			})
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("update = %v, want ErrNotFound so existence never leaks", err)
			}
			_, _, err = st.DeleteWorkoutSet(ctx, store.SetDeletion{
				UserID: tc.userID, WorkoutID: tc.workoutID, SetID: tc.setID,
				ClientMutationID: "intrusion-delete-" + tc.name, At: base,
			})
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("delete = %v, want ErrNotFound so existence never leaks", err)
			}
		})
	}

	// Nothing was written by any of it.
	live, err := st.ListWorkoutSets(ctx, owner.ID, workout.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(live) != 1 || live[0].WeightKg != set.WeightKg || !live[0].Live() {
		t.Fatalf("the owner's set was touched: %+v", live)
	}
}

// testMutationIdCannotBeRecycled: the same clientMutationId must not be able to
// aim a second, different change at another row.
func testMutationIdCannotBeRecycled(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")
	workout := mustWorkout(t, st, user.ID)

	first, _, err := st.InsertWorkoutSet(ctx, sampleSet(user.ID, workout.ID, "create-1"))
	if err != nil {
		t.Fatalf("insert first: %v", err)
	}
	second := sampleSet(user.ID, workout.ID, "create-2")
	second.SetNumber = 3
	stored, _, err := st.InsertWorkoutSet(ctx, second)
	if err != nil {
		t.Fatalf("insert second: %v", err)
	}

	if _, applied, err := st.UpdateWorkoutSet(ctx, store.SetUpdate{
		UserID: user.ID, WorkoutID: workout.ID, SetID: first.ID,
		WeightKg: 70, Repetitions: 8, RIR: 1, ClientMutationID: "edit-1", At: base,
	}); err != nil || !applied {
		t.Fatalf("first edit = (%v, %v)", applied, err)
	}

	// Same ID, different set.
	_, _, err = st.UpdateWorkoutSet(ctx, store.SetUpdate{
		UserID: user.ID, WorkoutID: workout.ID, SetID: stored.ID,
		WeightKg: 70, Repetitions: 8, RIR: 1, ClientMutationID: "edit-1", At: base,
	})
	if !errors.Is(err, store.ErrMutationReused) {
		t.Fatalf("recycled mutation id = %v, want ErrMutationReused", err)
	}

	// Same ID, same set, different *kind* of change.
	_, _, err = st.DeleteWorkoutSet(ctx, store.SetDeletion{
		UserID: user.ID, WorkoutID: workout.ID, SetID: first.ID,
		ClientMutationID: "edit-1", At: base,
	})
	if !errors.Is(err, store.ErrMutationReused) {
		t.Fatalf("mutation id reused for a deletion = %v, want ErrMutationReused", err)
	}

	// The very same ID is a different row for a different user (the unique key
	// is (user_id, client_mutation_id), not the ID alone).
	other := mustUser(t, st, "other@example.com")
	otherWorkout := mustWorkout(t, st, other.ID)
	otherSet, _, err := st.InsertWorkoutSet(ctx, sampleSet(other.ID, otherWorkout.ID, "create-1"))
	if err != nil {
		t.Fatalf("insert for other user: %v", err)
	}
	if _, applied, err := st.UpdateWorkoutSet(ctx, store.SetUpdate{
		UserID: other.ID, WorkoutID: otherWorkout.ID, SetID: otherSet.ID,
		WeightKg: 70, Repetitions: 8, RIR: 1, ClientMutationID: "edit-1", At: base,
	}); err != nil || !applied {
		t.Fatalf("another user's identical mutation id = (%v, %v), want an independent apply", applied, err)
	}
}

// testCorrectionStateRules: an edit inside a cancelled session is refused, an
// edit of an already-deleted set is refused, and — the decision the feature
// turns on — an edit inside a *completed* workout is allowed.
func testCorrectionStateRules(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")

	// Completed: allowed. The athlete notices the typo on the "Итоги" screen,
	// which only exists once the session is finished.
	completed := mustWorkoutAt(t, st, user.ID, store.StatusCompleted, base)
	set, _, err := st.InsertWorkoutSet(ctx, sampleSet(user.ID, completed.ID, "create-1"))
	if err != nil {
		t.Fatalf("insert into completed workout: %v", err)
	}
	if _, applied, err := st.UpdateWorkoutSet(ctx, store.SetUpdate{
		UserID: user.ID, WorkoutID: completed.ID, SetID: set.ID,
		WeightKg: 60, Repetitions: 12, RIR: 3, ClientMutationID: "edit-completed", At: base.Add(time.Hour),
	}); err != nil || !applied {
		t.Fatalf("editing a completed workout = (%v, %v), want it allowed", applied, err)
	}

	// Deleted: refused with ErrGone, which is the caller's own row and so may
	// be named — unlike a foreign one.
	if _, applied, err := st.DeleteWorkoutSet(ctx, store.SetDeletion{
		UserID: user.ID, WorkoutID: completed.ID, SetID: set.ID,
		ClientMutationID: "delete-completed", At: base.Add(2 * time.Hour),
	}); err != nil || !applied {
		t.Fatalf("delete = (%v, %v)", applied, err)
	}
	if _, _, err := st.UpdateWorkoutSet(ctx, store.SetUpdate{
		UserID: user.ID, WorkoutID: completed.ID, SetID: set.ID,
		WeightKg: 61, Repetitions: 12, RIR: 3, ClientMutationID: "edit-after-delete", At: base.Add(3 * time.Hour),
	}); !errors.Is(err, store.ErrGone) {
		t.Fatalf("editing a deleted set = %v, want ErrGone", err)
	}

	// Cancelled: refused. The session was thrown away; its sets count towards
	// nothing already, and rewriting them only adds noise to a discarded log.
	cancelled := mustWorkoutAt(t, st, user.ID, store.StatusActive, base)
	live, _, err := st.InsertWorkoutSet(ctx, sampleSet(user.ID, cancelled.ID, "create-2"))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := st.SetWorkoutStatus(ctx, user.ID, cancelled.ID,
		[]string{store.StatusActive, store.StatusPaused}, store.StatusCancelled, base.Add(time.Hour)); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, _, err := st.UpdateWorkoutSet(ctx, store.SetUpdate{
		UserID: user.ID, WorkoutID: cancelled.ID, SetID: live.ID,
		WeightKg: 50, Repetitions: 6, RIR: 2, ClientMutationID: "edit-cancelled", At: base.Add(2 * time.Hour),
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("editing a cancelled workout = %v, want ErrConflict", err)
	}
	if _, _, err := st.DeleteWorkoutSet(ctx, store.SetDeletion{
		UserID: user.ID, WorkoutID: cancelled.ID, SetID: live.ID,
		ClientMutationID: "delete-cancelled", At: base.Add(2 * time.Hour),
	}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("deleting inside a cancelled workout = %v, want ErrConflict", err)
	}
}

// testSetNumbersSurviveADeletion: removing the set in the middle must leave the
// numbers of the others alone, or the client's deterministic
// `workoutId:exerciseId:setNumber` mutation ID would start naming another set.
func testSetNumbersSurviveADeletion(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")
	workout := mustWorkout(t, st, user.ID)

	setIDs := make([]string, 0, 3)
	for number := 1; number <= 3; number++ {
		set := sampleSet(user.ID, workout.ID, "bench:"+strconv.Itoa(number))
		set.SetNumber = number
		stored, _, err := st.InsertWorkoutSet(ctx, set)
		if err != nil {
			t.Fatalf("insert set %d: %v", number, err)
		}
		setIDs = append(setIDs, stored.ID)
	}

	if _, applied, err := st.DeleteWorkoutSet(ctx, store.SetDeletion{
		UserID: user.ID, WorkoutID: workout.ID, SetID: setIDs[1],
		ClientMutationID: "delete-middle", At: base,
	}); err != nil || !applied {
		t.Fatalf("delete middle = (%v, %v)", applied, err)
	}

	sets, err := st.ListWorkoutSets(ctx, user.ID, workout.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sets) != 2 {
		t.Fatalf("listed %d sets, want 2", len(sets))
	}
	if sets[0].SetNumber != 1 || sets[1].SetNumber != 3 {
		t.Fatalf("numbers shifted to %d and %d, want 1 and 3 — a gap, not a renumbering",
			sets[0].SetNumber, sets[1].SetNumber)
	}
}

// testWorkoutRename covers naming a session that was started offline unnamed,
// including the ledger and the ownership boundary.
func testWorkoutRename(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	owner := mustUser(t, st, "owner@example.com")
	intruder := mustUser(t, st, "intruder@example.com")

	unnamed, _, err := st.CreateWorkout(ctx, store.Workout{
		ID: ids.NewUUID(), UserID: owner.ID, Title: "", Status: store.StatusActive, CreatedAt: base,
	})
	if err != nil {
		t.Fatalf("create unnamed workout: %v", err)
	}

	renamed, applied, err := st.RenameWorkout(ctx, store.WorkoutRename{
		UserID: owner.ID, WorkoutID: unnamed.ID, Title: "Pull day",
		ClientMutationID: "rename-1", At: base.Add(time.Hour),
	})
	if err != nil || !applied {
		t.Fatalf("rename = (%+v, %v, %v), want applied", renamed, applied, err)
	}
	if renamed.Title != "Pull day" {
		t.Fatalf("title = %q, want %q", renamed.Title, "Pull day")
	}
	if !renamed.UpdatedAt.After(renamed.CreatedAt) {
		t.Fatalf("updatedAt %s must move past createdAt %s", renamed.UpdatedAt, renamed.CreatedAt)
	}
	if renamed.Status != store.StatusActive || renamed.EndedAt != nil {
		t.Fatalf("a rename must not touch the lifecycle: %+v", renamed)
	}

	// A replay of the same queued rename does not re-apply.
	replay, applied, err := st.RenameWorkout(ctx, store.WorkoutRename{
		UserID: owner.ID, WorkoutID: unnamed.ID, Title: "Something else",
		ClientMutationID: "rename-1", At: base.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("replayed rename: %v", err)
	}
	if applied || replay.Title != "Pull day" {
		t.Fatalf("the replay changed the title to %q (applied=%v)", replay.Title, applied)
	}

	// An unlabelled rename is last-write-wins, which is what a rename from the
	// UI of a connected client is.
	direct, applied, err := st.RenameWorkout(ctx, store.WorkoutRename{
		UserID: owner.ID, WorkoutID: unnamed.ID, Title: "Leg day", At: base.Add(3 * time.Hour),
	})
	if err != nil || !applied || direct.Title != "Leg day" {
		t.Fatalf("unlabelled rename = (%+v, %v, %v)", direct, applied, err)
	}

	// Somebody else's workout does not exist as far as this call is concerned.
	if _, _, err := st.RenameWorkout(ctx, store.WorkoutRename{
		UserID: intruder.ID, WorkoutID: unnamed.ID, Title: "Mine now",
		ClientMutationID: "rename-2", At: base,
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign rename = %v, want ErrNotFound", err)
	}
	if _, _, err := st.RenameWorkout(ctx, store.WorkoutRename{
		UserID: owner.ID, WorkoutID: ids.NewUUID(), Title: "Ghost",
		ClientMutationID: "rename-3", At: base,
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing rename = %v, want ErrNotFound", err)
	}

	after, err := st.WorkoutForUser(ctx, owner.ID, unnamed.ID)
	if err != nil || after.Title != "Leg day" {
		t.Fatalf("stored workout = (%+v, %v)", after, err)
	}
}
