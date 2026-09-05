package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

// listPage is a decoded page of GET /workouts.
type listPage struct {
	ids        []string
	statuses   []string
	nextCursor string
	raw        response
}

func (h *harness) listWorkouts(token, query string) listPage {
	h.t.Helper()

	path := basePath + "/workouts"
	if query != "" {
		path += "?" + query
	}
	res := h.send(request{method: http.MethodGet, path: path, token: token})
	if res.status != http.StatusOK {
		h.t.Fatalf("list %q: status %d, body %s", query, res.status, res.body)
	}

	page := listPage{raw: res}
	body := res.json(h.t)
	items, _ := body["items"].([]any)
	for _, item := range items {
		row, _ := item.(map[string]any)
		id, _ := row["id"].(string)
		status, _ := row["status"].(string)
		page.ids = append(page.ids, id)
		page.statuses = append(page.statuses, status)
	}
	if next, ok := body["nextCursor"].(string); ok {
		page.nextCursor = next
	}
	return page
}

// seedWorkouts creates n workouts one minute apart so their order is stable.
func (h *harness) seedWorkouts(token string, n int) []string {
	h.t.Helper()
	ids := make([]string, 0, n)
	for range n {
		ids = append(ids, h.createWorkout(token))
		h.clock.Advance(time.Minute)
	}
	return ids
}

// TestWorkoutListIsNewestFirstAndUserScoped is the "Итоги" list contract.
func TestWorkoutListIsNewestFirstAndUserScoped(t *testing.T) {
	h := newHarness(t, nil)
	owner := h.register("owner@example.com", "correct-horse-battery")
	intruder := h.register("intruder@example.com", "correct-horse-battery")

	created := h.seedWorkouts(owner.accessToken, 3)
	intruderWorkout := h.createWorkout(intruder.accessToken)

	page := h.listWorkouts(owner.accessToken, "")
	if len(page.ids) != 3 {
		t.Fatalf("owner sees %d workouts, want 3: %s", len(page.ids), page.raw.body)
	}
	for i, id := range []string{created[2], created[1], created[0]} {
		if page.ids[i] != id {
			t.Fatalf("position %d is %s, want %s — the list must be newest first", i, page.ids[i], id)
		}
	}
	for _, id := range page.ids {
		if id == intruderWorkout {
			t.Fatal("another athlete's workout appeared in the list")
		}
	}
	if page.nextCursor != "" {
		t.Fatalf("nextCursor = %q on a complete page, want null", page.nextCursor)
	}
}

// TestWorkoutListPagesAtTheBoundary walks the cursor across an exact multiple
// of the page size: no row twice, no row missing, and the final page says so.
func TestWorkoutListPagesAtTheBoundary(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")
	created := h.seedWorkouts(user.accessToken, 4)

	seen := map[string]int{}
	cursor := ""
	pages := 0
	for {
		query := "limit=2"
		if cursor != "" {
			query += "&cursor=" + url.QueryEscape(cursor)
		}
		page := h.listWorkouts(user.accessToken, query)
		pages++
		if len(page.ids) > 2 {
			t.Fatalf("page %d holds %d rows, want at most the requested 2", pages, len(page.ids))
		}
		for _, id := range page.ids {
			seen[id]++
		}
		if page.nextCursor == "" {
			if len(page.ids) != 0 && pages != 2 {
				t.Fatalf("paging finished after %d pages of 4 rows at limit 2", pages)
			}
			break
		}
		cursor = page.nextCursor
		if pages > 6 {
			t.Fatal("the cursor never reported the end of the list")
		}
	}

	if len(seen) != len(created) {
		t.Fatalf("paging saw %d distinct workouts, want %d", len(seen), len(created))
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("workout %s came back %d times", id, count)
		}
	}
}

// TestWorkoutListCursorIsOpaqueAndValidated: a cursor the API did not issue is
// a 400, never a 500 and never a wider result set.
func TestWorkoutListCursorIsOpaqueAndValidated(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")
	h.seedWorkouts(user.accessToken, 2)

	for _, cursor := range []string{"not-base64!!", "bm90LWEtY3Vyc29y", "MjAyNi0wMy0wMXwx"} {
		res := h.send(request{method: http.MethodGet, path: basePath + "/workouts?cursor=" + url.QueryEscape(cursor), token: user.accessToken})
		if res.status != http.StatusBadRequest {
			t.Fatalf("cursor %q: status %d, body %s, want 400", cursor, res.status, res.body)
		}
		if code := res.str(t, "error", "code"); code != "invalid_cursor" {
			t.Fatalf("cursor %q: code %q, want invalid_cursor", cursor, code)
		}
	}
}

// TestWorkoutListCursorFromAnotherAccountStaysScoped: even a valid cursor
// stolen from another athlete cannot widen what the caller sees.
func TestWorkoutListCursorFromAnotherAccountStaysScoped(t *testing.T) {
	h := newHarness(t, nil)
	owner := h.register("owner@example.com", "correct-horse-battery")
	intruder := h.register("intruder@example.com", "correct-horse-battery")

	h.seedWorkouts(owner.accessToken, 3)
	stolen := h.listWorkouts(owner.accessToken, "limit=1").nextCursor
	if stolen == "" {
		t.Fatal("expected a cursor to steal")
	}
	h.seedWorkouts(intruder.accessToken, 1)

	page := h.listWorkouts(intruder.accessToken, "limit=10&cursor="+url.QueryEscape(stolen))
	for _, id := range page.ids {
		detail := h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + id, token: intruder.accessToken})
		if detail.status != http.StatusOK {
			t.Fatalf("the list returned %s, which the caller may not even read", id)
		}
	}
}

// TestWorkoutListFilters covers the status and date filters the client uses.
func TestWorkoutListFilters(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")

	first := h.createWorkout(user.accessToken)
	h.mustTransition(user.accessToken, first, "completed")
	h.clock.Advance(time.Minute)
	second := h.createWorkout(user.accessToken)
	h.mustTransition(user.accessToken, second, "cancelled")
	h.clock.Advance(time.Minute)
	third := h.createWorkout(user.accessToken)

	completed := h.listWorkouts(user.accessToken, "status=completed")
	if len(completed.ids) != 1 || completed.ids[0] != first {
		t.Fatalf("status=completed returned %v, want [%s]", completed.ids, first)
	}

	both := h.listWorkouts(user.accessToken, "status=completed,cancelled")
	if len(both.ids) != 2 {
		t.Fatalf("status=completed,cancelled returned %d rows, want 2", len(both.ids))
	}
	repeated := h.listWorkouts(user.accessToken, "status=completed&status=active")
	if len(repeated.ids) != 2 {
		t.Fatalf("repeated status params returned %d rows, want 2", len(repeated.ids))
	}
	for _, status := range repeated.statuses {
		if status != "completed" && status != "active" {
			t.Fatalf("filter leaked a %s workout", status)
		}
	}

	// Only the newest workout was created less than a minute ago.
	from := h.clock.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339)
	window := h.listWorkouts(user.accessToken, "from="+url.QueryEscape(from))
	if len(window.ids) != 1 || window.ids[0] != third {
		t.Fatalf("from=%s returned %v, want only the newest workout %s", from, window.ids, third)
	}

	to := h.clock.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339)
	older := h.listWorkouts(user.accessToken, "to="+url.QueryEscape(to))
	if len(older.ids) != 2 {
		t.Fatalf("to=%s returned %d rows, want the two older ones", to, len(older.ids))
	}

	bad := h.send(request{method: http.MethodGet, path: basePath + "/workouts?status=finished", token: user.accessToken})
	if bad.status != http.StatusBadRequest {
		t.Fatalf("unknown status filter = %d, want 400", bad.status)
	}
	badLimit := h.send(request{method: http.MethodGet, path: basePath + "/workouts?limit=1000", token: user.accessToken})
	if badLimit.status != http.StatusBadRequest {
		t.Fatalf("limit=1000 = %d, want 400", badLimit.status)
	}
	badDate := h.send(request{method: http.MethodGet, path: basePath + "/workouts?from=yesterday", token: user.accessToken})
	if badDate.status != http.StatusBadRequest {
		t.Fatalf("from=yesterday = %d, want 400", badDate.status)
	}
}

// TestWorkoutListRequiresAuthentication states the obvious guard.
func TestWorkoutListRequiresAuthentication(t *testing.T) {
	h := newHarness(t, nil)
	res := h.send(request{method: http.MethodGet, path: basePath + "/workouts"})
	if res.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.status)
	}
}
