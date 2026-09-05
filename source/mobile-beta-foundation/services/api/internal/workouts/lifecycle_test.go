package workouts_test

import (
	"testing"
	"time"

	"athletica.ai/api/internal/store"
	"athletica.ai/api/internal/workouts"
)

// TestTransitionTableIsComplete states the lifecycle once more, independently
// of the map the production code walks.
func TestTransitionTableIsComplete(t *testing.T) {
	want := map[string]map[string]bool{
		"active":    {"paused": true, "completed": true, "cancelled": true},
		"paused":    {"active": true, "completed": true, "cancelled": true},
		"completed": {},
		"cancelled": {},
	}

	statuses := workouts.AllStatuses()
	if len(statuses) != 4 {
		t.Fatalf("AllStatuses() = %v, want exactly the four domain statuses", statuses)
	}
	for _, from := range statuses {
		for _, to := range statuses {
			if got := workouts.TransitionAllowed(from, to); got != want[from][to] {
				t.Fatalf("%s -> %s allowed = %v, want %v", from, to, got, want[from][to])
			}
		}
	}
}

// TestCancelIsReachableFromEveryUnfinishedStatus is audit blocker QA-004
// expressed at the domain level.
func TestCancelIsReachableFromEveryUnfinishedStatus(t *testing.T) {
	for _, from := range []string{store.StatusActive, store.StatusPaused} {
		if !workouts.TransitionAllowed(from, store.StatusCancelled) {
			t.Fatalf("a %s workout cannot be cancelled — QA-004 is back", from)
		}
	}
	for _, from := range []string{store.StatusCompleted, store.StatusCancelled} {
		if !workouts.IsTerminal(from) {
			t.Fatalf("%s must be terminal", from)
		}
		for _, to := range workouts.AllStatuses() {
			if workouts.TransitionAllowed(from, to) {
				t.Fatalf("%s is terminal but still allows %s", from, to)
			}
		}
	}
}

// TestStatusesReachingMatchesTheTable: the allowed-from set handed to the store
// is derived from the very same table, so the two cannot drift.
func TestStatusesReachingMatchesTheTable(t *testing.T) {
	for _, to := range workouts.AllStatuses() {
		reaching := map[string]bool{}
		for _, from := range workouts.StatusesReaching(to) {
			reaching[from] = true
		}
		for _, from := range workouts.AllStatuses() {
			if workouts.TransitionAllowed(from, to) != reaching[from] {
				t.Fatalf("StatusesReaching(%s) disagrees with the table about %s", to, from)
			}
		}
	}
}

func TestParseStatuses(t *testing.T) {
	got, err := workouts.ParseStatuses([]string{"active, completed", "ACTIVE", ""})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 || got[0] != "active" || got[1] != "completed" {
		t.Fatalf("parsed %v, want [active completed] with the duplicate collapsed", got)
	}
	if _, err := workouts.ParseStatuses([]string{"finished"}); err == nil {
		t.Fatal("an unknown status must be refused, not silently dropped")
	}
	if got, err := workouts.ParseStatuses(nil); err != nil || len(got) != 0 {
		t.Fatalf("no filter = (%v, %v), want an empty list", got, err)
	}
}

// TestCursorRoundTrip: a cursor survives encoding, and nothing else decodes.
func TestCursorRoundTrip(t *testing.T) {
	want := store.WorkoutCursor{
		CreatedAt: time.Date(2026, 3, 4, 12, 0, 0, 123456789, time.UTC),
		ID:        "3f6c1b2a-0000-4000-8000-000000000000",
	}
	got, err := workouts.DecodeCursor(workouts.EncodeCursor(want))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || got.ID != want.ID {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}

	for _, bad := range []string{"", "!!!", "bm90LWEtY3Vyc29y", "MjAyNi0wMy0wNHw="} {
		if _, err := workouts.DecodeCursor(bad); err == nil {
			t.Fatalf("DecodeCursor(%q) succeeded, want ErrInvalidCursor", bad)
		}
	}
}
