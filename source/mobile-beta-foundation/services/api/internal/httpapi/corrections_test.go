package httpapi_test

import (
	"net/http"
	"testing"

	"athletica.ai/api/internal/ids"
)

// logSetReturning writes one set and hands back its server-assigned ID.
func (h *harness) logSetReturning(token, workoutID string, body map[string]any) string {
	h.t.Helper()
	res := h.send(request{method: http.MethodPost, path: basePath + "/workouts/" + workoutID + "/sets", token: token, body: body})
	if res.status != http.StatusCreated {
		h.t.Fatalf("log set: status %d, body %s", res.status, res.body)
	}
	return res.str(h.t, "id")
}

func setPath(workoutID, setID string) string {
	return basePath + "/workouts/" + workoutID + "/sets/" + setID
}

// The core claim of the correction feature: an edit replayed out of the offline
// outbox is applied once, whatever payload the retry carries.
func TestCorrectingASetIsIdempotent(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")
	workoutID := h.createWorkout(acc.accessToken)
	setID := h.logSetReturning(acc.accessToken, workoutID, validSetBody("create:1"))

	edit := map[string]any{"weightKg": 70, "repetitions": 8, "rir": 1, "clientMutationId": "edit:1"}
	first := h.send(request{method: http.MethodPatch, path: setPath(workoutID, setID), token: acc.accessToken, body: edit})
	if first.status != http.StatusOK {
		t.Fatalf("first edit: status %d, body %s", first.status, first.body)
	}
	body := first.json(t)
	if body["weightKg"] != 70.0 || body["repetitions"] != 8.0 || body["rir"] != 1.0 {
		t.Fatalf("edit stored %v", body)
	}
	// The identity of the set is untouched: same row, same number, same
	// creation mutation ID the client derived from workoutId:exerciseId:number.
	if first.str(t, "id") != setID || body["setNumber"] != 2.0 || first.str(t, "clientMutationId") != "create:1" {
		t.Fatalf("the correction re-identified the set: %s", first.body)
	}
	if body["deletedAt"] != nil {
		t.Fatalf("a corrected set must not be deleted: %s", first.body)
	}

	// The replay carries different numbers on purpose: applying it twice would
	// be visible in the answer, and must not happen.
	replay := map[string]any{"weightKg": 999, "repetitions": 1, "rir": 9, "clientMutationId": "edit:1"}
	second := h.send(request{method: http.MethodPatch, path: setPath(workoutID, setID), token: acc.accessToken, body: replay})
	if second.status != http.StatusConflict {
		t.Fatalf("replay: status %d, want 409; body %s", second.status, second.body)
	}
	if got := second.str(t, "error", "code"); got != "duplicate_client_mutation" {
		t.Fatalf("error code = %q", got)
	}
	stored := second.json(t)["set"].(map[string]any)
	if stored["weightKg"] != 70.0 || stored["repetitions"] != 8.0 {
		t.Fatalf("the replay applied a second time: %v", stored)
	}

	// And the detail screen shows the first edit, not the replay.
	detail := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + workoutID, token: acc.accessToken})
	sets := detail.json(t)["sets"].([]any)
	if len(sets) != 1 {
		t.Fatalf("detail holds %d sets, want 1", len(sets))
	}
	if sets[0].(map[string]any)["weightKg"] != 70.0 {
		t.Fatalf("detail shows %v", sets[0])
	}
	if totals := detail.json(t)["totals"].(map[string]any); totals["repetitions"] != 8.0 || totals["volumeKg"] != 560.0 {
		t.Fatalf("totals were not recomputed from the correction: %v", totals)
	}
}

// A correction may not push a set outside the bounds the original write was
// held to — otherwise editing would be a way around the domain.
func TestCorrectionCannotLeaveTheDomainBounds(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")
	workoutID := h.createWorkout(acc.accessToken)
	setID := h.logSetReturning(acc.accessToken, workoutID, validSetBody("create:1"))

	cases := map[string]map[string]any{
		"weight above 1000":   {"weightKg": 1000.01, "repetitions": 8, "rir": 1},
		"negative weight":     {"weightKg": -1, "repetitions": 8, "rir": 1},
		"zero repetitions":    {"weightKg": 60, "repetitions": 0, "rir": 1},
		"repetitions past100": {"weightKg": 60, "repetitions": 101, "rir": 1},
		"negative rir":        {"weightKg": 60, "repetitions": 8, "rir": -1},
		"rir above 10":        {"weightKg": 60, "repetitions": 8, "rir": 11},
	}
	for name, values := range cases {
		t.Run(name, func(t *testing.T) {
			body := map[string]any{"clientMutationId": "edit:" + name}
			for k, v := range values {
				body[k] = v
			}
			res := h.send(request{method: http.MethodPatch, path: setPath(workoutID, setID), token: acc.accessToken, body: body})
			if res.status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body %s", res.status, res.body)
			}
			if got := res.str(t, "error", "code"); got != "validation_failed" {
				t.Fatalf("error code = %q", got)
			}
		})
	}

	// Nothing above was stored: the set is exactly as it was logged.
	sets := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + workoutID + "/sets", token: acc.accessToken})
	items := sets.json(t)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["weightKg"] != 62.5 {
		t.Fatalf("a rejected correction wrote something: %s", sets.body)
	}
}

// Every required field must be named rather than silently defaulted: a PATCH
// that omits repetitions must not quietly set them to zero.
func TestCorrectionRequiresEveryValue(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")
	workoutID := h.createWorkout(acc.accessToken)
	setID := h.logSetReturning(acc.accessToken, workoutID, validSetBody("create:1"))

	res := h.send(request{method: http.MethodPatch, path: setPath(workoutID, setID), token: acc.accessToken,
		body: map[string]any{"weightKg": 70, "clientMutationId": "edit:1"}})
	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %s", res.status, res.body)
	}

	// And so must the mutation ID: an edit with no ID cannot be deduplicated.
	res = h.send(request{method: http.MethodPatch, path: setPath(workoutID, setID), token: acc.accessToken,
		body: map[string]any{"weightKg": 70, "repetitions": 8, "rir": 1}})
	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("missing clientMutationId: status = %d, want 422; body %s", res.status, res.body)
	}
}

// Deleting is soft, repeatable and invisible afterwards.
func TestDeletingASetIsSafeToRepeat(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")
	workoutID := h.createWorkout(acc.accessToken)
	setID := h.logSetReturning(acc.accessToken, workoutID, validSetBody("create:1"))

	del := map[string]any{"clientMutationId": "delete:1"}
	first := h.send(request{method: http.MethodDelete, path: setPath(workoutID, setID), token: acc.accessToken, body: del})
	if first.status != http.StatusOK {
		t.Fatalf("first delete: status %d, body %s", first.status, first.body)
	}
	deletedAt := first.str(t, "deletedAt")

	// The same queued deletion again.
	second := h.send(request{method: http.MethodDelete, path: setPath(workoutID, setID), token: acc.accessToken, body: del})
	if second.status != http.StatusOK {
		t.Fatalf("replayed delete: status %d, want 200; body %s", second.status, second.body)
	}
	if second.str(t, "deletedAt") != deletedAt {
		t.Fatalf("the replay moved deletedAt from %s to %s", deletedAt, second.str(t, "deletedAt"))
	}

	// A *different* mutation ID pointed at an already-deleted set is equally
	// safe: the state it asks for is the state that already holds.
	third := h.send(request{method: http.MethodDelete, path: setPath(workoutID, setID), token: acc.accessToken,
		body: map[string]any{"clientMutationId": "delete:2"}})
	if third.status != http.StatusOK || third.str(t, "deletedAt") != deletedAt {
		t.Fatalf("second delete: status %d, body %s", third.status, third.body)
	}

	// The mutation ID may also travel in the query string, for clients and
	// proxies that drop a body from a DELETE.
	fourth := h.send(request{method: http.MethodDelete,
		path: setPath(workoutID, setID) + "?clientMutationId=delete:3", token: acc.accessToken})
	if fourth.status != http.StatusOK {
		t.Fatalf("query-parameter delete: status %d, body %s", fourth.status, fourth.body)
	}

	// It is gone from the detail and from the set list.
	detail := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + workoutID, token: acc.accessToken})
	if sets := detail.json(t)["sets"].([]any); len(sets) != 0 {
		t.Fatalf("a deleted set is still on the detail screen: %s", detail.body)
	}
	totals := detail.json(t)["totals"].(map[string]any)
	if totals["sets"] != 0.0 || totals["repetitions"] != 0.0 || totals["volumeKg"] != 0.0 {
		t.Fatalf("totals still count the deleted set: %v", totals)
	}
	list := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + workoutID + "/sets", token: acc.accessToken})
	if items := list.json(t)["items"].([]any); len(items) != 0 {
		t.Fatalf("a deleted set is still listed: %s", list.body)
	}

	// Exactly one row exists throughout: the deletion is soft, not a second row.
	if n := h.mem.CountWorkoutSets(acc.id); n != 1 {
		t.Fatalf("stored %d rows, want the single soft-deleted one", n)
	}

	// And the outbox replaying the *creation* cannot bring it back.
	resurrect := h.send(request{method: http.MethodPost, path: basePath + "/workouts/" + workoutID + "/sets",
		token: acc.accessToken, body: validSetBody("create:1")})
	if resurrect.status != http.StatusConflict {
		t.Fatalf("replayed creation: status %d, want 409; body %s", resurrect.status, resurrect.body)
	}
	if h.mem.CountWorkoutSets(acc.id) != 1 {
		t.Fatal("a replayed creation resurrected the deleted set")
	}
	if resurrect.json(t)["set"].(map[string]any)["deletedAt"] == nil {
		t.Fatalf("the replay must report the set as deleted: %s", resurrect.body)
	}
}

// A deleted set must disappear from "Прогресс" too, not only from "Итоги" —
// that is the whole point of being able to remove a mistyped record.
func TestDeletedSetLeavesTheProgressAggregates(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")
	workoutID := h.createWorkout(acc.accessToken)

	h.logSet(acc.accessToken, workoutID, "squat", 100, 5, "create:1")
	typo := validSetBody("create:2")
	typo["exerciseId"] = "squat"
	typo["weightKg"] = 300
	typo["repetitions"] = 5
	typo["setNumber"] = 2
	typoID := h.logSetReturning(acc.accessToken, workoutID, typo)
	h.mustTransition(acc.accessToken, workoutID, "completed")

	before := h.send(request{method: http.MethodGet, path: basePath + "/progress", token: acc.accessToken})
	record := before.json(t)["strength"].([]any)[0].(map[string]any)
	if record["bestWeight"].(map[string]any)["weightKg"] != 300.0 {
		t.Fatalf("the typo should hold the record before it is deleted: %v", record)
	}

	res := h.send(request{method: http.MethodDelete, path: setPath(workoutID, typoID), token: acc.accessToken,
		body: map[string]any{"clientMutationId": "delete:typo"}})
	if res.status != http.StatusOK {
		t.Fatalf("delete: status %d, body %s", res.status, res.body)
	}

	after := h.send(request{method: http.MethodGet, path: basePath + "/progress", token: acc.accessToken})
	strength := after.json(t)["strength"].([]any)
	if len(strength) != 1 {
		t.Fatalf("strength = %s", after.body)
	}
	record = strength[0].(map[string]any)
	if record["bestWeight"].(map[string]any)["weightKg"] != 100.0 {
		t.Fatalf("the deleted set still holds the record: %v", record)
	}
	if record["sets"] != 1.0 || record["volumeKg"] != 500.0 {
		t.Fatalf("the deleted set still counts towards the totals: %v", record)
	}
	weeks := after.json(t)["weeklyVolume"].([]any)
	for _, week := range weeks {
		w := week.(map[string]any)
		if w["sets"] != 1.0 || w["volumeKg"] != 500.0 {
			t.Fatalf("weekly volume still counts the deleted set: %v", w)
		}
	}
}

// Ownership: another athlete's set can be neither corrected nor deleted, and
// the refusal is byte-for-byte the refusal a set that never existed gets.
func TestForeignSetCannotBeCorrectedOrDeleted(t *testing.T) {
	h := newHarness(t, nil)
	owner := h.register("owner@example.com", "correct-horse-battery")
	intruder := h.register("intruder@example.com", "correct-horse-battery")

	workoutID := h.createWorkout(owner.accessToken)
	setID := h.logSetReturning(owner.accessToken, workoutID, validSetBody("create:1"))
	intruderWorkout := h.createWorkout(intruder.accessToken)

	edit := map[string]any{"weightKg": 5, "repetitions": 1, "rir": 0, "clientMutationId": "steal:1"}
	del := map[string]any{"clientMutationId": "steal:2"}

	// The 404 a *missing* set gets, to compare every other answer against.
	missing := h.send(request{method: http.MethodPatch,
		path: setPath(intruderWorkout, ids.NewUUID()), token: intruder.accessToken, body: edit})
	if missing.status != http.StatusNotFound {
		t.Fatalf("missing set: status %d, body %s", missing.status, missing.body)
	}

	targets := map[string]string{
		"through the owner's workout":    setPath(workoutID, setID),
		"through the intruder's workout": setPath(intruderWorkout, setID),
	}
	for name, path := range targets {
		t.Run("edit "+name, func(t *testing.T) {
			res := h.send(request{method: http.MethodPatch, path: path, token: intruder.accessToken, body: edit})
			if res.status != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body %s", res.status, res.body)
			}
			if string(res.body) != string(missing.body) {
				t.Fatalf("a foreign set answers %s, a missing one %s — the two must be identical", res.body, missing.body)
			}
		})
		t.Run("delete "+name, func(t *testing.T) {
			res := h.send(request{method: http.MethodDelete, path: path, token: intruder.accessToken, body: del})
			if res.status != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body %s", res.status, res.body)
			}
			if string(res.body) != string(missing.body) {
				t.Fatalf("a foreign set answers %s, a missing one %s — the two must be identical", res.body, missing.body)
			}
		})
	}

	// The owner's set is untouched and still live.
	list := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + workoutID + "/sets", token: owner.accessToken})
	items := list.json(t)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("the owner has %d sets, want 1: %s", len(items), list.body)
	}
	set := items[0].(map[string]any)
	if set["weightKg"] != 62.5 || set["deletedAt"] != nil {
		t.Fatalf("the intruder changed the owner's set: %v", set)
	}
	// And nothing was recorded in the intruder's mutation ledger either.
	if n := h.mem.CountClientMutations(intruder.id); n != 0 {
		t.Fatalf("a refused intrusion wrote %d ledger rows", n)
	}
}

// Correcting inside a finished session is the case the feature exists for:
// the athlete sees the typo on the "Итоги" screen, which only appears once the
// workout is completed. A cancelled session is the one place it is refused.
func TestCorrectionAndWorkoutStatus(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")

	completed := h.createWorkout(acc.accessToken)
	setID := h.logSetReturning(acc.accessToken, completed, validSetBody("create:1"))
	h.mustTransition(acc.accessToken, completed, "completed")

	res := h.send(request{method: http.MethodPatch, path: setPath(completed, setID), token: acc.accessToken,
		body: map[string]any{"weightKg": 65, "repetitions": 9, "rir": 2, "clientMutationId": "edit:1"}})
	if res.status != http.StatusOK {
		t.Fatalf("editing a completed workout: status %d, body %s", res.status, res.body)
	}

	// Deleting inside it works too.
	res = h.send(request{method: http.MethodDelete, path: setPath(completed, setID), token: acc.accessToken,
		body: map[string]any{"clientMutationId": "delete:1"}})
	if res.status != http.StatusOK {
		t.Fatalf("deleting inside a completed workout: status %d, body %s", res.status, res.body)
	}
	// But editing what is now deleted is refused, and named as such.
	res = h.send(request{method: http.MethodPatch, path: setPath(completed, setID), token: acc.accessToken,
		body: map[string]any{"weightKg": 66, "repetitions": 9, "rir": 2, "clientMutationId": "edit:2"}})
	if res.status != http.StatusConflict || res.str(t, "error", "code") != "set_deleted" {
		t.Fatalf("editing a deleted set: status %d, body %s", res.status, res.body)
	}

	// A cancelled session is a discarded one; its log is left alone.
	cancelled := h.createWorkout(acc.accessToken)
	otherSet := h.logSetReturning(acc.accessToken, cancelled, validSetBody("create:2"))
	h.mustTransition(acc.accessToken, cancelled, "cancelled")

	res = h.send(request{method: http.MethodPatch, path: setPath(cancelled, otherSet), token: acc.accessToken,
		body: map[string]any{"weightKg": 65, "repetitions": 9, "rir": 2, "clientMutationId": "edit:3"}})
	if res.status != http.StatusConflict || res.str(t, "error", "code") != "workout_not_editable" {
		t.Fatalf("editing a cancelled workout: status %d, body %s", res.status, res.body)
	}
	res = h.send(request{method: http.MethodDelete, path: setPath(cancelled, otherSet), token: acc.accessToken,
		body: map[string]any{"clientMutationId": "delete:2"}})
	if res.status != http.StatusConflict || res.str(t, "error", "code") != "workout_not_editable" {
		t.Fatalf("deleting inside a cancelled workout: status %d, body %s", res.status, res.body)
	}
}

// Removing the set in the middle must leave the others' numbers alone: the
// client's mutation ID is `workoutId:exerciseId:setNumber`, so a renumbering
// would make an already-spent ID name a different set.
func TestDeletingTheMiddleSetLeavesTheNumbersAlone(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")
	workoutID := h.createWorkout(acc.accessToken)

	setIDs := make([]string, 0, 3)
	for _, number := range []int{1, 2, 3} {
		body := validSetBody("bench:" + string(rune('0'+number)))
		body["exerciseId"] = "bench"
		body["setNumber"] = number
		setIDs = append(setIDs, h.logSetReturning(acc.accessToken, workoutID, body))
	}

	res := h.send(request{method: http.MethodDelete, path: setPath(workoutID, setIDs[1]), token: acc.accessToken,
		body: map[string]any{"clientMutationId": "delete:2"}})
	if res.status != http.StatusOK {
		t.Fatalf("delete middle: status %d, body %s", res.status, res.body)
	}

	list := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + workoutID + "/sets", token: acc.accessToken})
	items := list.json(t)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("listed %d sets, want 2: %s", len(items), list.body)
	}
	if items[0].(map[string]any)["setNumber"] != 1.0 || items[1].(map[string]any)["setNumber"] != 3.0 {
		t.Fatalf("numbers shifted: %s — a gap is correct, a renumbering is not", list.body)
	}

	// The client's next set continues past the gap and is accepted as new.
	fourth := validSetBody("bench:4")
	fourth["exerciseId"] = "bench"
	fourth["setNumber"] = 4
	if id := h.logSetReturning(acc.accessToken, workoutID, fourth); id == "" {
		t.Fatal("logging the next set after a gap must work")
	}
}

// A workout started offline with no name gets one later.
func TestRenamingAWorkout(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")
	intruder := h.register("intruder@example.com", "correct-horse-battery")

	created := h.send(request{method: http.MethodPost, path: basePath + "/workouts", token: acc.accessToken, body: map[string]any{}})
	if created.status != http.StatusCreated {
		t.Fatalf("create unnamed workout: status %d, body %s", created.status, created.body)
	}
	workoutID := created.str(t, "id")
	if created.str(t, "title") != "" {
		t.Fatalf("the workout should start unnamed: %s", created.body)
	}

	res := h.send(request{method: http.MethodPatch, path: basePath + "/workouts/" + workoutID, token: acc.accessToken,
		body: map[string]any{"title": "  Pull day  ", "clientMutationId": "rename:1"}})
	if res.status != http.StatusOK {
		t.Fatalf("rename: status %d, body %s", res.status, res.body)
	}
	if got := res.str(t, "title"); got != "Pull day" {
		t.Fatalf("title = %q, want the trimmed %q", got, "Pull day")
	}
	if res.str(t, "status") != "active" || res.json(t)["endedAt"] != nil {
		t.Fatalf("a rename must not touch the lifecycle: %s", res.body)
	}

	// A replay of the queued rename does not overwrite a newer title.
	replay := h.send(request{method: http.MethodPatch, path: basePath + "/workouts/" + workoutID, token: acc.accessToken,
		body: map[string]any{"title": "Something else", "clientMutationId": "rename:1"}})
	if replay.status != http.StatusOK || replay.str(t, "title") != "Pull day" {
		t.Fatalf("replayed rename: status %d, body %s", replay.status, replay.body)
	}

	// An unlabelled rename is last-write-wins, which is what a rename typed
	// into a connected client is.
	direct := h.send(request{method: http.MethodPatch, path: basePath + "/workouts/" + workoutID, token: acc.accessToken,
		body: map[string]any{"title": "Leg day"}})
	if direct.status != http.StatusOK || direct.str(t, "title") != "Leg day" {
		t.Fatalf("unlabelled rename: status %d, body %s", direct.status, direct.body)
	}

	// A title over the domain limit is refused, and a missing title too.
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	res = h.send(request{method: http.MethodPatch, path: basePath + "/workouts/" + workoutID, token: acc.accessToken,
		body: map[string]any{"title": string(long)}})
	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("over-long title: status %d, body %s", res.status, res.body)
	}
	res = h.send(request{method: http.MethodPatch, path: basePath + "/workouts/" + workoutID, token: acc.accessToken,
		body: map[string]any{}})
	if res.status != http.StatusUnprocessableEntity {
		t.Fatalf("missing title: status %d, body %s", res.status, res.body)
	}

	// Somebody else's workout answers exactly like one that does not exist.
	missing := h.send(request{method: http.MethodPatch, path: basePath + "/workouts/" + ids.NewUUID(),
		token: intruder.accessToken, body: map[string]any{"title": "Ghost"}})
	foreign := h.send(request{method: http.MethodPatch, path: basePath + "/workouts/" + workoutID,
		token: intruder.accessToken, body: map[string]any{"title": "Mine now"}})
	if foreign.status != http.StatusNotFound || string(foreign.body) != string(missing.body) {
		t.Fatalf("foreign rename %d %s vs missing %d %s", foreign.status, foreign.body, missing.status, missing.body)
	}

	// Unauthenticated it is a 401 and writes nothing.
	anon := h.send(request{method: http.MethodPatch, path: basePath + "/workouts/" + workoutID, body: map[string]any{"title": "Anon"}})
	if anon.status != http.StatusUnauthorized {
		t.Fatalf("anonymous rename: status %d, body %s", anon.status, anon.body)
	}
	final := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + workoutID, token: acc.accessToken})
	if final.str(t, "title") != "Leg day" {
		t.Fatalf("title ended as %s", final.body)
	}
}

// A clientMutationId is spent once and for all: it may not be recycled to aim a
// second, different change at another row.
func TestMutationIdCannotBeRecycledAcrossChanges(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")
	workoutID := h.createWorkout(acc.accessToken)

	first := h.logSetReturning(acc.accessToken, workoutID, validSetBody("create:1"))
	second := validSetBody("create:2")
	second["setNumber"] = 3
	secondID := h.logSetReturning(acc.accessToken, workoutID, second)

	edit := map[string]any{"weightKg": 70, "repetitions": 8, "rir": 1, "clientMutationId": "edit:1"}
	if res := h.send(request{method: http.MethodPatch, path: setPath(workoutID, first), token: acc.accessToken, body: edit}); res.status != http.StatusOK {
		t.Fatalf("first edit: status %d, body %s", res.status, res.body)
	}

	// Same ID, different set.
	res := h.send(request{method: http.MethodPatch, path: setPath(workoutID, secondID), token: acc.accessToken, body: edit})
	if res.status != http.StatusConflict || res.str(t, "error", "code") != "duplicate_client_mutation" {
		t.Fatalf("recycled mutation id: status %d, body %s", res.status, res.body)
	}

	// Same ID, same set, different kind of change.
	res = h.send(request{method: http.MethodDelete, path: setPath(workoutID, first), token: acc.accessToken,
		body: map[string]any{"clientMutationId": "edit:1"}})
	if res.status != http.StatusConflict || res.str(t, "error", "code") != "duplicate_client_mutation" {
		t.Fatalf("mutation id reused for a deletion: status %d, body %s", res.status, res.body)
	}

	// The second set was not touched by any of it.
	list := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + workoutID + "/sets", token: acc.accessToken})
	for _, item := range list.json(t)["items"].([]any) {
		set := item.(map[string]any)
		if set["id"] == secondID && set["weightKg"] != 62.5 {
			t.Fatalf("the recycled mutation id changed the second set: %v", set)
		}
	}
}

// A correction and a deletion are writes, and an anonymous request must not
// perform either.
func TestCorrectionsRequireAuthentication(t *testing.T) {
	h := newHarness(t, nil)
	acc := h.register("athlete@example.com", "correct-horse-battery")
	workoutID := h.createWorkout(acc.accessToken)
	setID := h.logSetReturning(acc.accessToken, workoutID, validSetBody("create:1"))

	tokens := map[string]string{
		"no token":      "",
		"garbage token": "not-a-jwt",
		"forged token":  "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJhdHRhY2tlciIsInR5cCI6ImFjY2VzcyIsImV4cCI6OTk5OTk5OTk5OX0.",
	}
	for name, token := range tokens {
		t.Run(name, func(t *testing.T) {
			edit := h.send(request{method: http.MethodPatch, path: setPath(workoutID, setID), token: token,
				body: map[string]any{"weightKg": 5, "repetitions": 1, "rir": 0, "clientMutationId": "anon:1"}})
			if edit.status != http.StatusUnauthorized {
				t.Fatalf("edit status = %d, want 401; body %s", edit.status, edit.body)
			}
			del := h.send(request{method: http.MethodDelete, path: setPath(workoutID, setID), token: token,
				body: map[string]any{"clientMutationId": "anon:2"}})
			if del.status != http.StatusUnauthorized {
				t.Fatalf("delete status = %d, want 401; body %s", del.status, del.body)
			}
		})
	}

	list := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + workoutID + "/sets", token: acc.accessToken})
	set := list.json(t)["items"].([]any)[0].(map[string]any)
	if set["weightKg"] != 62.5 || set["deletedAt"] != nil {
		t.Fatalf("an unauthenticated request changed something: %v", set)
	}
}
