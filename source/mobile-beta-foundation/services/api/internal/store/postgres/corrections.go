package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

// Correcting and removing a set, and naming a workout after the fact.
//
// Each of the three runs inside one transaction that first *claims* the
// client mutation ID and only then applies the change. The claim is an
// `INSERT … ON CONFLICT DO NOTHING` against the unique index
// client_mutations (user_id, client_mutation_id) — the same shape, and the same
// database-level guarantee, as the set write itself. Two concurrent retries of
// one outbox entry therefore apply the change exactly once: the second insert
// blocks on the index until the first transaction commits and then returns no
// row, which is the replay path.

// claimMutation reserves (userID, clientMutationID) inside tx.
//
// claimed=false means the ID was already spent on exactly this change (a
// replay). store.ErrMutationReused means it was spent on something else, which
// must never be allowed to overwrite an unrelated row.
func claimMutation(ctx context.Context, tx pgx.Tx, userID, clientMutationID, kind, targetID string) (bool, error) {
	if strings.TrimSpace(clientMutationID) == "" {
		// An unlabelled mutation is not deduplicated: the caller only omits the
		// ID for a change whose effect converges on the value it carries.
		return true, nil
	}

	const insert = `INSERT INTO client_mutations (id, user_id, client_mutation_id, kind, target_id, applied_at)
	                VALUES ($1, $2, $3, $4, $5, now())
	                ON CONFLICT (user_id, client_mutation_id) DO NOTHING
	                RETURNING id`
	var claimedID string
	err := tx.QueryRow(ctx, insert, ids.NewUUID(), userID, clientMutationID, kind, targetID).Scan(&claimedID)
	switch {
	case err == nil:
		return true, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return false, fmt.Errorf("postgres: claim client mutation: %w", err)
	}

	// The index rejected the insert: read what the ID was originally spent on.
	const existing = `SELECT kind, target_id FROM client_mutations
	                  WHERE user_id = $1 AND client_mutation_id = $2`
	var storedKind, storedTarget string
	if err := tx.QueryRow(ctx, existing, userID, clientMutationID).Scan(&storedKind, &storedTarget); err != nil {
		return false, fmt.Errorf("postgres: load client mutation: %w", err)
	}
	if storedKind != kind || storedTarget != targetID {
		return false, store.ErrMutationReused
	}
	return false, nil
}

// setColumnsWithAudit is the projection for the correctable set row.
const setColumnsWithAudit = setColumns + `, updated_at, deleted_at`

func scanAuditedSet(row scanner) (store.WorkoutSet, error) {
	var set store.WorkoutSet
	err := row.Scan(&set.ID, &set.UserID, &set.WorkoutID, &set.ExerciseID, &set.SetNumber,
		&set.WeightKg, &set.Repetitions, &set.RIR, &set.ClientMutationID, &set.CreatedAt,
		&set.UpdatedAt, &set.DeletedAt)
	return set, err
}

// lockSetForChange resolves the workout and the set inside tx and takes a row
// lock on the set, so the read and the write that follows are one decision.
//
// Every clause carries user_id, and a set that belongs to a different workout
// is reported as missing rather than as a mismatch, so nothing here can tell a
// caller that an ID exists somewhere they cannot see.
func lockSetForChange(ctx context.Context, tx pgx.Tx, userID, workoutID, setID string) (store.WorkoutSet, string, error) {
	if !ids.IsUUID(userID) || !ids.IsUUID(workoutID) || !ids.IsUUID(setID) {
		return store.WorkoutSet{}, "", store.ErrNotFound
	}

	const workoutQ = `SELECT status FROM workouts WHERE id = $1 AND user_id = $2`
	var status string
	switch err := tx.QueryRow(ctx, workoutQ, workoutID, userID).Scan(&status); {
	case errors.Is(err, pgx.ErrNoRows):
		return store.WorkoutSet{}, "", store.ErrNotFound
	case err != nil:
		return store.WorkoutSet{}, "", fmt.Errorf("postgres: load workout for change: %w", err)
	}

	const setQ = `SELECT ` + setColumnsWithAudit + ` FROM workout_sets
	              WHERE id = $1 AND user_id = $2 AND workout_id = $3
	              FOR UPDATE`
	set, err := scanAuditedSet(tx.QueryRow(ctx, setQ, setID, userID, workoutID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return store.WorkoutSet{}, "", store.ErrNotFound
	case err != nil:
		return store.WorkoutSet{}, "", fmt.Errorf("postgres: load set for change: %w", err)
	}
	return set, status, nil
}

// UpdateWorkoutSet corrects the three mistypeable values of a stored set.
func (s *Store) UpdateWorkoutSet(ctx context.Context, in store.SetUpdate) (store.WorkoutSet, bool, error) {
	var (
		stored  store.WorkoutSet
		applied bool
	)
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		set, status, err := lockSetForChange(ctx, tx, in.UserID, in.WorkoutID, in.SetID)
		if err != nil {
			return err
		}
		// A cancelled session was thrown away; its sets already count towards
		// nothing, and rewriting them would only add noise to a discarded log.
		if status == store.StatusCancelled {
			return store.ErrConflict
		}
		if !set.Live() {
			return store.ErrGone
		}

		claimed, err := claimMutation(ctx, tx, in.UserID, in.ClientMutationID, store.MutationSetUpdate, in.SetID)
		if err != nil {
			return err
		}
		if !claimed {
			// Already applied: hand back the row as the first application left it.
			stored, applied = set, false
			return nil
		}

		at := in.At
		if at.IsZero() {
			at = time.Now()
		}
		const q = `UPDATE workout_sets
		              SET weight_kg = $4, repetitions = $5, rir = $6, updated_at = $7
		            WHERE id = $1 AND user_id = $2 AND workout_id = $3 AND deleted_at IS NULL
		        RETURNING ` + setColumnsWithAudit
		updated, err := scanAuditedSet(tx.QueryRow(ctx, q, in.SetID, in.UserID, in.WorkoutID,
			in.WeightKg, in.Repetitions, in.RIR, at.UTC()))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return store.ErrNotFound
		case isCode(err, codeCheckViolation):
			// The domain bounds are also CHECK constraints; a value that got
			// past validation must never become a 500.
			return fmt.Errorf("postgres: update workout set violates a domain constraint: %w", err)
		case err != nil:
			return fmt.Errorf("postgres: update workout set: %w", err)
		}
		stored, applied = updated, true
		return nil
	})
	if err != nil {
		return store.WorkoutSet{}, false, err
	}
	return stored, applied, nil
}

// DeleteWorkoutSet soft-deletes a set. Repeating it is always safe.
func (s *Store) DeleteWorkoutSet(ctx context.Context, in store.SetDeletion) (store.WorkoutSet, bool, error) {
	var (
		stored  store.WorkoutSet
		applied bool
	)
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		set, status, err := lockSetForChange(ctx, tx, in.UserID, in.WorkoutID, in.SetID)
		if err != nil {
			return err
		}
		if status == store.StatusCancelled {
			return store.ErrConflict
		}
		// An already-deleted set is the end state a deletion asks for, so a
		// repeat converges instead of failing — whatever ID it carries.
		if !set.Live() {
			stored, applied = set, false
			return nil
		}

		claimed, err := claimMutation(ctx, tx, in.UserID, in.ClientMutationID, store.MutationSetDelete, in.SetID)
		if err != nil {
			return err
		}
		if !claimed {
			stored, applied = set, false
			return nil
		}

		at := in.At
		if at.IsZero() {
			at = time.Now()
		}
		const q = `UPDATE workout_sets
		              SET deleted_at = $4, updated_at = $4
		            WHERE id = $1 AND user_id = $2 AND workout_id = $3 AND deleted_at IS NULL
		        RETURNING ` + setColumnsWithAudit
		deleted, err := scanAuditedSet(tx.QueryRow(ctx, q, in.SetID, in.UserID, in.WorkoutID, at.UTC()))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return store.ErrNotFound
		case err != nil:
			return fmt.Errorf("postgres: delete workout set: %w", err)
		}
		stored, applied = deleted, true
		return nil
	})
	if err != nil {
		return store.WorkoutSet{}, false, err
	}
	return stored, applied, nil
}

// RenameWorkout gives a workout its title after the fact.
func (s *Store) RenameWorkout(ctx context.Context, in store.WorkoutRename) (store.Workout, bool, error) {
	if !ids.IsUUID(in.UserID) || !ids.IsUUID(in.WorkoutID) {
		return store.Workout{}, false, store.ErrNotFound
	}

	var (
		stored  store.Workout
		applied bool
	)
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		const load = `SELECT ` + workoutColumns + ` FROM workouts
		              WHERE id = $1 AND user_id = $2 FOR UPDATE`
		workout, err := scanWorkout(tx.QueryRow(ctx, load, in.WorkoutID, in.UserID))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return store.ErrNotFound
		case err != nil:
			return fmt.Errorf("postgres: load workout for rename: %w", err)
		}

		claimed, err := claimMutation(ctx, tx, in.UserID, in.ClientMutationID, store.MutationWorkoutRename, in.WorkoutID)
		if err != nil {
			return err
		}
		if !claimed {
			stored, applied = workout, false
			return nil
		}

		at := in.At
		if at.IsZero() {
			at = time.Now()
		}
		const q = `UPDATE workouts SET title = $3, updated_at = $4
		            WHERE id = $1 AND user_id = $2
		        RETURNING ` + workoutColumns
		renamed, err := scanWorkout(tx.QueryRow(ctx, q, in.WorkoutID, in.UserID, in.Title, at.UTC()))
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return store.ErrNotFound
		case err != nil:
			return fmt.Errorf("postgres: rename workout: %w", err)
		}
		stored, applied = renamed, true
		return nil
	})
	if err != nil {
		return store.Workout{}, false, err
	}
	return stored, applied, nil
}

// inTx runs fn in one transaction, rolling back on any error. Claiming the
// mutation ID and applying the change must be atomic: a crash between the two
// would either lose the change or make a retry look like a replay.
func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit: %w", err)
	}
	return nil
}
