package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"athletica.ai/api/internal/workouts"
)

// maxRequestBody caps every request body; the API only accepts small documents.
const maxRequestBody = 64 << 10

// Error codes returned to clients. They are part of the published contract.
const (
	codeInvalidRequest    = "invalid_request"
	codeValidationFailed  = "validation_failed"
	codeUnauthorized      = "unauthorized"
	codeInvalidCreds      = "invalid_credentials"
	codeEmailTaken        = "email_taken"
	codeNotFound          = "not_found"
	codeDuplicateMutation = "duplicate_client_mutation"
	codeInvalidTransition = "invalid_transition"
	codeInvalidCursor     = "invalid_cursor"
	codeWorkoutIDTaken    = "workout_id_taken"
	codeRateLimited       = "rate_limited"
	codeInternal          = "internal_error"
	codeUnavailable       = "service_unavailable"
)

// errorBody is the single error envelope used by every endpoint.
type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    string           `json:"code"`
	Message string           `json:"message"`
	Details []workouts.Issue `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, log *slog.Logger, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil && log != nil {
		log.Warn("write response failed", "error", err.Error())
	}
}

func writeError(w http.ResponseWriter, log *slog.Logger, status int, code, message string) {
	writeJSON(w, log, status, errorBody{Error: errorPayload{Code: code, Message: message}})
}

func writeValidationError(w http.ResponseWriter, log *slog.Logger, verr *workouts.ValidationError) {
	writeJSON(w, log, http.StatusUnprocessableEntity, errorBody{Error: errorPayload{
		Code:    codeValidationFailed,
		Message: "request payload failed domain validation",
		Details: verr.Issues,
	}})
}

func writeRateLimited(w http.ResponseWriter, log *slog.Logger, retryAfter time.Duration) {
	seconds := int(retryAfter.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeError(w, log, http.StatusTooManyRequests, codeRateLimited,
		"too many attempts, retry in "+strconv.Itoa(seconds)+"s")
}

// decodeJSON reads a bounded JSON body into dst.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	// Reject trailing content so "{}{}" is not silently accepted.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("body must contain exactly one JSON object")
	}
	return nil
}
