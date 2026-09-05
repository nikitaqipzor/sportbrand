// Package metrics is the service's observability surface: a tiny Prometheus
// text-format registry with no dependencies beyond the standard library.
//
// Two rules shape what may be recorded here:
//
//  1. **Nothing about a person.** No e-mail, no user ID, no workout or set ID,
//     no raw request path (which carries UUIDs), no token, no IP. Every label
//     value in this file comes from a fixed, compile-time set — the registered
//     route templates, the HTTP methods, the status codes, the throttling
//     scopes — so the series count is bounded and a label can never be steered
//     by a request.
//  2. **Never on the public port.** The registry only renders; who may read it
//     is decided by the metrics listener in package httpapi.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// durationBuckets are the cumulative histogram bounds, in seconds. They span a
// fast in-memory read to a request that hit the shutdown timeout.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// PoolStats is the database pool state the /metrics page reports. It is a plain
// struct rather than the pgx type so the memory store — and the tests — need no
// database to satisfy the interface.
type PoolStats struct {
	MaxConns          int32
	TotalConns        int32
	AcquiredConns     int32
	IdleConns         int32
	ConstructingConns int32
	AcquireCount      int64
	// EmptyAcquireCount is how often a caller had to wait for a free
	// connection: the number that turns "the pool is fine" into a fact.
	EmptyAcquireCount int64
	CanceledAcquire   int64
}

type routeKey struct {
	route  string
	method string
	status int
}

type methodKey struct {
	route  string
	method string
}

type histogram struct {
	counts []uint64 // one per bucket bound, cumulative at render time
	sum    float64
	count  uint64
}

func (h *histogram) observe(seconds float64) {
	h.sum += seconds
	h.count++
	for i, bound := range durationBuckets {
		if seconds <= bound {
			h.counts[i]++
			return
		}
	}
}

// Registry collects the service's metrics. The zero value is not usable; call
// New.
type Registry struct {
	mu sync.Mutex

	version  string
	requests map[routeKey]uint64
	duration map[methodKey]*histogram
	// throttled counts rate-limit rejections by scope (ip|account) and reason
	// (rate|backoff). Never by key: a key is an IP or an account.
	throttled map[[2]string]uint64

	migrationsPending int
	migrationsApplied int
	migrationsKnown   bool

	poolSource func() PoolStats
}

// New returns an empty registry labelled with the build version.
func New(version string) *Registry {
	if strings.TrimSpace(version) == "" {
		version = "dev"
	}
	return &Registry{
		version:   version,
		requests:  map[routeKey]uint64{},
		duration:  map[methodKey]*histogram{},
		throttled: map[[2]string]uint64{},
	}
}

// ObserveRequest records one served request.
//
// route must be a registered route *template* such as
// "/api/v1/workouts/{workoutId}/sets" — never r.URL.Path, which carries the
// caller's workout and set IDs.
func (r *Registry) ObserveRequest(route, method string, status int, d time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.requests[routeKey{route: route, method: method, status: status}]++

	key := methodKey{route: route, method: method}
	h, ok := r.duration[key]
	if !ok {
		h = &histogram{counts: make([]uint64, len(durationBuckets))}
		r.duration[key] = h
	}
	h.observe(d.Seconds())
}

// Throttled records one rate-limit rejection. scope is "ip" or "account";
// reason is the limiter's own "rate" or "backoff".
func (r *Registry) Throttled(scope, reason string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.throttled[[2]string{scope, reason}]++
}

// SetMigrationQueue records the migration backlog observed at start-up:
// how many were still pending and how many this process applied.
func (r *Registry) SetMigrationQueue(pending, applied int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.migrationsPending, r.migrationsApplied, r.migrationsKnown = pending, applied, true
}

// SetPoolSource installs a callback that reports the database pool state. It is
// absent for the in-memory store, and the pool gauges are then simply not
// rendered rather than reported as zero.
func (r *Registry) SetPoolSource(source func() PoolStats) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.poolSource = source
}

// Render writes the registry in the Prometheus text exposition format.
func (r *Registry) Render(w io.Writer) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	version := r.version
	requests := make(map[routeKey]uint64, len(r.requests))
	for k, v := range r.requests {
		requests[k] = v
	}
	duration := make(map[methodKey]histogram, len(r.duration))
	for k, v := range r.duration {
		snapshot := histogram{counts: append([]uint64(nil), v.counts...), sum: v.sum, count: v.count}
		duration[k] = snapshot
	}
	throttled := make(map[[2]string]uint64, len(r.throttled))
	for k, v := range r.throttled {
		throttled[k] = v
	}
	migrationsPending, migrationsApplied, migrationsKnown := r.migrationsPending, r.migrationsApplied, r.migrationsKnown
	poolSource := r.poolSource
	r.mu.Unlock()

	var b strings.Builder

	b.WriteString("# HELP athletica_build_info Build version of the running service.\n")
	b.WriteString("# TYPE athletica_build_info gauge\n")
	fmt.Fprintf(&b, "athletica_build_info{version=%s} 1\n", quote(version))

	b.WriteString("# HELP athletica_http_requests_total Requests served, by route template, method and status.\n")
	b.WriteString("# TYPE athletica_http_requests_total counter\n")
	keys := make([]routeKey, 0, len(requests))
	for k := range requests {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].status < keys[j].status
	})
	for _, k := range keys {
		fmt.Fprintf(&b, "athletica_http_requests_total{route=%s,method=%s,status=%s} %d\n",
			quote(k.route), quote(k.method), quote(strconv.Itoa(k.status)), requests[k])
	}

	b.WriteString("# HELP athletica_http_request_duration_seconds Request latency by route template and method.\n")
	b.WriteString("# TYPE athletica_http_request_duration_seconds histogram\n")
	mkeys := make([]methodKey, 0, len(duration))
	for k := range duration {
		mkeys = append(mkeys, k)
	}
	sort.Slice(mkeys, func(i, j int) bool {
		if mkeys[i].route != mkeys[j].route {
			return mkeys[i].route < mkeys[j].route
		}
		return mkeys[i].method < mkeys[j].method
	})
	for _, k := range mkeys {
		h := duration[k]
		var cumulative uint64
		for i, bound := range durationBuckets {
			cumulative += h.counts[i]
			fmt.Fprintf(&b, "athletica_http_request_duration_seconds_bucket{route=%s,method=%s,le=%s} %d\n",
				quote(k.route), quote(k.method), quote(formatFloat(bound)), cumulative)
		}
		fmt.Fprintf(&b, "athletica_http_request_duration_seconds_bucket{route=%s,method=%s,le=\"+Inf\"} %d\n",
			quote(k.route), quote(k.method), h.count)
		fmt.Fprintf(&b, "athletica_http_request_duration_seconds_sum{route=%s,method=%s} %s\n",
			quote(k.route), quote(k.method), formatFloat(h.sum))
		fmt.Fprintf(&b, "athletica_http_request_duration_seconds_count{route=%s,method=%s} %d\n",
			quote(k.route), quote(k.method), h.count)
	}

	b.WriteString("# HELP athletica_rate_limited_total Requests refused by the auth throttle, by scope and reason.\n")
	b.WriteString("# TYPE athletica_rate_limited_total counter\n")
	tkeys := make([][2]string, 0, len(throttled))
	for k := range throttled {
		tkeys = append(tkeys, k)
	}
	sort.Slice(tkeys, func(i, j int) bool {
		if tkeys[i][0] != tkeys[j][0] {
			return tkeys[i][0] < tkeys[j][0]
		}
		return tkeys[i][1] < tkeys[j][1]
	})
	for _, k := range tkeys {
		fmt.Fprintf(&b, "athletica_rate_limited_total{scope=%s,reason=%s} %d\n", quote(k[0]), quote(k[1]), throttled[k])
	}

	if migrationsKnown {
		b.WriteString("# HELP athletica_migrations_pending Migrations still unapplied after start-up; 0 means the schema is current.\n")
		b.WriteString("# TYPE athletica_migrations_pending gauge\n")
		fmt.Fprintf(&b, "athletica_migrations_pending %d\n", migrationsPending)
		b.WriteString("# HELP athletica_migrations_applied_total Migrations this process applied at start-up.\n")
		b.WriteString("# TYPE athletica_migrations_applied_total gauge\n")
		fmt.Fprintf(&b, "athletica_migrations_applied_total %d\n", migrationsApplied)
	}

	if poolSource != nil {
		stats := poolSource()
		b.WriteString("# HELP athletica_db_pool_connections Database pool connections by state.\n")
		b.WriteString("# TYPE athletica_db_pool_connections gauge\n")
		fmt.Fprintf(&b, "athletica_db_pool_connections{state=\"acquired\"} %d\n", stats.AcquiredConns)
		fmt.Fprintf(&b, "athletica_db_pool_connections{state=\"idle\"} %d\n", stats.IdleConns)
		fmt.Fprintf(&b, "athletica_db_pool_connections{state=\"constructing\"} %d\n", stats.ConstructingConns)
		fmt.Fprintf(&b, "athletica_db_pool_connections{state=\"total\"} %d\n", stats.TotalConns)
		b.WriteString("# HELP athletica_db_pool_max_connections Configured pool ceiling.\n")
		b.WriteString("# TYPE athletica_db_pool_max_connections gauge\n")
		fmt.Fprintf(&b, "athletica_db_pool_max_connections %d\n", stats.MaxConns)
		b.WriteString("# HELP athletica_db_pool_acquires_total Connection acquisitions since start.\n")
		b.WriteString("# TYPE athletica_db_pool_acquires_total counter\n")
		fmt.Fprintf(&b, "athletica_db_pool_acquires_total %d\n", stats.AcquireCount)
		b.WriteString("# HELP athletica_db_pool_empty_acquires_total Acquisitions that had to wait for a free connection.\n")
		b.WriteString("# TYPE athletica_db_pool_empty_acquires_total counter\n")
		fmt.Fprintf(&b, "athletica_db_pool_empty_acquires_total %d\n", stats.EmptyAcquireCount)
		b.WriteString("# HELP athletica_db_pool_canceled_acquires_total Acquisitions abandoned because the request context ended.\n")
		b.WriteString("# TYPE athletica_db_pool_canceled_acquires_total counter\n")
		fmt.Fprintf(&b, "athletica_db_pool_canceled_acquires_total %d\n", stats.CanceledAcquire)
	}

	_, err := io.WriteString(w, b.String())
	return err
}

// quote renders a label value. Only the fixed vocabulary above ever reaches it,
// but it escapes anyway so a future label cannot break the format.
func quote(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(v) + `"`
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
