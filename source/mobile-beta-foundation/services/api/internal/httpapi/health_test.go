package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"athletica.ai/api/internal/store"
	"athletica.ai/api/internal/store/memory"
)

type brokenPingStore struct {
	*memory.Store
}

func (brokenPingStore) Ping(context.Context) error { return errors.New("connection refused") }

func TestHealthReportsOK(t *testing.T) {
	h := newHarness(t, nil)

	for _, path := range []string{basePath + "/health", "/health"} {
		res := h.send(request{method: http.MethodGet, path: path})
		if res.status != http.StatusOK {
			t.Fatalf("GET %s: status %d, body %s", path, res.status, res.body)
		}
		if got := res.str(t, "status"); got != "ok" {
			t.Fatalf("GET %s: status field = %q", path, got)
		}
		if got := res.str(t, "database"); got != "up" {
			t.Fatalf("GET %s: database field = %q", path, got)
		}
	}
}

// A service that cannot reach its database must not claim to be healthy.
func TestHealthReportsDatabaseOutage(t *testing.T) {
	h := newHarnessWithStore(t, func(m *memory.Store) store.Store { return brokenPingStore{m} }, nil)

	res := h.send(request{method: http.MethodGet, path: basePath + "/health"})
	if res.status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", res.status, res.body)
	}
	if got := res.str(t, "database"); got != "down" {
		t.Fatalf("database field = %q, want down", got)
	}
}

func TestUnknownRouteReturnsJSONError(t *testing.T) {
	h := newHarness(t, nil)

	res := h.send(request{method: http.MethodGet, path: basePath + "/nutrition"})
	if res.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.status)
	}
	if got := res.str(t, "error", "code"); got != "not_found" {
		t.Fatalf("error code = %q", got)
	}
}

// The mobile client is pinned to /api/v1; the unversioned path must not answer.
func TestBasePathIsRequired(t *testing.T) {
	h := newHarness(t, nil)

	res := h.send(request{method: http.MethodPost, path: "/auth/login", body: map[string]any{"email": "a@b.com", "password": "0123456789"}})
	if res.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unversioned auth path", res.status)
	}
}
