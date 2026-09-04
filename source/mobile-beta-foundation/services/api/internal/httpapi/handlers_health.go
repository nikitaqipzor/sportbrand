package httpapi

import (
	"context"
	"net/http"
	"time"
)

const healthCheckTimeout = 2 * time.Second

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
	Version  string `json:"version,omitempty"`
	Time     string `json:"time"`
}

// handleHealth reports liveness of the process and reachability of the store.
// A store that cannot be reached yields 503 so orchestrators stop routing here.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancel()

	body := healthResponse{
		Status:   "ok",
		Database: "up",
		Version:  s.version,
		Time:     s.now().UTC().Format(time.RFC3339),
	}
	status := http.StatusOK

	if err := s.store.Ping(ctx); err != nil {
		body.Status = "degraded"
		body.Database = "down"
		status = http.StatusServiceUnavailable
		s.log.Error("health check failed", "request_id", RequestIDFrom(r.Context()), "error", err.Error())
	}
	writeJSON(w, s.log, status, body)
}
