package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"athletica.ai/api/internal/store"
	"athletica.ai/api/internal/workouts"
)

type createWorkoutRequest struct {
	// ID lets the client name the workout it is starting, so a session begun
	// offline keeps one identity from the first set onwards.
	ID    *string `json:"id"`
	Title *string `json:"title"`
}

// workoutResponse gained updatedAt and endedAt in contract 0.3.0. Both are
// additive: a client built against 0.2.0 keeps working unchanged.
type workoutResponse struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
	EndedAt   *string `json:"endedAt"`
}

// workoutDetailResponse is a workout with its sets and their totals — the
// payload behind the "Итоги" screen.
type workoutDetailResponse struct {
	workoutResponse
	Sets   []setResponse         `json:"sets"`
	Totals workoutTotalsResponse `json:"totals"`
}

type workoutTotalsResponse struct {
	Sets        int     `json:"sets"`
	Repetitions int     `json:"repetitions"`
	VolumeKg    float64 `json:"volumeKg"`
}

type workoutListResponse struct {
	Items []workoutResponse `json:"items"`
	// NextCursor is null on the last page. It is opaque: clients must echo it
	// back untouched rather than build one themselves.
	NextCursor *string `json:"nextCursor"`
}

type workoutStatusRequest struct {
	Status *string `json:"status"`
}

func toWorkoutResponse(w store.Workout) workoutResponse {
	out := workoutResponse{
		ID:        w.ID,
		Title:     w.Title,
		Status:    w.Status,
		CreatedAt: w.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: w.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if w.EndedAt != nil {
		ended := w.EndedAt.UTC().Format(time.RFC3339)
		out.EndedAt = &ended
	}
	return out
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

// setResponse gained updatedAt and deletedAt in contract 0.5.0. Both are
// additive: a client built against 0.4.0 keeps working unchanged.
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
	UpdatedAt        string  `json:"updatedAt"`
	// DeletedAt is null for every set the client can still see. It is non-null
	// only in the answer to a deletion, and in the 409 that a replayed
	// *creation* of an already-removed set gets — which is how the outbox
	// learns that its retry must not resurrect anything.
	DeletedAt *string `json:"deletedAt"`
}

type duplicateSetResponse struct {
	Error errorPayload `json:"error"`
	Set   setResponse  `json:"set"`
}

type setListResponse struct {
	Items []setResponse `json:"items"`
}

func toSetResponse(s store.WorkoutSet) setResponse {
	out := setResponse{
		ID:               s.ID,
		WorkoutID:        s.WorkoutID,
		ExerciseID:       s.ExerciseID,
		SetNumber:        s.SetNumber,
		WeightKg:         s.WeightKg,
		Repetitions:      s.Repetitions,
		RIR:              s.RIR,
		ClientMutationID: s.ClientMutationID,
		CreatedAt:        s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        s.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if s.DeletedAt != nil {
		deleted := s.DeletedAt.UTC().Format(time.RFC3339)
		out.DeletedAt = &deleted
	}
	return out
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

	workout, created, err := s.workouts.CreateWorkout(r.Context(), user.ID, deref(req.ID), deref(req.Title))
	var verr *workouts.ValidationError
	switch {
	case err == nil && created:
		writeJSON(w, s.log, http.StatusCreated, toWorkoutResponse(workout))
	case err == nil:
		// The client already started this workout: 200 with the stored row, so
		// a retry after a lost response settles instead of failing.
		writeJSON(w, s.log, http.StatusOK, toWorkoutResponse(workout))
	case errors.As(err, &verr):
		writeValidationError(w, s.log, verr)
	case errors.Is(err, store.ErrNotFound):
		// The ID belongs to somebody else. Refuse without admitting it exists.
		writeError(w, s.log, http.StatusConflict, codeWorkoutIDTaken, "workout id is already in use")
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

// handleListWorkouts pages the caller's workouts for the "Итоги" screen.
//
// Filters: `status` (repeatable or comma separated), `from`/`to` on the
// creation timestamp, `limit` and an opaque `cursor`. The owner is the token
// subject, so there is no query parameter that could widen the scope.
func (s *Server) handleListWorkouts(w http.ResponseWriter, r *http.Request, user store.User) {
	query := r.URL.Query()

	statuses, err := workouts.ParseStatuses(query["status"])
	if err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest,
			"status must be one of "+strings.Join(workouts.AllStatuses(), ", "))
		return
	}
	from, err := optionalTime(query.Get("from"))
	if err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest,
			"from must be an RFC 3339 timestamp or a YYYY-MM-DD date")
		return
	}
	to, err := optionalTime(query.Get("to"))
	if err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest,
			"to must be an RFC 3339 timestamp or a YYYY-MM-DD date")
		return
	}
	limit, err := optionalInt(query.Get("limit"), 0)
	if err != nil || limit < 0 || limit > workouts.MaxPageSize {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest,
			"limit must be an integer between 1 and "+strconv.Itoa(workouts.MaxPageSize))
		return
	}

	listQuery := workouts.ListQuery{Statuses: statuses, From: from, To: to, Limit: limit}
	if raw := strings.TrimSpace(query.Get("cursor")); raw != "" {
		cursor, err := workouts.DecodeCursor(raw)
		if err != nil {
			writeError(w, s.log, http.StatusBadRequest, codeInvalidCursor,
				"cursor is not one this API issued; start the list again without it")
			return
		}
		listQuery.Cursor = &cursor
	}

	page, err := s.workouts.ListWorkouts(r.Context(), user.ID, listQuery)
	if err != nil {
		s.internal(w, r, "list workouts failed", err)
		return
	}

	body := workoutListResponse{Items: make([]workoutResponse, 0, len(page.Items))}
	for _, workout := range page.Items {
		body.Items = append(body.Items, toWorkoutResponse(workout))
	}
	if page.NextCursor != "" {
		next := page.NextCursor
		body.NextCursor = &next
	}
	writeJSON(w, s.log, http.StatusOK, body)
}

// handleGetWorkout returns one of the caller's workouts with its sets.
func (s *Server) handleGetWorkout(w http.ResponseWriter, r *http.Request, user store.User) {
	detail, err := s.workouts.Workout(r.Context(), user.ID, r.PathValue("workoutId"))
	switch {
	case err == nil:
	case errors.Is(err, store.ErrNotFound):
		s.writeWorkoutNotFound(w)
		return
	default:
		s.internal(w, r, "load workout failed", err)
		return
	}

	body := workoutDetailResponse{
		workoutResponse: toWorkoutResponse(detail.Workout),
		Sets:            make([]setResponse, 0, len(detail.Sets)),
	}
	for _, set := range detail.Sets {
		body.Sets = append(body.Sets, toSetResponse(set))
		body.Totals.Sets++
		body.Totals.Repetitions += set.Repetitions
		body.Totals.VolumeKg += set.WeightKg * float64(set.Repetitions)
	}
	body.Totals.VolumeKg = round2(body.Totals.VolumeKg)
	writeJSON(w, s.log, http.StatusOK, body)
}

// handleWorkoutStatus applies a lifecycle transition.
//
//   - 200 — the workout is now in the requested status (asking for the status
//     it already holds is an accepted no-op, so a retried request is safe);
//   - 404 — the workout is missing *or* belongs to another user;
//   - 409 — the transition is not allowed from the current status, e.g.
//     completing a cancelled workout. Never a 500;
//   - 422 — the requested status is not part of the domain.
func (s *Server) handleWorkoutStatus(w http.ResponseWriter, r *http.Request, user store.User) {
	var req workoutStatusRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest,
			"request body must be a JSON object with a status field")
		return
	}
	if req.Status == nil {
		writeValidationError(w, s.log, &workouts.ValidationError{
			Issues: []workouts.Issue{{Field: "status", Message: "required"}},
		})
		return
	}

	workout, err := s.workouts.Transition(r.Context(), user.ID, r.PathValue("workoutId"), *req.Status)
	switch {
	case err == nil:
		writeJSON(w, s.log, http.StatusOK, toWorkoutResponse(workout))
	case errors.Is(err, workouts.ErrUnknownStatus):
		writeValidationError(w, s.log, &workouts.ValidationError{Issues: []workouts.Issue{{
			Field:   "status",
			Message: "must be one of " + strings.Join(workouts.AllStatuses(), ", "),
		}}})
	case errors.Is(err, store.ErrNotFound):
		s.writeWorkoutNotFound(w)
	case errors.Is(err, workouts.ErrInvalidTransition):
		writeError(w, s.log, http.StatusConflict, codeInvalidTransition,
			"this workout cannot move to "+strings.ToLower(strings.TrimSpace(*req.Status))+" from its current status")
	default:
		s.internal(w, r, "workout transition failed", err)
	}
}

// optionalTime parses an RFC 3339 timestamp or a bare YYYY-MM-DD date (read as
// UTC midnight). An empty string means "not given".
func optionalTime(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		utc := parsed.UTC()
		return &utc, nil
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func optionalInt(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
