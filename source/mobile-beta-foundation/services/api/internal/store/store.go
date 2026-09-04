// Package store defines the persistence boundary of the API service.
//
// Every read and every write is scoped by user ID (audit finding H1): there is
// deliberately no method that can return another user's rows, and the
// implementations take the owner as an explicit argument rather than deriving
// it from the payload.
package store

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Sentinel errors returned by every Store implementation.
var (
	// ErrNotFound is returned when a row does not exist, or exists but belongs
	// to a different user. Callers must not distinguish the two cases.
	ErrNotFound = errors.New("store: not found")
	// ErrEmailTaken is returned when a registration collides with an existing account.
	ErrEmailTaken = errors.New("store: email already registered")
)

// User is an account. PasswordHash never leaves the service.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// RefreshToken is a stored refresh-token handle. Only the hash is persisted.
type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	IssuedAt  time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Active reports whether the token may still be exchanged at time now.
func (t RefreshToken) Active(now time.Time) bool {
	return t.RevokedAt == nil && now.Before(t.ExpiresAt)
}

// Workout is a training session owned by exactly one user.
type Workout struct {
	ID        string
	UserID    string
	Title     string
	Status    string
	CreatedAt time.Time
}

// WorkoutSet is a single logged strength-training set.
type WorkoutSet struct {
	ID               string
	UserID           string
	WorkoutID        string
	ExerciseID       string
	SetNumber        int
	WeightKg         float64
	Repetitions      int
	RIR              int
	ClientMutationID string
	CreatedAt        time.Time
}

// Store is the persistence port used by the HTTP layer.
type Store interface {
	// Ping verifies the backing storage is reachable.
	Ping(ctx context.Context) error
	// Close releases resources held by the implementation.
	Close()

	CreateUser(ctx context.Context, user User) (User, error)
	UserByEmail(ctx context.Context, email string) (User, error)
	UserByID(ctx context.Context, id string) (User, error)

	CreateRefreshToken(ctx context.Context, token RefreshToken) error
	RefreshTokenByHash(ctx context.Context, hash string) (RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, id string) error
	RevokeUserRefreshTokens(ctx context.Context, userID string) error

	CreateWorkout(ctx context.Context, workout Workout) (Workout, error)
	// WorkoutForUser returns ErrNotFound both for a missing workout and for a
	// workout owned by somebody else, so existence never leaks.
	WorkoutForUser(ctx context.Context, userID, workoutID string) (Workout, error)

	// InsertWorkoutSet stores a set idempotently. The uniqueness of
	// (user_id, client_mutation_id) is guaranteed by the storage engine, not by
	// a read-then-write check in Go. It reports inserted=false and returns the
	// previously stored row when the mutation ID was already used by this user.
	InsertWorkoutSet(ctx context.Context, set WorkoutSet) (stored WorkoutSet, inserted bool, err error)
	// ListWorkoutSets returns the sets of one workout owned by userID.
	ListWorkoutSets(ctx context.Context, userID, workoutID string) ([]WorkoutSet, error)
}

// NormalizeEmail lowercases and trims an address so that lookups and the
// unique index agree on what "the same account" means.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
