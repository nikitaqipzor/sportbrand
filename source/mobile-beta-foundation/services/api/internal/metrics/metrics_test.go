package metrics_test

import (
	"strings"
	"testing"
	"time"

	"athletica.ai/api/internal/metrics"
)

func render(t *testing.T, r *metrics.Registry) string {
	t.Helper()
	var b strings.Builder
	if err := r.Render(&b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// The histogram must be cumulative and its counts must agree with the total,
// or a latency panel reads as nonsense.
func TestHistogramIsCumulative(t *testing.T) {
	r := metrics.New("1.2.3")
	for _, d := range []time.Duration{time.Millisecond, 3 * time.Millisecond, 40 * time.Millisecond, 20 * time.Second} {
		r.ObserveRequest("/api/v1/workouts", "GET", 200, d)
	}
	page := render(t, r)

	want := []string{
		`athletica_build_info{version="1.2.3"} 1`,
		`athletica_http_requests_total{route="/api/v1/workouts",method="GET",status="200"} 4`,
		`athletica_http_request_duration_seconds_bucket{route="/api/v1/workouts",method="GET",le="0.005"} 2`,
		`athletica_http_request_duration_seconds_bucket{route="/api/v1/workouts",method="GET",le="0.05"} 3`,
		`athletica_http_request_duration_seconds_bucket{route="/api/v1/workouts",method="GET",le="10"} 3`,
		`athletica_http_request_duration_seconds_bucket{route="/api/v1/workouts",method="GET",le="+Inf"} 4`,
		`athletica_http_request_duration_seconds_count{route="/api/v1/workouts",method="GET"} 4`,
	}
	for _, line := range want {
		if !strings.Contains(page, line) {
			t.Fatalf("missing %q in\n%s", line, page)
		}
	}
}

// Gauges that describe a store the process does not have must be absent rather
// than a misleading zero.
func TestPoolAndMigrationGaugesAreOptional(t *testing.T) {
	r := metrics.New("dev")
	page := render(t, r)
	if strings.Contains(page, "athletica_db_pool_") || strings.Contains(page, "athletica_migrations_") {
		t.Fatalf("optional gauges rendered without a source:\n%s", page)
	}

	r.SetMigrationQueue(2, 1)
	r.SetPoolSource(func() metrics.PoolStats {
		return metrics.PoolStats{MaxConns: 8, TotalConns: 3, AcquiredConns: 1, IdleConns: 2, AcquireCount: 17}
	})
	page = render(t, r)
	for _, line := range []string{
		"athletica_migrations_pending 2",
		"athletica_migrations_applied_total 1",
		`athletica_db_pool_connections{state="acquired"} 1`,
		`athletica_db_pool_connections{state="idle"} 2`,
		"athletica_db_pool_max_connections 8",
		"athletica_db_pool_acquires_total 17",
	} {
		if !strings.Contains(page, line) {
			t.Fatalf("missing %q in\n%s", line, page)
		}
	}
}

// A label value can only ever come from the fixed route vocabulary, but it is
// escaped anyway so a future label cannot break the exposition format.
func TestLabelValuesAreEscaped(t *testing.T) {
	r := metrics.New(`weird "version"` + "\n")
	page := render(t, r)
	if strings.Count(page, "athletica_build_info") != 3 { // HELP, TYPE, sample
		t.Fatalf("build info was not a single well-formed sample:\n%s", page)
	}
	if !strings.Contains(page, `athletica_build_info{version="weird \"version\"\n"} 1`) {
		t.Fatalf("label was not escaped:\n%s", page)
	}
}

// The registry is written from every request goroutine and read by a scrape.
func TestConcurrentUseIsSafe(t *testing.T) {
	r := metrics.New("dev")
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			r.ObserveRequest("/api/v1/health", "GET", 200, time.Millisecond)
			r.Throttled("ip", "rate")
		}
	}()
	for range 50 {
		render(t, r)
	}
	<-done
	if !strings.Contains(render(t, r), `athletica_rate_limited_total{scope="ip",reason="rate"} 200`) {
		t.Fatal("throttle counter lost writes")
	}
}
