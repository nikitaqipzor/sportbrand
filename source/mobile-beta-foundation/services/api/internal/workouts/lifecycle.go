package workouts

import (
	"errors"
	"sort"
	"strings"

	"athletica.ai/api/internal/store"
)

// Statuses of a workout, in the order the client shows them.
const (
	StatusPaused    = store.StatusPaused
	StatusCompleted = store.StatusCompleted
	StatusCancelled = store.StatusCancelled
)

// ErrUnknownStatus is returned when a caller asks for a status the domain does
// not define. It is a validation problem, never a transition conflict.
var ErrUnknownStatus = errors.New("workouts: unknown status")

// ErrInvalidTransition is returned when the requested transition exists in the
// domain but is not allowed from the workout's current status. The HTTP layer
// maps it to 409 — never to 500.
var ErrInvalidTransition = errors.New("workouts: transition is not allowed from the current status")

// transitions is the whole lifecycle, written out rather than implied:
//
//	active    → paused, completed, cancelled
//	paused    → active, completed, cancelled
//	completed → (terminal)
//	cancelled → (terminal)
//
// Cancelling is reachable from every non-terminal status, which is the fix for
// audit blocker QA-004: a started session must always be abandonable.
var transitions = map[string][]string{
	StatusActive:    {StatusPaused, StatusCompleted, StatusCancelled},
	StatusPaused:    {StatusActive, StatusCompleted, StatusCancelled},
	StatusCompleted: {},
	StatusCancelled: {},
}

// AllStatuses lists every status the contract accepts, sorted for stable
// error messages and documentation.
func AllStatuses() []string {
	out := make([]string, 0, len(transitions))
	for status := range transitions {
		out = append(out, status)
	}
	sort.Strings(out)
	return out
}

// IsStatus reports whether raw is one of the four domain statuses.
func IsStatus(raw string) bool {
	_, ok := transitions[raw]
	return ok
}

// IsTerminal reports whether a workout in this status can still change.
func IsTerminal(status string) bool {
	return status == StatusCompleted || status == StatusCancelled
}

// StatusesReaching returns every status a workout may legally be in for a
// transition to next to be allowed. The result is what the store is told to
// match, so the allowed-from set and the transition table cannot drift.
func StatusesReaching(next string) []string {
	var from []string
	for current, allowed := range transitions {
		for _, candidate := range allowed {
			if candidate == next {
				from = append(from, current)
			}
		}
	}
	sort.Strings(from)
	return from
}

// TransitionAllowed reports whether current → next is in the table.
func TransitionAllowed(current, next string) bool {
	for _, allowed := range transitions[current] {
		if allowed == next {
			return true
		}
	}
	return false
}

// ParseStatuses validates a filter value such as "active,paused" and returns
// the canonical list. An empty input means "no filter".
func ParseStatuses(values []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			status := strings.ToLower(strings.TrimSpace(raw))
			if status == "" {
				continue
			}
			if !IsStatus(status) {
				return nil, ErrUnknownStatus
			}
			if !seen[status] {
				seen[status] = true
				out = append(out, status)
			}
		}
	}
	return out, nil
}
