package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

// base is a fixed instant every time-sensitive test hangs off, so the two
// implementations are compared on identical inputs. It is a Wednesday, which
// makes the ISO week boundary (Monday) visible rather than accidental.
var base = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)

func mustWorkoutAt(t *testing.T, st store.Store, userID, status string, createdAt time.Time) store.Workout {
	t.Helper()
	workout := store.Workout{
		ID:        ids.NewUUID(),
		UserID:    userID,
		Title:     "Session",
		Status:    status,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	if status == store.StatusCompleted || status == store.StatusCancelled {
		ended := createdAt
		workout.EndedAt = &ended
	}
	out, err := st.CreateWorkout(context.Background(), workout)
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}
	return out
}

func mustSetAt(t *testing.T, st store.Store, userID, workoutID, exerciseID string, weight float64, reps int, mutation string, at time.Time) {
	t.Helper()
	_, created, err := st.InsertWorkoutSet(context.Background(), store.WorkoutSet{
		ID:               ids.NewUUID(),
		UserID:           userID,
		WorkoutID:        workoutID,
		ExerciseID:       exerciseID,
		SetNumber:        1,
		WeightKg:         weight,
		Repetitions:      reps,
		RIR:              2,
		ClientMutationID: mutation,
		CreatedAt:        at,
	})
	if err != nil || !created {
		t.Fatalf("insert set %s: created=%v err=%v", mutation, created, err)
	}
}

// testRefreshTokenSweep covers the housekeeping behind POST /auth/logout: rows
// that expired or were revoked long enough ago must actually disappear, and
// live rows must survive.
func testRefreshTokenSweep(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")

	live := store.RefreshToken{ID: ids.NewUUID(), UserID: user.ID, TokenHash: "live", IssuedAt: base, ExpiresAt: base.Add(time.Hour)}
	expired := store.RefreshToken{ID: ids.NewUUID(), UserID: user.ID, TokenHash: "expired", IssuedAt: base.Add(-48 * time.Hour), ExpiresAt: base.Add(-24 * time.Hour)}
	revokedAt := base.Add(-24 * time.Hour)
	revoked := store.RefreshToken{ID: ids.NewUUID(), UserID: user.ID, TokenHash: "revoked", IssuedAt: base.Add(-48 * time.Hour), ExpiresAt: base.Add(time.Hour), RevokedAt: &revokedAt}

	for _, token := range []store.RefreshToken{live, expired, revoked} {
		if err := st.CreateRefreshToken(ctx, token); err != nil {
			t.Fatalf("create %s: %v", token.TokenHash, err)
		}
	}

	deleted, err := st.DeleteExpiredRefreshTokens(ctx, base)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("sweep deleted %d rows, want 2 (the expired and the revoked one)", deleted)
	}
	if _, err := st.RefreshTokenByHash(ctx, "live"); err != nil {
		t.Fatalf("a live token must survive the sweep: %v", err)
	}
	for _, hash := range []string{"expired", "revoked"} {
		if _, err := st.RefreshTokenByHash(ctx, hash); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("%s token = %v, want ErrNotFound after the sweep", hash, err)
		}
	}
}

// testStatusTransitions pins the atomic conditional update: the allowed-from
// set decides, a wrong current status is a conflict and never a silent write,
// and another user's workout is a plain not-found.
func testStatusTransitions(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	owner := mustUser(t, st, "owner@example.com")
	intruder := mustUser(t, st, "intruder@example.com")

	workout := mustWorkoutAt(t, st, owner.ID, store.StatusActive, base)
	paused, err := st.SetWorkoutStatus(ctx, owner.ID, workout.ID, []string{store.StatusActive}, store.StatusPaused, base.Add(time.Minute))
	if err != nil {
		t.Fatalf("active -> paused: %v", err)
	}
	if paused.Status != store.StatusPaused || paused.EndedAt != nil {
		t.Fatalf("paused = %+v, want status paused and no endedAt", paused)
	}
	if !paused.UpdatedAt.After(paused.CreatedAt) {
		t.Fatalf("updatedAt %s must move forward from createdAt %s", paused.UpdatedAt, paused.CreatedAt)
	}

	completed, err := st.SetWorkoutStatus(ctx, owner.ID, workout.ID, []string{store.StatusActive, store.StatusPaused}, store.StatusCompleted, base.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("paused -> completed: %v", err)
	}
	if completed.EndedAt == nil {
		t.Fatal("a completed workout must carry endedAt")
	}

	// A terminal workout is not in the allowed-from set of any transition.
	if _, err := st.SetWorkoutStatus(ctx, owner.ID, workout.ID, []string{store.StatusActive, store.StatusPaused}, store.StatusCancelled, base.Add(3*time.Minute)); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("cancelling a completed workout = %v, want ErrConflict", err)
	}
	after, err := st.WorkoutForUser(ctx, owner.ID, workout.ID)
	if err != nil || after.Status != store.StatusCompleted {
		t.Fatalf("a rejected transition must not write: %+v %v", after, err)
	}

	// A foreign workout is not a conflict, it does not exist.
	if _, err := st.SetWorkoutStatus(ctx, intruder.ID, workout.ID, []string{store.StatusCompleted}, store.StatusCancelled, base); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign transition = %v, want ErrNotFound", err)
	}
	if _, err := st.SetWorkoutStatus(ctx, owner.ID, ids.NewUUID(), []string{store.StatusActive}, store.StatusCancelled, base); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing workout = %v, want ErrNotFound", err)
	}
}

// testWorkoutListing pins ordering, filtering, the keyset cursor at a page
// boundary and — most importantly — that another user's workouts are simply
// not part of the result set.
func testWorkoutListing(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	owner := mustUser(t, st, "owner@example.com")
	other := mustUser(t, st, "other@example.com")

	// Five workouts, one hour apart, newest last created.
	created := make([]store.Workout, 0, 5)
	for i := range 5 {
		status := store.StatusActive
		if i%2 == 0 {
			status = store.StatusCompleted
		}
		created = append(created, mustWorkoutAt(t, st, owner.ID, status, base.Add(time.Duration(i)*time.Hour)))
	}
	mustWorkoutAt(t, st, other.ID, store.StatusActive, base.Add(90*time.Minute))

	all, err := st.ListWorkouts(ctx, owner.ID, store.WorkoutFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("listed %d workouts, want 5 of the owner and none of anybody else", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].CreatedAt.After(all[i-1].CreatedAt) {
			t.Fatalf("list is not newest-first: %s before %s", all[i-1].CreatedAt, all[i].CreatedAt)
		}
		if all[i].UserID != owner.ID {
			t.Fatalf("row %d belongs to %s, not to the caller", i, all[i].UserID)
		}
	}

	// Page of two, then continue from the cursor: every row exactly once.
	seen := map[string]bool{}
	var cursor *store.WorkoutCursor
	pages := 0
	for {
		page, err := st.ListWorkouts(ctx, owner.ID, store.WorkoutFilter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		if len(page) == 0 {
			break
		}
		for _, workout := range page {
			if seen[workout.ID] {
				t.Fatalf("workout %s was returned twice while paging", workout.ID)
			}
			seen[workout.ID] = true
		}
		last := page[len(page)-1]
		cursor = &store.WorkoutCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		pages++
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 5 {
		t.Fatalf("paging saw %d of 5 workouts", len(seen))
	}

	// A cursor taken from the exact page boundary must not repeat its own row.
	firstPage, err := st.ListWorkouts(ctx, owner.ID, store.WorkoutFilter{Limit: 5})
	if err != nil {
		t.Fatalf("boundary page: %v", err)
	}
	boundary := firstPage[len(firstPage)-1]
	tail, err := st.ListWorkouts(ctx, owner.ID, store.WorkoutFilter{
		Limit:  5,
		Cursor: &store.WorkoutCursor{CreatedAt: boundary.CreatedAt, ID: boundary.ID},
	})
	if err != nil {
		t.Fatalf("boundary continuation: %v", err)
	}
	if len(tail) != 0 {
		t.Fatalf("continuing past the last row returned %d rows, want none", len(tail))
	}

	// Status filter.
	completed, err := st.ListWorkouts(ctx, owner.ID, store.WorkoutFilter{Statuses: []string{store.StatusCompleted}, Limit: 10})
	if err != nil {
		t.Fatalf("status filter: %v", err)
	}
	if len(completed) != 3 {
		t.Fatalf("status filter returned %d rows, want 3", len(completed))
	}
	for _, workout := range completed {
		if workout.Status != store.StatusCompleted {
			t.Fatalf("status filter leaked a %s workout", workout.Status)
		}
	}

	// Date window: [base+1h, base+3h) selects exactly two.
	from := base.Add(time.Hour)
	to := base.Add(3 * time.Hour)
	window, err := st.ListWorkouts(ctx, owner.ID, store.WorkoutFilter{From: &from, To: &to, Limit: 10})
	if err != nil {
		t.Fatalf("date filter: %v", err)
	}
	if len(window) != 2 {
		t.Fatalf("date filter returned %d rows, want 2", len(window))
	}

	// The other user sees only their own single workout, cursor or not.
	theirs, err := st.ListWorkouts(ctx, other.ID, store.WorkoutFilter{Limit: 10})
	if err != nil {
		t.Fatalf("other list: %v", err)
	}
	if len(theirs) != 1 || theirs[0].UserID != other.ID {
		t.Fatalf("other user sees %+v, want exactly their own workout", theirs)
	}
	leaked, err := st.ListWorkouts(ctx, other.ID, store.WorkoutFilter{
		Limit:  10,
		Cursor: &store.WorkoutCursor{CreatedAt: created[4].CreatedAt, ID: created[4].ID},
	})
	if err != nil {
		t.Fatalf("other list with a foreign cursor: %v", err)
	}
	for _, workout := range leaked {
		if workout.UserID != other.ID {
			t.Fatal("a cursor copied from another account must not widen the scope")
		}
	}
}
