package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// scrape reads the metrics page off the *separate* listener the server exposes,
// with whatever authorization the caller passes.
func (h *harness) scrape(token string) response {
	h.t.Helper()

	r := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.api.MetricsHandler().ServeHTTP(rec, r)
	return response{status: rec.Code, body: rec.Body.Bytes(), header: rec.Header().Clone()}
}

// The public port must not serve metrics at all: /metrics there is the same
// 404 as any other unknown path, so a misrouted proxy cannot expose it.
func TestMetricsAreNotOnThePublicPort(t *testing.T) {
	h := newHarness(t, map[string]string{"ATHLETICA_METRICS_TOKEN": "scrape-token"})

	for _, path := range []string{"/metrics", basePath + "/metrics"} {
		res := h.send(request{method: http.MethodGet, path: path})
		if res.status != http.StatusNotFound {
			t.Fatalf("%s on the public port: status %d, body %s", path, res.status, res.body)
		}
		if strings.Contains(string(res.body), "athletica_http_requests_total") {
			t.Fatalf("%s leaked the metrics page: %s", path, res.body)
		}
	}

	// A bearer token that is valid for the API is not a key to the metrics port.
	acc := h.register("athlete@example.com", "correct-horse-battery")
	res := h.send(request{method: http.MethodGet, path: "/metrics", token: acc.accessToken})
	if res.status != http.StatusNotFound {
		t.Fatalf("an authenticated /metrics on the public port: status %d, body %s", res.status, res.body)
	}
}

// With a token configured, the metrics listener refuses an anonymous scrape.
func TestMetricsRequireTheirToken(t *testing.T) {
	h := newHarness(t, map[string]string{"ATHLETICA_METRICS_TOKEN": "scrape-token"})

	anon := h.scrape("")
	if anon.status != http.StatusUnauthorized {
		t.Fatalf("anonymous scrape: status %d, body %s", anon.status, anon.body)
	}
	if strings.Contains(string(anon.body), "athletica_") {
		t.Fatalf("the refusal leaked metrics: %s", anon.body)
	}
	if got := anon.header.Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
		t.Fatalf("WWW-Authenticate = %q", got)
	}

	if wrong := h.scrape("not-the-token"); wrong.status != http.StatusUnauthorized {
		t.Fatalf("wrong token: status %d, body %s", wrong.status, wrong.body)
	}
	// An access token for the API is not a metrics token either.
	acc := h.register("athlete@example.com", "correct-horse-battery")
	if borrowed := h.scrape(acc.accessToken); borrowed.status != http.StatusUnauthorized {
		t.Fatalf("an API access token opened the metrics page: status %d", borrowed.status)
	}

	if ok := h.scrape("scrape-token"); ok.status != http.StatusOK {
		t.Fatalf("authorized scrape: status %d, body %s", ok.status, ok.body)
	}
}

// What the page reports, and — just as important — what it must never contain.
func TestMetricsCarryNoUserData(t *testing.T) {
	h := newHarness(t, map[string]string{
		"ATHLETICA_METRICS_TOKEN":   "scrape-token",
		"ATHLETICA_AUTH_RATE_LIMIT": "2",
	})

	acc := h.register("athlete@example.com", "correct-horse-battery")
	workoutID := h.createWorkout(acc.accessToken)
	setID := h.logSetReturning(acc.accessToken, workoutID, validSetBody("create:1"))
	h.send(request{method: http.MethodPatch, path: setPath(workoutID, setID), token: acc.accessToken,
		body: map[string]any{"weightKg": 70, "repetitions": 8, "rir": 1, "clientMutationId": "edit:1"}})
	h.send(request{method: http.MethodGet, path: basePath + "/workouts/" + workoutID, token: acc.accessToken})

	// Exhaust the login budget so the throttle counter has something to report.
	for range 5 {
		h.send(request{method: http.MethodPost, path: basePath + "/auth/login", ip: "203.0.113.7",
			body: map[string]any{"email": "athlete@example.com", "password": "wrong-password-here"}})
	}

	page := string(h.scrape("scrape-token").body)

	wantSeries := []string{
		`athletica_http_requests_total{route="/api/v1/workouts/{workoutId}/sets",method="POST",status="201"}`,
		`athletica_http_requests_total{route="/api/v1/workouts/{workoutId}/sets/{setId}",method="PATCH",status="200"}`,
		`athletica_http_requests_total{route="/api/v1/workouts/{workoutId}",method="GET",status="200"}`,
		`athletica_http_request_duration_seconds_count{route="/api/v1/workouts",method="POST"}`,
		`athletica_http_request_duration_seconds_bucket{route="/api/v1/workouts",method="POST",le="+Inf"}`,
		"athletica_rate_limited_total{",
		"athletica_build_info{version=",
	}
	for _, want := range wantSeries {
		if !strings.Contains(page, want) {
			t.Fatalf("metrics page is missing %s\n%s", want, page)
		}
	}

	// Nothing about the person, and no raw path: the route label is a template.
	forbidden := map[string]string{
		"the workout id":  workoutID,
		"the set id":      setID,
		"the user id":     acc.id,
		"the e-mail":      "athlete@example.com",
		"an access token": acc.accessToken,
		"an IP address":   "203.0.113.7",
	}
	for name, needle := range forbidden {
		if strings.Contains(page, needle) {
			t.Fatalf("the metrics page contains %s:\n%s", name, page)
		}
	}
	if strings.Contains(page, "/workouts/"+workoutID) {
		t.Fatalf("a raw request path reached a metric label:\n%s", page)
	}

	// The in-memory store has no pool and no schema, so those gauges are
	// absent rather than a misleading zero.
	if strings.Contains(page, "athletica_db_pool_") {
		t.Fatalf("pool gauges rendered without a database:\n%s", page)
	}
}

// Without a token the listener is loopback-only, which config enforces; the
// page itself then answers a local scrape.
func TestMetricsWithoutATokenStillServeLocally(t *testing.T) {
	h := newHarness(t, nil)
	if res := h.scrape(""); res.status != http.StatusOK {
		t.Fatalf("loopback scrape: status %d, body %s", res.status, res.body)
	}
	if res := h.scrape(""); !strings.Contains(res.header.Get("Content-Type"), "text/plain") {
		t.Fatalf("content type = %q", res.header.Get("Content-Type"))
	}
	// Anything but /metrics on that listener is a plain 404.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.api.MetricsHandler().ServeHTTP(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/ on the metrics listener: status %d", rec.Code)
	}
}
