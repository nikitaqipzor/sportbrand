package httpapi

import (
	"errors"
	"net/http"
	"time"

	"athletica.ai/api/internal/store"
	"athletica.ai/api/internal/workouts"
)

type createWorkoutRequest struct {
	Title *string `json:"title"`
}

type workoutResponse struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

// logSetRequest is the wire form of POST /workouts/{workoutId}/sets.
//
// Note what is *absent*: there is no userId field. Even if a client sends one
// it is ignored, because the owner is taken from the access token subject.
type logSetRequest struct {
	ExerciseID       *string  `json:"exerciseId"`
	SetNumber        *int     `json:"setNumber"`
	WeightKg         *float64 `json:"weightKg"`
	Repetitions      *int     `json:"repetitions"`
	RIR              *int     `json:"rir"`
	ClientMutationID *string  `json:"clientMutationId"`
}

type setResponse struct {
	ID               string  `json:"id"`
	WorkoutID        string  `json:"workoutId"`
	ExerciseID       string  `json:"exerciseId"`
	SetNumber        int     `json:"setNumber"`
	WeightKg         float64 `json:"weightKg"`
	Repetitions      int     `json:"repetitions"`
	RIR              int     `json:"rir"`
	ClientMutationID string  `json:"clientMutationId"`
	CreatedAt        string  `json:"createdAt"`
}

type duplicateSetResponse struct {
	Error errorPayload `json:"error"`
	Set   setResponse  `json:"set"`
}

type setListResponse struct {
	Items []setResponse `json:"items"`
}

func toSetResponse(s store.WorkoutSet) setResponse {
	return setResponse{
		ID:               s.ID,
		WorkoutID:        s.WorkoutID,
		ExerciseID:       s.ExerciseID,
		SetNumber:        s.SetNumber,
		WeightKg:         s.WeightKg,
		Repetitions:      s.Repetitions,
		RIR:              s.RIR,
		ClientMutationID: s.ClientMutationID,
		CreatedAt:        s.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// handleCreateWorkout starts a session for the authenticated user.
func (s *Server) handleCreateWorkout(w http.ResponseWriter, r *http.Request, user store.User) {
	var req createWorkoutRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest, "request body must be a JSON object")
			return
		}
	}

	workout, err := s.workouts.CreateWorkout(r.Context(), user.ID, deref(req.Title))
	var verr *workouts.ValidationError
	switch {
	case err == nil:
		writeJSON(w, s.log, http.StatusCreated, workoutResponse{
			ID:        workout.ID,
			Title:     workout.Title,
			Status:    workout.Status,
			CreatedAt: workout.CreatedAt.UTC().Format(time.RFC3339),
		})
	case errors.As(err, &verr):
		writeValidationError(w, s.log, verr)
	default:
		s.internal(w, r, "create workout failed", err)
	}
}

// handleLogSet is the idempotent set write.
//
//   - 201 — the set was stored for the first time;
//   - 409 — this (userId, clientMutationId) was already accepted; the body
//     carries the originally stored set so the client can settle its outbox;
//   - 404 — the workout does not exist *or* belongs to another user;
//   - 422 — the payload violates the shared domain rules.
func (s *Server) handleLogSet(w http.ResponseWriter, r *http.Request, user store.User) {
	var req logSetRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest, "request body must be a JSON object matching the set schema")
		return
	}

	issues := requiredSetFields(req)
	if len(issues) > 0 {
		writeValidationError(w, s.log, &workouts.ValidationError{Issues: issues})
		return
	}

	input := workouts.SetInput{
		WorkoutID:        r.PathValue("workoutId"),
		ExerciseID:       deref(req.ExerciseID),
		SetNumber:        *req.SetNumber,
		WeightKg:         *req.WeightKg,
		Repetitions:      *req.Repetitions,
		RIR:              *req.RIR,
		ClientMutationID: deref(req.ClientMutationID),
	}

	// user.ID comes from the verified token subject — never from the body.
	stored, created, err := s.workouts.LogSet(r.Context(), user.ID, input)
	var verr *workouts.ValidationError
	switch {
	case err == nil && created:
		w.Header().Set("Location", s.cfg.BasePath+"/workouts/"+stored.WorkoutID+"/sets")
		writeJSON(w, s.log, http.StatusCreated, toSetResponse(stored))
	case err == nil:
		writeJSON(w, s.log, http.StatusConflict, duplicateSetResponse{
			Error: errorPayload{
				Code:    codeDuplicateMutation,
				Message: "clientMutationId was already accepted; the stored set is returned unchanged",
			},
			Set: toSetResponse(stored),
		})
	case errors.As(err, &verr):
		writeValidationError(w, s.log, verr)
	case errors.Is(err, store.ErrNotFound):
		s.writeWorkoutNotFound(w)
	default:
		s.internal(w, r, "log set failed", err)
	}
}

// handleListSets returns the caller's sets for one of the caller's workouts.
func (s *Server) handleListSets(w http.ResponseWriter, r *http.Request, user store.User) {
	sets, err := s.workouts.ListSets(r.Context(), user.ID, r.PathValue("workoutId"))
	switch {
	case err == nil:
		items := make([]setResponse, 0, len(sets))
		for _, set := range sets {
			items = append(items, toSetResponse(set))
		}
		writeJSON(w, s.log, http.StatusOK, setListResponse{Items: items})
	case errors.Is(err, store.ErrNotFound):
		s.writeWorkoutNotFound(w)
	default:
		s.internal(w, r, "list sets failed", err)
	}
}

// writeWorkoutNotFound is the single response used both for a workout that does
// not exist and for one owned by somebody else, so existence never leaks.
func (s *Server) writeWorkoutNotFound(w http.ResponseWriter) {
	writeError(w, s.log, http.StatusNotFound, codeNotFound, "workout not found")
}

func requiredSetFields(req logSetRequest) []workouts.Issue {
	var issues []workouts.Issue
	if req.SetNumber == nil {
		issues = append(issues, workouts.Issue{Field: "setNumber", Message: "required"})
	}
	if req.WeightKg == nil {
		issues = append(issues, workouts.Issue{Field: "weightKg", Message: "required"})
	}
	if req.Repetitions == nil {
		issues = append(issues, workouts.Issue{Field: "repetitions", Message: "required"})
	}
	if req.RIR == nil {
		issues = append(issues, workouts.Issue{Field: "rir", Message: "required"})
	}
	return issues
}
