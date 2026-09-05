package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"athletica.ai/api/internal/store"
	"athletica.ai/api/internal/workouts"
)

// Correcting a logged set, and naming a workout after it was started.
//
// Both are ordinary offline-outbox mutations: they carry a clientMutationId,
// they are settled by a unique index in the database, and a replay never
// applies twice. The owner comes from the access token in every one of them —
// there is no userId field on the wire, and one in the body is ignored.

// updateSetRequest is the wire form of PATCH /workouts/{workoutId}/sets/{setId}.
//
// The three correctable values are all required. That is deliberate: a replay
// out of the outbox is then byte-identical to the mutation that was applied,
// and a partial patch cannot silently reset the field it left out. exerciseId
// and setNumber are absent because the client's deterministic mutation ID is
// `workoutId:exerciseId:setNumber` — moving either would make an already-spent
// ID name a different set.
type updateSetRequest struct {
	WeightKg         *float64 `json:"weightKg"`
	Repetitions      *int     `json:"repetitions"`
	RIR              *int     `json:"rir"`
	ClientMutationID *string  `json:"clientMutationId"`
}

// deleteSetRequest is the wire form of DELETE /workouts/{workoutId}/sets/{setId}.
// The mutation ID may also arrive as the `clientMutationId` query parameter,
// for clients and proxies that drop a body from a DELETE; the body wins.
type deleteSetRequest struct {
	ClientMutationID *string `json:"clientMutationId"`
}

// renameWorkoutRequest is the wire form of PATCH /workouts/{workoutId}.
type renameWorkoutRequest struct {
	Title            *string `json:"title"`
	ClientMutationID *string `json:"clientMutationId"`
}

// handleUpdateSet corrects the weight, repetitions or RIR of a stored set.
//
//   - 200 — the correction was applied; the body is the set as it now stands;
//   - 409 duplicate_client_mutation — this clientMutationId was already spent;
//     the body carries the stored set, unchanged by the replay;
//   - 409 set_deleted — the athlete already removed this set;
//   - 409 workout_not_editable — the workout was cancelled;
//   - 404 — the workout or the set does not exist *or* belongs to another user;
//   - 422 — the payload leaves the domain bounds.
func (s *Server) handleUpdateSet(w http.ResponseWriter, r *http.Request, user store.User) {
	var req updateSetRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest,
			"request body must be a JSON object with weightKg, repetitions, rir and clientMutationId")
		return
	}

	var issues []workouts.Issue
	if req.WeightKg == nil {
		issues = append(issues, workouts.Issue{Field: "weightKg", Message: "required"})
	}
	if req.Repetitions == nil {
		issues = append(issues, workouts.Issue{Field: "repetitions", Message: "required"})
	}
	if req.RIR == nil {
		issues = append(issues, workouts.Issue{Field: "rir", Message: "required"})
	}
	if len(issues) > 0 {
		writeValidationError(w, s.log, &workouts.ValidationError{Issues: issues})
		return
	}

	input := workouts.SetUpdateInput{
		WorkoutID:        r.PathValue("workoutId"),
		SetID:            r.PathValue("setId"),
		WeightKg:         *req.WeightKg,
		Repetitions:      *req.Repetitions,
		RIR:              *req.RIR,
		ClientMutationID: deref(req.ClientMutationID),
	}

	// user.ID comes from the verified token subject — never from the body.
	stored, applied, err := s.workouts.UpdateSet(r.Context(), user.ID, input)
	var verr *workouts.ValidationError
	switch {
	case err == nil && applied:
		writeJSON(w, s.log, http.StatusOK, toSetResponse(stored))
	case err == nil:
		// A replay. The stored set is returned so the outbox can settle on the
		// values that were actually applied, which need not be the ones in the
		// body it just sent.
		writeJSON(w, s.log, http.StatusConflict, duplicateSetResponse{
			Error: errorPayload{
				Code:    codeDuplicateMutation,
				Message: "clientMutationId was already applied; the stored set is returned unchanged",
			},
			Set: toSetResponse(stored),
		})
	case errors.As(err, &verr):
		writeValidationError(w, s.log, verr)
	default:
		s.writeCorrectionError(w, r, "update set failed", err)
	}
}

// handleDeleteSet removes a set. The deletion is soft, and repeating it is
// safe: the second call answers `200` with the same stored row rather than a
// `404`, because the state the caller asked for is the state that already
// holds. That is the one place this API answers a replay with `200` instead of
// `409` — a deletion converges, an edit does not.
func (s *Server) handleDeleteSet(w http.ResponseWriter, r *http.Request, user store.User) {
	mutationID := strings.TrimSpace(r.URL.Query().Get("clientMutationId"))
	if r.ContentLength != 0 {
		var req deleteSetRequest
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest,
				"request body must be a JSON object with a clientMutationId")
			return
		}
		if req.ClientMutationID != nil {
			mutationID = deref(req.ClientMutationID)
		}
	}

	stored, applied, err := s.workouts.DeleteSet(r.Context(), user.ID, workouts.SetDeleteInput{
		WorkoutID:        r.PathValue("workoutId"),
		SetID:            r.PathValue("setId"),
		ClientMutationID: mutationID,
	})
	var verr *workouts.ValidationError
	switch {
	case err == nil:
		// Applied or repeated, the answer is the same deleted set: a client
		// retrying an entry whose response it never saw is not punished.
		_ = applied
		writeJSON(w, s.log, http.StatusOK, toSetResponse(stored))
	case errors.As(err, &verr):
		writeValidationError(w, s.log, verr)
	default:
		s.writeCorrectionError(w, r, "delete set failed", err)
	}
}

// handleRenameWorkout gives a workout its title after the fact — the session
// started offline with no name is exactly the case this exists for.
//
// clientMutationId is optional here: a rename converges on the title it
// carries, so an unlabelled one is last-write-wins, while a labelled one is
// deduplicated through the same ledger as an edit. Either way the answer is
// `200` with the workout as it now stands.
func (s *Server) handleRenameWorkout(w http.ResponseWriter, r *http.Request, user store.User) {
	var req renameWorkoutRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest,
			"request body must be a JSON object with a title")
		return
	}
	if req.Title == nil {
		writeValidationError(w, s.log, &workouts.ValidationError{
			Issues: []workouts.Issue{{Field: "title", Message: "required"}},
		})
		return
	}

	workout, _, err := s.workouts.Rename(r.Context(), user.ID, workouts.RenameInput{
		WorkoutID:        r.PathValue("workoutId"),
		Title:            *req.Title,
		ClientMutationID: deref(req.ClientMutationID),
	})
	var verr *workouts.ValidationError
	switch {
	case err == nil:
		writeJSON(w, s.log, http.StatusOK, toWorkoutResponse(workout))
	case errors.As(err, &verr):
		writeValidationError(w, s.log, verr)
	default:
		s.writeCorrectionError(w, r, "rename workout failed", err)
	}
}

// writeCorrectionError maps the store sentinels the three corrections share.
//
// store.ErrNotFound is the single answer for a missing row and for a foreign
// one, so ownership never leaks through a status code.
func (s *Server) writeCorrectionError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeWorkoutNotFound(w)
	case errors.Is(err, store.ErrGone):
		writeError(w, s.log, http.StatusConflict, codeSetDeleted,
			"this set was deleted; it cannot be corrected any more")
	case errors.Is(err, store.ErrConflict):
		writeError(w, s.log, http.StatusConflict, codeWorkoutNotEditable,
			"a cancelled workout cannot be edited")
	case errors.Is(err, store.ErrMutationReused):
		writeError(w, s.log, http.StatusConflict, codeDuplicateMutation,
			"clientMutationId was already used for a different change; generate a new one")
	default:
		s.internal(w, r, msg, err)
	}
}
