// Package storetest is a conformance suite every Store implementation must
// pass. The in-memory store runs it on every `go test ./...`; the PostgreSQL
// store runs the very same suite when ATHLETICA_TEST_DATABASE_URL is set, so
// the two implementations cannot drift apart (audit finding H3).
package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

// Factory returns a fresh, empty Store for one subtest.
type Factory func(t *testing.T) store.Store

// Run executes the whole conformance suite against newStore.
func Run(t *testing.T, newStore Factory) {
	t.Helper()

	t.Run("EmailIsUniqueAndCaseInsensitive", func(t *testing.T) { testEmailUnique(t, newStore) })
	t.Run("UserLookups", func(t *testing.T) { testUserLookups(t, newStore) })
	t.Run("RefreshTokenLifecycle", func(t *testing.T) { testRefreshTokens(t, newStore) })
	t.Run("WorkoutOwnershipIsScoped", func(t *testing.T) { testWorkoutOwnership(t, newStore) })
	t.Run("SetWriteIsIdempotent", func(t *testing.T) { testIdempotentSet(t, newStore) })
	t.Run("MutationIdIsScopedToUser", func(t *testing.T) { testMutationScope(t, newStore) })
	t.Run("ForeignWorkoutIsRejected", func(t *testing.T) { testForeignWorkout(t, newStore) })
	t.Run("ListIsUserScoped", func(t *testing.T) { testListScope(t, newStore) })
	t.Run("ConcurrentReplayCreatesOneRow", func(t *testing.T) { testConcurrentReplay(t, newStore) })
}

func mustUser(t *testing.T, st store.Store, email string) store.User {
	t.Helper()
	user, err := st.CreateUser(context.Background(), store.User{
		ID:           ids.NewUUID(),
		Email:        email,
		PasswordHash: "$2a$04$notarealhashbutlongenoughvaluehere.................",
	})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return user
}

func mustWorkout(t *testing.T, st store.Store, userID string) store.Workout {
	t.Helper()
	workout, err := st.CreateWorkout(context.Background(), store.Workout{
		ID:     ids.NewUUID(),
		UserID: userID,
		Title:  "Push day",
		Status: "active",
	})
	if err != nil {
		t.Fatalf("create workout: %v", err)
	}
	return workout
}

func sampleSet(userID, workoutID, mutationID string) store.WorkoutSet {
	return store.WorkoutSet{
		ID:               ids.NewUUID(),
		UserID:           userID,
		WorkoutID:        workoutID,
		ExerciseID:       "lat-pulldown",
		SetNumber:        2,
		WeightKg:         62.5,
		Repetitions:      10,
		RIR:              2,
		ClientMutationID: mutationID,
	}
}

func testEmailUnique(t *testing.T, newStore Factory) {
	st := newStore(t)
	mustUser(t, st, "athlete@example.com")

	_, err := st.CreateUser(context.Background(), store.User{ID: ids.NewUUID(), Email: "ATHLETE@Example.com", PasswordHash: "x"})
	if !errors.Is(err, store.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken for a differently-cased duplicate", err)
	}
}

func testUserLookups(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")

	byEmail, err := st.UserByEmail(ctx, "  ATHLETE@example.com ")
	if err != nil || byEmail.ID != user.ID {
		t.Fatalf("UserByEmail = (%+v, %v)", byEmail, err)
	}
	byID, err := st.UserByID(ctx, user.ID)
	if err != nil || byID.Email != "athlete@example.com" {
		t.Fatalf("UserByID = (%+v, %v)", byID, err)
	}
	if _, err := st.UserByEmail(ctx, "nobody@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := st.UserByID(ctx, ids.NewUUID()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func testRefreshTokens(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")

	now := time.Now().UTC()
	token := store.RefreshToken{
		ID:        ids.NewUUID(),
		UserID:    user.ID,
		TokenHash: "hash-1",
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := st.CreateRefreshToken(ctx, token); err != nil {
		t.Fatalf("create refresh token: %v", err)
	}

	loaded, err := st.RefreshTokenByHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("load refresh token: %v", err)
	}
	if !loaded.Active(now) {
		t.Fatal("a fresh token must be active")
	}

	if err := st.RevokeRefreshToken(ctx, token.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	loaded, err = st.RefreshTokenByHash(ctx, "hash-1")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.RevokedAt == nil || loaded.Active(now) {
		t.Fatal("a revoked token must not be active")
	}

	if _, err := st.RefreshTokenByHash(ctx, "unknown"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}

	second := store.RefreshToken{ID: ids.NewUUID(), UserID: user.ID, TokenHash: "hash-2", IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := st.CreateRefreshToken(ctx, second); err != nil {
		t.Fatalf("create second token: %v", err)
	}
	if err := st.RevokeUserRefreshTokens(ctx, user.ID); err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	loaded, _ = st.RefreshTokenByHash(ctx, "hash-2")
	if loaded.RevokedAt == nil {
		t.Fatal("revoking the family must revoke every token of the user")
	}
}

func testWorkoutOwnership(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	owner := mustUser(t, st, "owner@example.com")
	intruder := mustUser(t, st, "intruder@example.com")
	workout := mustWorkout(t, st, owner.ID)

	if _, err := st.WorkoutForUser(ctx, owner.ID, workout.ID); err != nil {
		t.Fatalf("the owner must see their workout: %v", err)
	}
	if _, err := st.WorkoutForUser(ctx, intruder.ID, workout.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound so existence does not leak", err)
	}
	if _, err := st.WorkoutForUser(ctx, owner.ID, ids.NewUUID()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func testIdempotentSet(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")
	workout := mustWorkout(t, st, user.ID)

	first, created, err := st.InsertWorkoutSet(ctx, sampleSet(user.ID, workout.ID, "mutation-1"))
	if err != nil || !created {
		t.Fatalf("first insert = (%+v, %v, %v), want created", first, created, err)
	}

	replay := sampleSet(user.ID, workout.ID, "mutation-1")
	replay.Repetitions = 99 // even a changed payload must not create a row
	second, created, err := st.InsertWorkoutSet(ctx, replay)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if created {
		t.Fatal("a replayed clientMutationId must not create a second row")
	}
	if second.ID != first.ID || second.Repetitions != first.Repetitions {
		t.Fatalf("replay returned %+v, want the originally stored %+v", second, first)
	}

	sets, err := st.ListWorkoutSets(ctx, user.ID, workout.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("stored %d rows, want exactly 1", len(sets))
	}
}

func testMutationScope(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	alice := mustUser(t, st, "alice@example.com")
	bob := mustUser(t, st, "bob@example.com")
	aliceWorkout := mustWorkout(t, st, alice.ID)
	bobWorkout := mustWorkout(t, st, bob.ID)

	if _, created, err := st.InsertWorkoutSet(ctx, sampleSet(alice.ID, aliceWorkout.ID, "shared-id")); err != nil || !created {
		t.Fatalf("alice insert = (%v, %v)", created, err)
	}
	// The unique key is (user_id, client_mutation_id): the same mutation ID
	// from another user is a different row, not a duplicate.
	if _, created, err := st.InsertWorkoutSet(ctx, sampleSet(bob.ID, bobWorkout.ID, "shared-id")); err != nil || !created {
		t.Fatalf("bob insert = (%v, %v), want an independent row", created, err)
	}
}

func testForeignWorkout(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	owner := mustUser(t, st, "owner@example.com")
	intruder := mustUser(t, st, "intruder@example.com")
	workout := mustWorkout(t, st, owner.ID)

	_, _, err := st.InsertWorkoutSet(ctx, sampleSet(intruder.ID, workout.ID, "mutation-x"))
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound when writing into somebody else's workout", err)
	}

	sets, err := st.ListWorkoutSets(ctx, owner.ID, workout.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sets) != 0 {
		t.Fatalf("the foreign write leaked %d rows into the owner's workout", len(sets))
	}
}

func testListScope(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	alice := mustUser(t, st, "alice@example.com")
	bob := mustUser(t, st, "bob@example.com")
	aliceWorkout := mustWorkout(t, st, alice.ID)

	if _, _, err := st.InsertWorkoutSet(ctx, sampleSet(alice.ID, aliceWorkout.ID, "m1")); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := st.ListWorkoutSets(ctx, bob.ID, aliceWorkout.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound when listing another user's workout", err)
	}
}

func testConcurrentReplay(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()
	user := mustUser(t, st, "athlete@example.com")
	workout := mustWorkout(t, st, user.ID)

	const attempts = 16
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
		failed  error
	)
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := st.InsertWorkoutSet(ctx, sampleSet(user.ID, workout.ID, "racy-mutation"))
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = err
				return
			}
			if ok {
				created++
			}
		}()
	}
	wg.Wait()

	if failed != nil {
		t.Fatalf("concurrent insert failed: %v", failed)
	}
	if created != 1 {
		t.Fatalf("%d of %d concurrent replays were treated as new rows, want exactly 1", created, attempts)
	}
	sets, err := st.ListWorkoutSets(ctx, user.ID, workout.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("stored %d rows, want exactly 1", len(sets))
	}
}
