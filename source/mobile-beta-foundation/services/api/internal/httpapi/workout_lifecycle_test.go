package httpapi_test

import (
	"net/http"
	"testing"
)

// statuses, in the order the table below walks them.
var allStatuses = []string{"active", "paused", "completed", "cancelled"}

// allowed is the transition table from the domain, restated here on purpose:
// the test must fail when somebody widens the domain table by accident.
var allowed = map[string]map[string]bool{
	"active":    {"paused": true, "completed": true, "cancelled": true},
	"paused":    {"active": true, "completed": true, "cancelled": true},
	"completed": {},
	"cancelled": {},
}

// putInStatus creates a fresh workout and drives it to the wanted status.
func putInStatus(t *testing.T, h *harness, token, status string) string {
	t.Helper()
	id := h.createWorkout(token)
	if status != "active" {
		h.mustTransition(token, id, status)
	}
	return id
}

// TestEveryTransitionMatchesTheDomainTable walks all 16 (from, to) pairs.
//
// The two rules that matter: a move the table forbids answers 409 and leaves
// the workout untouched, and asking for the status a workout already holds is
// an accepted no-op so a retried request is safe.
func TestEveryTransitionMatchesTheDomainTable(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")

	for _, from := range allStatuses {
		for _, to := range allStatuses {
			t.Run(from+"_to_"+to, func(t *testing.T) {
				id := putInStatus(t, h, user.accessToken, from)
				res := h.transition(user.accessToken, id, to)

				want := http.StatusConflict
				if from == to || allowed[from][to] {
					want = http.StatusOK
				}
				if res.status != want {
					t.Fatalf("%s -> %s: status %d, want %d (body %s)", from, to, res.status, want, res.body)
				}

				after := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + id, token: user.accessToken})
				got := after.str(t, "status")
				switch {
				case want == http.StatusOK && got != to:
					t.Fatalf("%s -> %s succeeded but the workout is %s", from, to, got)
				case want == http.StatusConflict && got != from:
					t.Fatalf("%s -> %s was refused but the workout changed to %s", from, to, got)
				}
				if want == http.StatusConflict && after.status == http.StatusOK {
					if code := res.str(t, "error", "code"); code != "invalid_transition" {
						t.Fatalf("refusal carries code %q, want invalid_transition", code)
					}
				}
			})
		}
	}
}

// TestCancelIsPossibleFromEveryUnfinishedStatus is audit blocker QA-004: the
// athlete could not abandon a session at all. It must work from active and
// from paused, and it must set endedAt.
func TestCancelIsPossibleFromEveryUnfinishedStatus(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")

	for _, from := range []string{"active", "paused"} {
		t.Run("from_"+from, func(t *testing.T) {
			id := putInStatus(t, h, user.accessToken, from)
			res := h.transition(user.accessToken, id, "cancelled")
			if res.status != http.StatusOK {
				t.Fatalf("cancel from %s: status %d, body %s", from, res.status, res.body)
			}
			if got := res.str(t, "status"); got != "cancelled" {
				t.Fatalf("status = %s, want cancelled", got)
			}
			if res.json(t)["endedAt"] == nil {
				t.Fatalf("a cancelled workout must carry endedAt, got %s", res.body)
			}
		})
	}
}

// TestCompletedWorkoutKeepsItsSets: cancelling or completing never destroys the
// log — the "Итоги" screen still has something to render.
func TestCompletedWorkoutKeepsItsSets(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")
	id := h.createWorkout(user.accessToken)
	h.logSet(user.accessToken, id, "squat", 100, 5, "m1")
	h.mustTransition(user.accessToken, id, "completed")

	res := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + id, token: user.accessToken})
	if res.status != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.status, res.body)
	}
	body := res.json(t)
	sets, _ := body["sets"].([]any)
	if len(sets) != 1 {
		t.Fatalf("detail carries %d sets, want 1: %s", len(sets), res.body)
	}
	totals, _ := body["totals"].(map[string]any)
	if totals["repetitions"] != float64(5) || totals["volumeKg"] != float64(500) {
		t.Fatalf("totals = %v, want 5 reps and 500 kg of volume", totals)
	}
}

// TestUnknownStatusIsAValidationErrorNotAConflict keeps 409 meaning exactly
// one thing: a real lifecycle conflict.
func TestUnknownStatusIsAValidationErrorNotAConflict(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")
	id := h.createWorkout(user.accessToken)

	for _, status := range []string{"finished", "", "ACTIVE!"} {
		res := h.transition(user.accessToken, id, status)
		if res.status != http.StatusUnprocessableEntity {
			t.Fatalf("status %q: %d, body %s, want 422", status, res.status, res.body)
		}
	}
	missing := h.send(request{method: http.MethodPost, path: basePath + "/workouts/" + id + "/status", token: user.accessToken, body: map[string]any{}})
	if missing.status != http.StatusUnprocessableEntity {
		t.Fatalf("missing status field: %d, want 422", missing.status)
	}
}

// TestForeignWorkoutCannotBeTransitioned: a workout of another athlete answers
// exactly like one that does not exist, whatever its real status is.
func TestForeignWorkoutCannotBeTransitioned(t *testing.T) {
	h := newHarness(t, nil)
	owner := h.register("owner@example.com", "correct-horse-battery")
	intruder := h.register("intruder@example.com", "correct-horse-battery")

	id := h.createWorkout(owner.accessToken)
	foreign := h.transition(intruder.accessToken, id, "cancelled")
	missing := h.transition(intruder.accessToken, "3f6c1b2a-0000-4000-8000-000000000000", "cancelled")

	if foreign.status != http.StatusNotFound || missing.status != http.StatusNotFound {
		t.Fatalf("foreign=%d missing=%d, want both 404", foreign.status, missing.status)
	}
	if string(foreign.body) != string(missing.body) {
		t.Fatalf("bodies differ:\n foreign %s\n missing %s\nexistence leaks", foreign.body, missing.body)
	}

	// And the owner's workout is untouched.
	after := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + id, token: owner.accessToken})
	if got := after.str(t, "status"); got != "active" {
		t.Fatalf("the intruder changed the workout to %s", got)
	}
}

// TestWorkoutDetailIsUserScoped covers the single-workout read.
func TestWorkoutDetailIsUserScoped(t *testing.T) {
	h := newHarness(t, nil)
	owner := h.register("owner@example.com", "correct-horse-battery")
	intruder := h.register("intruder@example.com", "correct-horse-battery")
	id := h.createWorkout(owner.accessToken)
	h.logSet(owner.accessToken, id, "squat", 100, 5, "m1")

	foreign := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + id, token: intruder.accessToken})
	missing := h.send(request{method: http.MethodGet, path: basePath + "/workouts/3f6c1b2a-0000-4000-8000-000000000000", token: intruder.accessToken})
	if foreign.status != http.StatusNotFound || string(foreign.body) != string(missing.body) {
		t.Fatalf("foreign (%d) %s vs missing (%d) %s: the two must be identical",
			foreign.status, foreign.body, missing.status, missing.body)
	}

	unauthenticated := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + id})
	if unauthenticated.status != http.StatusUnauthorized {
		t.Fatalf("anonymous read = %d, want 401", unauthenticated.status)
	}
}
