package httpapi_test

import (
	"net/http"
	"testing"
)

// A workout the client names itself is what makes an offline session possible:
// the phone can start training with no connection and still have one identity
// to hang its sets on, exactly as every set carries its own mutation ID.
func TestAClientCanNameTheWorkoutItStarts(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")
	id := "6f1a9d54-3d2e-4a41-9a5b-2c8f0a7b1e33"

	res := h.send(request{
		method: http.MethodPost,
		path:   basePath + "/workouts",
		token:  user.accessToken,
		body:   map[string]any{"id": id, "title": "Тяга"},
	})

	if res.status != http.StatusCreated {
		t.Fatalf("status %d, want 201 (body %s)", res.status, res.body)
	}
	if got := res.str(t, "id"); got != id {
		t.Fatalf("id %q, want the one the client chose %q", got, id)
	}

	// The set must land, which is the whole point: before this the app wrote
	// sets into a workout the server had never heard of and got 404.
	h.logSet(user.accessToken, id, "lat-pulldown", 62.5, 10, "m-1")
}

// A lost response must settle, not fork: replaying the create returns the
// stored workout instead of starting a second session.
func TestReplayingACreateReturnsTheSameWorkout(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")
	id := "6f1a9d54-3d2e-4a41-9a5b-2c8f0a7b1e33"

	first := h.send(request{method: http.MethodPost, path: basePath + "/workouts", token: user.accessToken,
		body: map[string]any{"id": id, "title": "Тяга"}})
	second := h.send(request{method: http.MethodPost, path: basePath + "/workouts", token: user.accessToken,
		body: map[string]any{"id": id, "title": "Другое название"}})

	if first.status != http.StatusCreated {
		t.Fatalf("first create: status %d, want 201", first.status)
	}
	if second.status != http.StatusOK {
		t.Fatalf("replay: status %d, want 200 (body %s)", second.status, second.body)
	}
	if got := second.str(t, "title"); got != "Тяга" {
		t.Fatalf("title %q — a replay must not rewrite the stored workout", got)
	}
	if second.str(t, "createdAt") != first.str(t, "createdAt") {
		t.Fatal("a replay must return the stored row, not a fresh one")
	}

	// One session, not two.
	list := h.send(request{method: http.MethodGet, path: basePath + "/workouts", token: user.accessToken})
	items, ok := list.json(t)["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("workout count %d, want exactly 1 (body %s)", len(items), list.body)
	}
}

// An ID another account holds must be refused without confirming it exists,
// and without handing that workout over.
func TestAWorkoutIDHeldByAnotherUserIsRefused(t *testing.T) {
	h := newHarness(t, nil)
	owner := h.register("owner@example.com", "correct-horse-battery")
	stranger := h.register("stranger@example.com", "correct-horse-battery")
	id := "6f1a9d54-3d2e-4a41-9a5b-2c8f0a7b1e33"

	h.send(request{method: http.MethodPost, path: basePath + "/workouts", token: owner.accessToken,
		body: map[string]any{"id": id, "title": "Тяга"}})

	res := h.send(request{method: http.MethodPost, path: basePath + "/workouts", token: stranger.accessToken,
		body: map[string]any{"id": id, "title": "Чужая"}})

	if res.status != http.StatusConflict {
		t.Fatalf("status %d, want 409 (body %s)", res.status, res.body)
	}

	// The stranger must not have gained access to the owner's workout.
	detail := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + id, token: stranger.accessToken})
	if detail.status != http.StatusNotFound {
		t.Fatalf("foreign workout readable: status %d, want 404", detail.status)
	}
	list := h.send(request{method: http.MethodGet, path: basePath + "/workouts", token: stranger.accessToken})
	if items, _ := list.json(t)["items"].([]any); len(items) != 0 {
		t.Fatalf("stranger sees %d workouts, want 0", len(items))
	}
}

// The server still names the workout when the client does not care to.
func TestAnOmittedIDIsStillServerGenerated(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")

	res := h.send(request{method: http.MethodPost, path: basePath + "/workouts", token: user.accessToken,
		body: map[string]any{"title": "Без идентификатора"}})

	if res.status != http.StatusCreated {
		t.Fatalf("status %d, want 201 (body %s)", res.status, res.body)
	}
	if res.str(t, "id") == "" {
		t.Fatal("server must generate an id when the client omits one")
	}
}

// A malformed ID is a validation problem, never a conflict and never a 500.
func TestAMalformedWorkoutIDIsRejected(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")

	res := h.send(request{method: http.MethodPost, path: basePath + "/workouts", token: user.accessToken,
		body: map[string]any{"id": "demo-strength", "title": "Тяга"}})

	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422 (body %s)", res.status, res.body)
	}
}
