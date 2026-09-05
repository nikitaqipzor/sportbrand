package httpapi_test

import (
	"net/http"
	"testing"
	"time"
)

func (h *harness) progress(token, query string) response {
	h.t.Helper()
	path := basePath + "/progress"
	if query != "" {
		path += "?" + query
	}
	res := h.send(request{method: http.MethodGet, path: path, token: token})
	if res.status != http.StatusOK {
		h.t.Fatalf("progress %q: status %d, body %s", query, res.status, res.body)
	}
	return res
}

func strengthRows(t *testing.T, res response) []map[string]any {
	t.Helper()
	raw, _ := res.json(t)["strength"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, row := range raw {
		m, _ := row.(map[string]any)
		out = append(out, m)
	}
	return out
}

// TestProgressReportsStrengthVolumeAndAdherence pins the shape and the
// arithmetic the "Прогресс" screen is built on.
func TestProgressReportsStrengthVolumeAndAdherence(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")

	done := h.createWorkout(user.accessToken)
	h.logSet(user.accessToken, done, "squat", 100, 5, "m1") // Epley 116.67
	h.logSet(user.accessToken, done, "squat", 90, 10, "m2") // Epley 120.00
	h.logSet(user.accessToken, done, "bench", 60, 8, "m3")  // Epley 76.00
	h.mustTransition(user.accessToken, done, "completed")

	abandoned := h.createWorkout(user.accessToken)
	h.logSet(user.accessToken, abandoned, "squat", 300, 10, "m4")
	h.mustTransition(user.accessToken, abandoned, "cancelled")

	res := h.progress(user.accessToken, "")
	rows := strengthRows(t, res)
	if len(rows) != 2 {
		t.Fatalf("strength covers %d exercises, want squat and bench: %s", len(rows), res.body)
	}
	if rows[0]["exerciseId"] != "squat" {
		t.Fatalf("strength is led by %v, want the strongest lift first", rows[0]["exerciseId"])
	}

	squat := rows[0]
	best, _ := squat["bestWeight"].(map[string]any)
	if best["weightKg"] != float64(100) || best["repetitions"] != float64(5) {
		t.Fatalf("heaviest squat = %v, want 100 kg x 5 (the cancelled 300 kg must not count)", best)
	}
	e1rm, _ := squat["bestEstimated1Rm"].(map[string]any)
	if e1rm["estimated1RmKg"] != float64(120) || e1rm["weightKg"] != float64(90) {
		t.Fatalf("best estimated 1RM = %v, want 120 kg from 90 kg x 10", e1rm)
	}
	if squat["sets"] != float64(2) || squat["volumeKg"] != float64(100*5+90*10) {
		t.Fatalf("squat totals = %v sets / %v kg, want 2 / 1400", squat["sets"], squat["volumeKg"])
	}

	weeks, _ := res.json(t)["weeklyVolume"].([]any)
	if len(weeks) != 1 {
		t.Fatalf("weeklyVolume covers %d weeks, want 1: %s", len(weeks), res.body)
	}
	week, _ := weeks[0].(map[string]any)
	if week["sets"] != float64(3) || week["workouts"] != float64(1) {
		t.Fatalf("week = %v, want 3 sets in 1 workout (the cancelled one is excluded)", week)
	}
	if start, _ := week["weekStart"].(string); start != "2026-02-23T00:00:00Z" {
		t.Fatalf("weekStart = %q, want the Monday 2026-02-23T00:00:00Z", start)
	}

	adherence, _ := res.json(t)["adherence"].(map[string]any)
	totals, _ := adherence["totals"].(map[string]any)
	if totals["started"] != float64(2) || totals["completed"] != float64(1) || totals["cancelled"] != float64(1) {
		t.Fatalf("adherence totals = %v, want 2 started / 1 completed / 1 cancelled", totals)
	}
	if totals["completionRate"] != 0.5 {
		t.Fatalf("completionRate = %v, want 0.5", totals["completionRate"])
	}
	if totals["weeksWithTraining"] != float64(1) {
		t.Fatalf("weeksWithTraining = %v, want 1", totals["weeksWithTraining"])
	}
}

// TestProgressIsUserScoped is the isolation rule on the aggregate path: one
// athlete's records must never appear in another's report.
func TestProgressIsUserScoped(t *testing.T) {
	h := newHarness(t, nil)
	strong := h.register("strong@example.com", "correct-horse-battery")
	novice := h.register("novice@example.com", "correct-horse-battery")

	strongWorkout := h.createWorkout(strong.accessToken)
	h.logSet(strong.accessToken, strongWorkout, "squat", 250, 5, "s1")
	noviceWorkout := h.createWorkout(novice.accessToken)
	h.logSet(novice.accessToken, noviceWorkout, "squat", 60, 5, "n1")

	rows := strengthRows(t, h.progress(novice.accessToken, ""))
	if len(rows) != 1 {
		t.Fatalf("the novice sees %d exercises, want 1", len(rows))
	}
	best, _ := rows[0]["bestWeight"].(map[string]any)
	if best["weightKg"] != float64(60) {
		t.Fatalf("the novice's record is %v kg — another athlete's set leaked in", best["weightKg"])
	}

	adherence, _ := h.progress(novice.accessToken, "").json(t)["adherence"].(map[string]any)
	totals, _ := adherence["totals"].(map[string]any)
	if totals["started"] != float64(1) {
		t.Fatalf("the novice started %v workouts, want 1", totals["started"])
	}
}

// TestProgressEmptyHistoryIsAnEmptyReport: a brand new account must render,
// not fail.
func TestProgressEmptyHistoryIsAnEmptyReport(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")

	res := h.progress(user.accessToken, "")
	body := res.json(t)
	if rows, _ := body["strength"].([]any); len(rows) != 0 {
		t.Fatalf("strength = %v, want an empty array", rows)
	}
	if weeks, _ := body["weeklyVolume"].([]any); len(weeks) != 0 {
		t.Fatalf("weeklyVolume = %v, want an empty array", weeks)
	}
	adherence, _ := body["adherence"].(map[string]any)
	totals, _ := adherence["totals"].(map[string]any)
	if totals["completionRate"] != float64(0) {
		t.Fatalf("completionRate = %v on an empty history, want 0", totals["completionRate"])
	}
	window, _ := body["window"].(map[string]any)
	if window["from"] == nil || window["to"] == nil {
		t.Fatalf("the report must always name its window, got %v", window)
	}
}

// TestProgressWindowIsBoundedAndValidated: the window is snapped to whole ISO
// weeks and clamped, so no query can ask the database for an unbounded scan.
func TestProgressWindowIsBoundedAndValidated(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")

	res := h.progress(user.accessToken, "from=2000-01-01&to=2026-03-01")
	window, _ := res.json(t)["window"].(map[string]any)
	from, _ := window["from"].(string)
	to, _ := window["to"].(string)

	parsedFrom, err := time.Parse(time.RFC3339, from)
	if err != nil {
		t.Fatalf("window.from = %q: %v", from, err)
	}
	parsedTo, err := time.Parse(time.RFC3339, to)
	if err != nil {
		t.Fatalf("window.to = %q: %v", to, err)
	}
	if weeks := parsedTo.Sub(parsedFrom).Hours() / (24 * 7); weeks > 104 {
		t.Fatalf("window spans %.0f weeks, want it clamped to 104", weeks)
	}
	if parsedFrom.Weekday() != time.Monday || parsedTo.Weekday() != time.Monday {
		t.Fatalf("window %s..%s is not snapped to ISO weeks", from, to)
	}

	for _, query := range []string{"from=yesterday", "to=03/01/2026", "exerciseLimit=lots", "exerciseLimit=-1"} {
		got := h.send(request{method: http.MethodGet, path: basePath + "/progress?" + query, token: user.accessToken})
		if got.status != http.StatusBadRequest {
			t.Fatalf("%s = %d, body %s, want 400", query, got.status, got.body)
		}
	}
}

// TestProgressRequiresAuthentication states the guard.
func TestProgressRequiresAuthentication(t *testing.T) {
	h := newHarness(t, nil)
	res := h.send(request{method: http.MethodGet, path: basePath + "/progress"})
	if res.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.status)
	}
}
