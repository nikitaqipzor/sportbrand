// Package postgres is the production Store implementation on top of pgx.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

// PostgreSQL SQLSTATE codes we translate into store sentinels.
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
	codeCheckViolation      = "23514"
)

// Store implements store.Store against PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

var _ store.Store = (*Store)(nil)

// Open creates a connection pool and verifies it is usable.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse database url: %w", err)
	}
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 15 * time.Minute
	if cfg.MaxConns < 4 {
		cfg.MaxConns = 4
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Pool exposes the underlying pool for the migration runner.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Ping checks database availability, used by GET /health.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Close drains the pool.
func (s *Store) Close() { s.pool.Close() }

// CreateUser inserts an account; the unique index on lower(email) decides.
func (s *Store) CreateUser(ctx context.Context, user store.User) (store.User, error) {
	if user.ID == "" {
		user.ID = ids.NewUUID()
	}
	user.Email = store.NormalizeEmail(user.Email)
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now().UTC()
	}

	const q = `INSERT INTO users (id, email, password_hash, created_at)
	           VALUES ($1, $2, $3, $4)
	           RETURNING id, email, password_hash, created_at`

	var out store.User
	err := s.pool.QueryRow(ctx, q, user.ID, user.Email, user.PasswordHash, user.CreatedAt).
		Scan(&out.ID, &out.Email, &out.PasswordHash, &out.CreatedAt)
	if err != nil {
		if isCode(err, codeUniqueViolation) {
			return store.User{}, store.ErrEmailTaken
		}
		return store.User{}, fmt.Errorf("postgres: create user: %w", err)
	}
	return out, nil
}

// UserByEmail resolves an account by normalized address.
func (s *Store) UserByEmail(ctx context.Context, email string) (store.User, error) {
	const q = `SELECT id, email, password_hash, created_at FROM users WHERE lower(email) = lower($1)`
	return s.scanUser(ctx, q, store.NormalizeEmail(email))
}

// UserByID resolves an account by primary key.
func (s *Store) UserByID(ctx context.Context, id string) (store.User, error) {
	if !ids.IsUUID(id) {
		return store.User{}, store.ErrNotFound
	}
	const q = `SELECT id, email, password_hash, created_at FROM users WHERE id = $1`
	return s.scanUser(ctx, q, id)
}

func (s *Store) scanUser(ctx context.Context, q string, arg any) (store.User, error) {
	var user store.User
	err := s.pool.QueryRow(ctx, q, arg).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.CreatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return store.User{}, store.ErrNotFound
	case err != nil:
		return store.User{}, fmt.Errorf("postgres: load user: %w", err)
	}
	return user, nil
}

// CreateRefreshToken persists a refresh-token hash.
func (s *Store) CreateRefreshToken(ctx context.Context, token store.RefreshToken) error {
	if token.ID == "" {
		token.ID = ids.NewUUID()
	}
	if token.IssuedAt.IsZero() {
		token.IssuedAt = time.Now().UTC()
	}
	const q = `INSERT INTO refresh_tokens (id, user_id, token_hash, issued_at, expires_at, revoked_at, revoked_reason)
	           VALUES ($1, $2, $3, $4, $5, $6, $7)`
	if _, err := s.pool.Exec(ctx, q, token.ID, token.UserID, token.TokenHash, token.IssuedAt, token.ExpiresAt, token.RevokedAt, token.RevokedReason); err != nil {
		return fmt.Errorf("postgres: create refresh token: %w", err)
	}
	return nil
}

// RefreshTokenByHash resolves a presented refresh token.
func (s *Store) RefreshTokenByHash(ctx context.Context, hash string) (store.RefreshToken, error) {
	const q = `SELECT id, user_id, token_hash, issued_at, expires_at, revoked_at, revoked_reason
	           FROM refresh_tokens WHERE token_hash = $1`
	var token store.RefreshToken
	err := s.pool.QueryRow(ctx, q, hash).
		Scan(&token.ID, &token.UserID, &token.TokenHash, &token.IssuedAt, &token.ExpiresAt, &token.RevokedAt, &token.RevokedReason)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return store.RefreshToken{}, store.ErrNotFound
	case err != nil:
		return store.RefreshToken{}, fmt.Errorf("postgres: load refresh token: %w", err)
	}
	return token, nil
}

// RevokeRefreshToken marks one token as spent, recording why.
func (s *Store) RevokeRefreshToken(ctx context.Context, id, reason string) error {
	const q = `UPDATE refresh_tokens SET revoked_at = now(), revoked_reason = $2
	           WHERE id = $1 AND revoked_at IS NULL`
	tag, err := s.pool.Exec(ctx, q, id, reason)
	if err != nil {
		return fmt.Errorf("postgres: revoke refresh token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Already revoked or unknown: both are fine for the caller.
		return nil
	}
	return nil
}

// RevokeUserRefreshTokens revokes the whole family after a replay is detected.
func (s *Store) RevokeUserRefreshTokens(ctx context.Context, userID, reason string) error {
	const q = `UPDATE refresh_tokens SET revoked_at = now(), revoked_reason = $2
	           WHERE user_id = $1 AND revoked_at IS NULL`
	if _, err := s.pool.Exec(ctx, q, userID, reason); err != nil {
		return fmt.Errorf("postgres: revoke user refresh tokens: %w", err)
	}
	return nil
}

// DeleteExpiredRefreshTokens removes tokens that expired or were revoked
// before the cut-off. It answers with a count and never with a row.
func (s *Store) DeleteExpiredRefreshTokens(ctx context.Context, before time.Time) (int64, error) {
	const q = `DELETE FROM refresh_tokens
	           WHERE expires_at < $1 OR (revoked_at IS NOT NULL AND revoked_at < $1)`
	tag, err := s.pool.Exec(ctx, q, before)
	if err != nil {
		return 0, fmt.Errorf("postgres: delete expired refresh tokens: %w", err)
	}
	return tag.RowsAffected(), nil
}

// workoutColumns is the projection every workout query shares.
const workoutColumns = `id, user_id, title, status, created_at, updated_at, ended_at`

func scanWorkout(row scanner) (store.Workout, error) {
	var workout store.Workout
	err := row.Scan(&workout.ID, &workout.UserID, &workout.Title, &workout.Status,
		&workout.CreatedAt, &workout.UpdatedAt, &workout.EndedAt)
	return workout, err
}

// CreateWorkout inserts a workout owned by workout.UserID.
func (s *Store) CreateWorkout(ctx context.Context, workout store.Workout) (store.Workout, bool, error) {
	if workout.ID == "" {
		workout.ID = ids.NewUUID()
	}
	if workout.Status == "" {
		workout.Status = store.StatusActive
	}
	if workout.CreatedAt.IsZero() {
		workout.CreatedAt = time.Now().UTC()
	}
	if workout.UpdatedAt.IsZero() {
		workout.UpdatedAt = workout.CreatedAt
	}
	// workouts_ended_at_matches_status: ended_at exists exactly for a terminal
	// status. Normalise here so a caller cannot trip the CHECK constraint.
	switch {
	case workout.Status == store.StatusCompleted || workout.Status == store.StatusCancelled:
		if workout.EndedAt == nil {
			ended := workout.UpdatedAt
			workout.EndedAt = &ended
		}
	default:
		workout.EndedAt = nil
	}

	const q = `INSERT INTO workouts (` + workoutColumns + `)
	           VALUES ($1, $2, $3, $4, $5, $6, $7)
	           ON CONFLICT (id) DO NOTHING
	           RETURNING ` + workoutColumns
	out, err := scanWorkout(s.pool.QueryRow(ctx, q, workout.ID, workout.UserID, workout.Title,
		workout.Status, workout.CreatedAt, workout.UpdatedAt, workout.EndedAt))
	switch {
	case err == nil:
		return out, true, nil
	case errors.Is(err, pgx.ErrNoRows):
		// The ID is already taken. It is the caller's own workout only when the
		// owner matches; a collision with somebody else's row must not hand it
		// over, and must not reveal that it exists either.
		existing, findErr := s.WorkoutForUser(ctx, workout.UserID, workout.ID)
		if findErr != nil {
			return store.Workout{}, false, findErr
		}
		return existing, false, nil
	default:
		return store.Workout{}, false, fmt.Errorf("postgres: create workout: %w", err)
	}
}

// WorkoutForUser scopes the lookup by owner, so a foreign workout is
// indistinguishable from a missing one.
func (s *Store) WorkoutForUser(ctx context.Context, userID, workoutID string) (store.Workout, error) {
	if !ids.IsUUID(workoutID) || !ids.IsUUID(userID) {
		return store.Workout{}, store.ErrNotFound
	}
	const q = `SELECT ` + workoutColumns + ` FROM workouts WHERE id = $1 AND user_id = $2`
	workout, err := scanWorkout(s.pool.QueryRow(ctx, q, workoutID, userID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return store.Workout{}, store.ErrNotFound
	case err != nil:
		return store.Workout{}, fmt.Errorf("postgres: load workout: %w", err)
	}
	return workout, nil
}

const setColumns = `id, user_id, workout_id, exercise_id, set_number, weight_kg, repetitions, rir, client_mutation_id, created_at`

// InsertWorkoutSet performs the idempotent write. `ON CONFLICT DO NOTHING` on
// the unique index (user_id, client_mutation_id) means a replay can never add a
// second row, even when two requests race on different connections.
func (s *Store) InsertWorkoutSet(ctx context.Context, set store.WorkoutSet) (store.WorkoutSet, bool, error) {
	if set.ID == "" {
		set.ID = ids.NewUUID()
	}
	if set.CreatedAt.IsZero() {
		set.CreatedAt = time.Now().UTC()
	}

	const insert = `INSERT INTO workout_sets (` + setColumns + `, updated_at)
	                VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
	                ON CONFLICT (user_id, client_mutation_id) DO NOTHING
	                RETURNING ` + setColumnsWithAudit

	stored, err := scanAuditedSet(s.pool.QueryRow(ctx, insert,
		set.ID, set.UserID, set.WorkoutID, set.ExerciseID, set.SetNumber,
		set.WeightKg, set.Repetitions, set.RIR, set.ClientMutationID, set.CreatedAt))
	switch {
	case err == nil:
		return stored, true, nil
	case errors.Is(err, pgx.ErrNoRows):
		// The unique index rejected the insert: return the row already stored.
		// A *deleted* set still holds its slot, so a replayed creation out of
		// the outbox answers with the deleted row instead of resurrecting it.
		existing, findErr := s.workoutSetByMutation(ctx, set.UserID, set.ClientMutationID)
		if findErr != nil {
			return store.WorkoutSet{}, false, findErr
		}
		return existing, false, nil
	case isCode(err, codeForeignKeyViolation):
		// (workout_id, user_id) does not match any workout of this user.
		return store.WorkoutSet{}, false, store.ErrNotFound
	case isCode(err, codeCheckViolation):
		return store.WorkoutSet{}, false, fmt.Errorf("postgres: insert workout set violates a domain constraint: %w", err)
	default:
		return store.WorkoutSet{}, false, fmt.Errorf("postgres: insert workout set: %w", err)
	}
}

func (s *Store) workoutSetByMutation(ctx context.Context, userID, clientMutationID string) (store.WorkoutSet, error) {
	const q = `SELECT ` + setColumnsWithAudit + ` FROM workout_sets
	           WHERE user_id = $1 AND client_mutation_id = $2`
	set, err := scanAuditedSet(s.pool.QueryRow(ctx, q, userID, clientMutationID))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return store.WorkoutSet{}, store.ErrNotFound
	case err != nil:
		return store.WorkoutSet{}, fmt.Errorf("postgres: load workout set by mutation: %w", err)
	}
	return set, nil
}

// ListWorkoutSets returns the live sets of a workout, scoped to its owner.
// Deleted sets are filtered out by the partial index, so a removed set
// disappears from the "Итоги" detail without a second query.
func (s *Store) ListWorkoutSets(ctx context.Context, userID, workoutID string) ([]store.WorkoutSet, error) {
	if _, err := s.WorkoutForUser(ctx, userID, workoutID); err != nil {
		return nil, err
	}
	const q = `SELECT ` + setColumnsWithAudit + ` FROM workout_sets
	           WHERE user_id = $1 AND workout_id = $2 AND deleted_at IS NULL
	           ORDER BY set_number, created_at`
	rows, err := s.pool.Query(ctx, q, userID, workoutID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list workout sets: %w", err)
	}
	defer rows.Close()

	out := []store.WorkoutSet{}
	for rows.Next() {
		set, err := scanAuditedSet(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan workout set: %w", err)
		}
		out = append(out, set)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list workout sets: %w", err)
	}
	return out, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func isCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}
