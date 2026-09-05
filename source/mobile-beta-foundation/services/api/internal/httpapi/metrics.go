package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"athletica.ai/api/internal/metrics"
)

// Observability lives on its own listener.
//
// The public router has no /metrics route at all — asking for it there gets the
// same 404 as any other unknown path — because an operational page must not be
// one misconfiguration away from the internet. The metrics listener binds
// loopback by default, and config refuses to start when it is bound wider
// without a bearer token.

// Metrics returns the registry the server records into, so main can attach the
// database pool and the migration backlog to it.
func (s *Server) Metrics() *metrics.Registry { return s.metrics }

// MetricsHandler serves the Prometheus text page for the separate listener.
// It is never mounted on the public mux.
func (s *Server) MetricsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, s.log, http.StatusNotFound, codeNotFound, "no route matches "+r.Method+" "+r.URL.Path)
	})
	return mux
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.metricsAuthorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="athletica-metrics"`)
		writeError(w, s.log, http.StatusUnauthorized, codeUnauthorized, "metrics require a bearer token")
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if err := s.metrics.Render(w); err != nil {
		s.log.Warn("write metrics failed", "error", err.Error())
	}
}

// metricsAuthorized checks the metrics bearer token in constant time. With no
// token configured the listener is loopback-only (config enforces it), so the
// host boundary is the control.
func (s *Server) metricsAuthorized(r *http.Request) bool {
	want := strings.TrimSpace(s.cfg.MetricsToken)
	if want == "" {
		return true
	}
	got, ok := bearerToken(r)
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// instrument records one served request against a *route template*, not the
// request path: "/api/v1/workouts/{workoutId}/sets" and never the URL that
// carries the caller's workout ID. The label vocabulary is therefore the fixed
// set registered in routes(), and no request can invent a new series.
func (s *Server) instrument(route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		s.metrics.ObserveRequest(route, r.Method, rec.status, s.now().Sub(start))
	})
}

// routeTemplate strips the leading method from a ServeMux pattern such as
// "POST /api/v1/workouts/{workoutId}/sets"; the method becomes its own label.
func routeTemplate(pattern string) string {
	if _, path, found := strings.Cut(pattern, " "); found {
		return path
	}
	return pattern
}
