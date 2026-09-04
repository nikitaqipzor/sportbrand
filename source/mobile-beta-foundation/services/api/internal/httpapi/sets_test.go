package httpapi_test

import (
	"bytes"
	"net/http"
	"testing"

	"athletica.ai/api/internal/ids"
)

func TestLogSetRequiresAuthentication(t *testing.T) {
	h := newHarness(t, nil)
	owner := h.register("athlete@example.com", "correct-horse-battery")
	workoutID := h.createWorkout(owner.accessToken)
	path := basePath + "/workouts/" + workoutID + "/sets"

	cases := map[string]string{
		"no token":      "",
		"garbage token": "not-a-jwt",
		"forged token":  "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJhdHRhY2tlciIsInR5cCI6ImFjY2VzcyIsImV4cCI6OTk5OTk5OTk5OX0.",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			res := h.send(request{method: http.MethodPost, path: path, token: token, body: validSetBody("m-1")})
			if res.status != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body %s", res.status, res.body)
			}
			if h.mem.CountWorkoutSets(owner.id) != 0 {
				t.Fatal("an unauthenticated request must not write anything")
			}
		})
	}
}

// The core Sprint-1 invariant: a replayed clientMutationId never produces a
// second row, and the client gets the originally stored set back.
func TestLogSetIsIdempotent(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")
	workoutID := h.createWorkout(acc.accessToken)
	path := basePath + "/workouts/" + workoutID + "/sets"

	first := h.send(request{method: http.MethodPost, path: path, token: acc.accessToken, body: validSetBody("outbox-1")})
	if first.status != http.StatusCreated {
		t.Fatalf("first write: status %d, body %s", first.status, first.body)
	}
	setID := first.str(t, "id")

	// Same mutation ID, and even a different payload: still not a new row.
	replayBody := validSetBody("outbox-1")
	replayBody["repetitions"] = 99
	replay := h.send(request{method: http.MethodPost, path: path, token: acc.accessToken, body: replayBody})
	if replay.status != http.StatusConflict {
		t.Fatalf("replay: status %d, want 409; body %s", replay.status, replay.body)
	}
	if got := replay.str(t, "error", "code"); got != "duplicate_client_mutation" {
		t.Fatalf("error code = %q", got)
	}
	if got := replay.str(t, "set", "id"); got != setID {
		t.Fatalf("replay returned set %q, want the originally stored %q", got, setID)
	}
	if got := replay.json(t)["set"].(map[string]any)["repetitions"].(float64); got != 10 {
		t.Fatalf("replay returned repetitions %v, want the stored 10", got)
	}

	// Exactly one row, whatever the client did.
	if n := h.mem.CountWorkoutSets(acc.id); n != 1 {
		t.Fatalf("stored %d rows, want 1", n)
	}
	list := h.send(request{method: http.MethodGet, path: path, token: acc.accessToken})
	items := list.json(t)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("GET returned %d sets, want 1", len(items))
	}

	// A different mutation ID is a genuinely new set.
	second := h.send(request{method: http.MethodPost, path: path, token: acc.accessToken, body: validSetBody("outbox-2")})
	if second.status != http.StatusCreated {
		t.Fatalf("second write: status %d, body %s", second.status, second.body)
	}
	if n := h.mem.CountWorkoutSets(acc.id); n != 2 {
		t.Fatalf("stored %d rows, want 2", n)
	}
}

// Audit finding H1: the owner comes from the token, never from the payload.
func TestLogSetIgnoresUserIdInBody(t *testing.T) {
	h := newHarness(t, nil)
	victim := h.register("victim@example.com", "correct-horse-battery")
	attacker := h.register("attacker@example.com", "correct-horse-battery")

	attackerWorkout := h.createWorkout(attacker.accessToken)
	body := validSetBody("spoof-1")
	body["userId"] = victim.id
	body["user_id"] = victim.id

	res := h.send(request{
		method: http.MethodPost,
		path:   basePath + "/workouts/" + attackerWorkout + "/sets",
		token:  attacker.accessToken,
		body:   body,
	})
	if res.status != http.StatusCreated {
		t.Fatalf("status = %d, body %s", res.status, res.body)
	}
	if n := h.mem.CountWorkoutSets(victim.id); n != 0 {
		t.Fatalf("%d rows were written under the victim's id", n)
	}
	if n := h.mem.CountWorkoutSets(attacker.id); n != 1 {
		t.Fatalf("attacker owns %d rows, want 1", n)
	}
}

// Audit finding H1: another user's workout must be unreachable, and its
// existence must not leak through a different status code or message.
func TestForeignWorkoutIsIndistinguishableFromMissing(t *testing.T) {
	h := newHarness(t, nil)
	owner := h.register("owner@example.com", "correct-horse-battery")
	intruder := h.register("intruder@example.com", "correct-horse-battery")
	ownerWorkout := h.createWorkout(owner.accessToken)
	missingWorkout := ids.NewUUID()

	foreign := h.send(request{
		method: http.MethodPost,
		path:   basePath + "/workouts/" + ownerWorkout + "/sets",
		token:  intruder.accessToken,
		body:   validSetBody("intrusion-1"),
	})
	missing := h.send(request{
		method: http.MethodPost,
		path:   basePath + "/workouts/" + missingWorkout + "/sets",
		token:  intruder.accessToken,
		body:   validSetBody("intrusion-2"),
	})

	if foreign.status != http.StatusNotFound || missing.status != http.StatusNotFound {
		t.Fatalf("statuses = %d and %d, want 404 for both", foreign.status, missing.status)
	}
	if !bytes.Equal(foreign.body, missing.body) {
		t.Fatalf("a foreign workout answers differently from a missing one:\n %s\n %s", foreign.body, missing.body)
	}
	if h.mem.CountWorkoutSets(intruder.id) != 0 || h.mem.CountWorkoutSets(owner.id) != 0 {
		t.Fatal("the rejected write must not have stored anything")
	}

	// Reading is scoped the same way.
	read := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + ownerWorkout + "/sets", token: intruder.accessToken})
	if read.status != http.StatusNotFound {
		t.Fatalf("GET of a foreign workout: status %d, want 404", read.status)
	}
}

// A mutation ID is unique per user, so two users may safely use the same one.
func TestUsersAreIsolatedAcrossTheSameMutationId(t *testing.T) {
	h := newHarness(t, nil)
	alice := h.register("alice@example.com", "correct-horse-battery")
	bob := h.register("bob@example.com", "correct-horse-battery")
	aliceWorkout := h.createWorkout(alice.accessToken)
	bobWorkout := h.createWorkout(bob.accessToken)

	for _, tc := range []struct {
		token, workout string
	}{{alice.accessToken, aliceWorkout}, {bob.accessToken, bobWorkout}} {
		res := h.send(request{method: http.MethodPost, path: basePath + "/workouts/" + tc.workout + "/sets", token: tc.token, body: validSetBody("shared-outbox-id")})
		if res.status != http.StatusCreated {
			t.Fatalf("status = %d, body %s", res.status, res.body)
		}
	}

	if n := h.mem.CountWorkoutSets(alice.id); n != 1 {
		t.Fatalf("alice owns %d rows, want 1", n)
	}
	if n := h.mem.CountWorkoutSets(bob.id); n != 1 {
		t.Fatalf("bob owns %d rows, want 1", n)
	}

	aliceList := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + aliceWorkout + "/sets", token: alice.accessToken})
	items := aliceList.json(t)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("alice sees %d sets, want only her own", len(items))
	}
	if got := items[0].(map[string]any)["workoutId"].(string); got != aliceWorkout {
		t.Fatalf("alice sees a set from workout %q", got)
	}
}

// The HTTP layer enforces exactly the bounds of packages/domain/src/workout.ts.
func TestLogSetValidationOverHTTP(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")
	workoutID := h.createWorkout(acc.accessToken)
	path := basePath + "/workouts/" + workoutID + "/sets"

	cases := []struct {
		name   string
		mutate func(map[string]any)
		status int
	}{
		{"valid", func(map[string]any) {}, http.StatusCreated},
		{"set number 1", func(b map[string]any) { b["setNumber"] = 1 }, http.StatusCreated},
		{"set number 0", func(b map[string]any) { b["setNumber"] = 0 }, http.StatusUnprocessableEntity},
		{"negative set number", func(b map[string]any) { b["setNumber"] = -1 }, http.StatusUnprocessableEntity},
		{"weight 0", func(b map[string]any) { b["weightKg"] = 0 }, http.StatusCreated},
		{"weight 1000", func(b map[string]any) { b["weightKg"] = 1000 }, http.StatusCreated},
		{"weight 1000.5", func(b map[string]any) { b["weightKg"] = 1000.5 }, http.StatusUnprocessableEntity},
		{"negative weight", func(b map[string]any) { b["weightKg"] = -1 }, http.StatusUnprocessableEntity},
		{"1 repetition", func(b map[string]any) { b["repetitions"] = 1 }, http.StatusCreated},
		{"100 repetitions", func(b map[string]any) { b["repetitions"] = 100 }, http.StatusCreated},
		{"0 repetitions", func(b map[string]any) { b["repetitions"] = 0 }, http.StatusUnprocessableEntity},
		{"101 repetitions", func(b map[string]any) { b["repetitions"] = 101 }, http.StatusUnprocessableEntity},
		{"rir 0", func(b map[string]any) { b["rir"] = 0 }, http.StatusCreated},
		{"rir 10", func(b map[string]any) { b["rir"] = 10 }, http.StatusCreated},
		{"rir 11", func(b map[string]any) { b["rir"] = 11 }, http.StatusUnprocessableEntity},
		{"rir -1", func(b map[string]any) { b["rir"] = -1 }, http.StatusUnprocessableEntity},
		{"missing exerciseId", func(b map[string]any) { delete(b, "exerciseId") }, http.StatusUnprocessableEntity},
		{"blank exerciseId", func(b map[string]any) { b["exerciseId"] = "   " }, http.StatusUnprocessableEntity},
		{"missing clientMutationId", func(b map[string]any) { delete(b, "clientMutationId") }, http.StatusUnprocessableEntity},
		{"missing setNumber", func(b map[string]any) { delete(b, "setNumber") }, http.StatusUnprocessableEntity},
		{"missing weightKg", func(b map[string]any) { delete(b, "weightKg") }, http.StatusUnprocessableEntity},
		{"missing rir", func(b map[string]any) { delete(b, "rir") }, http.StatusUnprocessableEntity},
		{"fractional setNumber", func(b map[string]any) { b["setNumber"] = 1.5 }, http.StatusBadRequest},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := validSetBody("case-" + string(rune('a'+i)))
			tc.mutate(body)

			res := h.send(request{method: http.MethodPost, path: path, token: acc.accessToken, body: body})
			if res.status != tc.status {
				t.Fatalf("status = %d, want %d; body %s", res.status, tc.status, res.body)
			}
		})
	}
}

func TestLogSetRejectsMalformedBody(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")
	path := basePath + "/workouts/" + h.createWorkout(acc.accessToken) + "/sets"

	for _, body := range []string{"", "not json", `{"exerciseId":"x"}{"exerciseId":"y"}`} {
		res := h.send(request{method: http.MethodPost, path: path, token: acc.accessToken, body: body})
		if res.status != http.StatusBadRequest {
			t.Fatalf("body %q: status %d, want 400", body, res.status)
		}
	}
}

func TestCreateWorkoutIsScopedToTheCaller(t *testing.T) {
	h := newHarness(t, nil)
	alice := h.register("alice@example.com", "correct-horse-battery")
	bob := h.register("bob@example.com", "correct-horse-battery")

	workoutID := h.createWorkout(alice.accessToken)

	res := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + workoutID + "/sets", token: bob.accessToken})
	if res.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for another user's workout", res.status)
	}
}
