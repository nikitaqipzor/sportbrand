// Package store defines the persistence boundary of the API service.
//
// Every read and every write is scoped by user ID (audit finding H1): there is
// deliberately no method that can return another user's rows, and the
// implementations take the owner as an explicit argument rather than deriving
// it from the payload. The only method without a user argument —
// DeleteExpiredRefreshTokens — returns a count, never a row.
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
	// ErrConflict is returned when the row exists and belongs to the caller,
	// but its current state forbids the requested change — for example an
	// attempt to complete a cancelled workout. It maps to HTTP 409, never 500.
	ErrConflict = errors.New("store: conflicting state")
	// ErrMutationReused is returned when a clientMutationId this user already
	// spent is presented again for a *different* target or a different kind of
	// change. It is the corrective twin of the idempotent replay: replaying the
	// same mutation is a no-op, but recycling its ID for another row is not.
	ErrMutationReused = errors.New("store: client mutation id already names another change")
	// ErrGone is returned when the row exists and belongs to the caller but has
	// been deleted. It is deliberately not ErrNotFound: a foreign row must stay
	// indistinguishable from a missing one, while the caller's own deleted row
	// is something they themselves removed and can be told about.
	ErrGone = errors.New("store: row was deleted")
)

// Workout statuses. They mirror the domain model shared with the client and
// the CHECK constraint on workouts.status.
const (
	StatusActive    = "active"
	StatusPaused    = "paused"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
)

// User is an account. PasswordHash never leaves the service.
type User struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// Why a refresh token stopped being usable. The distinction matters: a token
// spent by rotation and then presented again is a compromise signal, while one
// the user logged out is simply gone.
const (
	RevokeReasonRotated = "rotated"
	RevokeReasonLogout  = "logout"
	RevokeReasonReuse   = "reuse"
)

// RefreshToken is a stored refresh-token handle. Only the hash is persisted.
type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	IssuedAt  time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
	// RevokedReason is one of the RevokeReason* constants, or empty for a
	// token that is still live (and for rows predating migration 0003).
	RevokedReason string
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
	UpdatedAt time.Time
	// EndedAt is set exactly when the workout reaches a terminal status
	// (completed or cancelled), and is nil while it is still open.
	EndedAt *time.Time
}

// WorkoutSet is a single logged strength-training set.
//
// SetNumber is data the client chose, not a position in a list: it is never
// renumbered by the server, because the client's deterministic mutation ID is
// built as `workoutId:exerciseId:setNumber` and a shifted number would make an
// already-spent ID name a different set.
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
	// UpdatedAt moves when a correction is applied; it equals CreatedAt for a
	// set that was never edited. Added by migration 0004.
	UpdatedAt time.Time
	// DeletedAt is set when the athlete removed the set. Deletion is soft on
	// purpose: the row keeps holding its (user_id, client_mutation_id) slot, so
	// a replay of the *creation* out of the offline outbox cannot resurrect it,
	// and a repeated deletion stays a safe no-op instead of a 404. A deleted
	// set is absent from the workout detail, from the set list and from every
	// progress aggregate.
	DeletedAt *time.Time
}

// Live reports whether the set still counts: it exists and was not deleted.
func (s WorkoutSet) Live() bool { return s.DeletedAt == nil }

// Kinds of idempotent change recorded in the client-mutation ledger. The
// creation of a set is *not* here: its idempotency slot is the unique index on
// workout_sets (user_id, client_mutation_id), which the ledger mirrors for the
// changes that do not insert a set row.
const (
	MutationSetUpdate     = "set_update"
	MutationSetDelete     = "set_delete"
	MutationWorkoutRename = "workout_rename"
)

// SetUpdate is a correction of an already-logged set.
//
// Only the three values a human mistypes are correctable. ExerciseID and
// SetNumber are deliberately absent: they are what the client's deterministic
// clientMutationId is derived from, so letting them move would break the
// offline outbox. All three values are required, so a replayed edit is
// byte-identical to the one that was applied.
type SetUpdate struct {
	UserID           string
	WorkoutID        string
	SetID            string
	WeightKg         float64
	Repetitions      int
	RIR              int
	ClientMutationID string
	At               time.Time
}

// SetDeletion removes one of the caller's sets, softly.
type SetDeletion struct {
	UserID           string
	WorkoutID        string
	SetID            string
	ClientMutationID string
	At               time.Time
}

// WorkoutRename gives a workout — typically one started offline with no name —
// its title. ClientMutationID is optional: a rename converges on the value it
// carries, so an unlabelled one is last-write-wins, while a labelled one is
// deduplicated through the same ledger as every other queued mutation.
type WorkoutRename struct {
	UserID           string
	WorkoutID        string
	Title            string
	ClientMutationID string
	At               time.Time
}

// WorkoutCursor is the keyset position used by GET /workouts. The list is
// ordered by (created_at DESC, id DESC), so the pair is a total order and no
// row can be skipped or repeated when rows are inserted mid-pagination.
type WorkoutCursor struct {
	CreatedAt time.Time
	ID        string
}

// WorkoutFilter narrows and pages the caller's workout list. It carries no
// user ID on purpose: the owner is always a separate argument.
type WorkoutFilter struct {
	// Statuses restricts the result; empty means every status.
	Statuses []string
	// From is an inclusive lower bound on created_at, To an exclusive upper one.
	From *time.Time
	To   *time.Time
	// Limit is the maximum number of rows to return; it is never unbounded.
	Limit int
	// Cursor, when set, returns only rows strictly after it in list order.
	Cursor *WorkoutCursor
}

// ProgressWindow is the closed-open time range [From, To) every progress query
// is evaluated over.
type ProgressWindow struct {
	From time.Time
	To   time.Time
}

// ExerciseRecord is the strength record for one exercise inside a window.
// Sets belonging to cancelled workouts are excluded: a session the athlete
// threw away must not produce a personal record.
type ExerciseRecord struct {
	ExerciseID       string
	Sets             int
	Repetitions      int
	VolumeKg         float64
	BestWeightKg     float64
	BestWeightReps   int
	BestWeightAt     time.Time
	BestEstimated1RM float64
	Best1RMWeightKg  float64
	Best1RMReps      int
	Best1RMAt        time.Time
	LastPerformedAt  time.Time
}

// WeeklyVolume is one ISO week (Monday 00:00 UTC) of training volume.
type WeeklyVolume struct {
	WeekStart   time.Time
	Sets        int
	Repetitions int
	VolumeKg    float64
	Workouts    int
}

// WeeklyAdherence counts how the workouts started in one ISO week ended.
type WeeklyAdherence struct {
	WeekStart  time.Time
	Started    int
	Completed  int
	Cancelled  int
	InProgress int
}

// Estimated1RM is the Epley estimate of a one-rep maximum for one set. It is
// defined here, next to the SQL that mirrors it, so the in-memory store and
// PostgreSQL cannot drift apart.
func Estimated1RM(weightKg float64, repetitions int) float64 {
	return weightKg * (1 + float64(repetitions)/30)
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
	RevokeRefreshToken(ctx context.Context, id, reason string) error
	RevokeUserRefreshTokens(ctx context.Context, userID, reason string) error
	// DeleteExpiredRefreshTokens removes rows that expired or were revoked
	// before the cut-off and reports how many disappeared. It returns a count
	// and never a row, so it cannot leak anybody's data.
	DeleteExpiredRefreshTokens(ctx context.Context, before time.Time) (int64, error)

	// CreateWorkout stores a workout idempotently. A client generates the ID
	// so a session can be started with no connection, exactly as a set carries
	// its own mutation ID; replaying the same (user_id, id) returns the stored
	// row with created=false instead of a second workout. Uniqueness is the
	// database's guarantee, not a read-then-write check in Go.
	CreateWorkout(ctx context.Context, workout Workout) (stored Workout, created bool, err error)
	// WorkoutForUser returns ErrNotFound both for a missing workout and for a
	// workout owned by somebody else, so existence never leaks.
	WorkoutForUser(ctx context.Context, userID, workoutID string) (Workout, error)
	// ListWorkouts returns the caller's workouts, newest first.
	ListWorkouts(ctx context.Context, userID string, filter WorkoutFilter) ([]Workout, error)
	// SetWorkoutStatus moves one of the caller's workouts to next, but only
	// from one of allowedFrom, in a single atomic statement. It returns
	// ErrNotFound when the workout is missing or foreign and ErrConflict when
	// it exists but sits in a status the transition does not allow.
	SetWorkoutStatus(ctx context.Context, userID, workoutID string, allowedFrom []string, next string, at time.Time) (Workout, error)

	// InsertWorkoutSet stores a set idempotently. The uniqueness of
	// (user_id, client_mutation_id) is guaranteed by the storage engine, not by
	// a read-then-write check in Go. It reports inserted=false and returns the
	// previously stored row when the mutation ID was already used by this user.
	InsertWorkoutSet(ctx context.Context, set WorkoutSet) (stored WorkoutSet, inserted bool, err error)
	// ListWorkoutSets returns the live sets of one workout owned by userID.
	// Deleted sets are not part of the answer.
	ListWorkoutSets(ctx context.Context, userID, workoutID string) ([]WorkoutSet, error)

	// UpdateWorkoutSet corrects one of the caller's sets, idempotently.
	//
	// The uniqueness of (user_id, client_mutation_id) in the client-mutation
	// ledger — a database guarantee, not a read-then-write check in Go —
	// decides whether this is the first application or a replay. A replay
	// reports applied=false together with the set as it stands now.
	//
	// Errors: ErrNotFound when the workout or the set is missing, foreign, or
	// not part of that workout; ErrGone when the caller already deleted the
	// set; ErrConflict when the workout is cancelled; ErrMutationReused when
	// the mutation ID was already spent on something else.
	UpdateWorkoutSet(ctx context.Context, in SetUpdate) (stored WorkoutSet, applied bool, err error)

	// DeleteWorkoutSet soft-deletes one of the caller's sets, idempotently.
	// Deleting an already-deleted set is a safe no-op that reports
	// applied=false and returns the stored row, never an error.
	DeleteWorkoutSet(ctx context.Context, in SetDeletion) (stored WorkoutSet, applied bool, err error)

	// RenameWorkout sets the title of one of the caller's workouts. When
	// in.ClientMutationID is empty the rename is applied as last-write-wins;
	// when it is present the ledger makes a replay a no-op.
	RenameWorkout(ctx context.Context, in WorkoutRename) (stored Workout, applied bool, err error)

	// ExerciseRecords aggregates the caller's strength records. The arithmetic
	// happens in the storage engine; sets are never streamed into the service.
	ExerciseRecords(ctx context.Context, userID string, window ProgressWindow, limit int) ([]ExerciseRecord, error)
	// WeeklyVolume aggregates the caller's training volume per ISO week.
	WeeklyVolume(ctx context.Context, userID string, window ProgressWindow) ([]WeeklyVolume, error)
	// WeeklyAdherence counts how the caller's workouts of each ISO week ended.
	WeeklyAdherence(ctx context.Context, userID string, window ProgressWindow) ([]WeeklyAdherence, error)

	// The exercise catalogue. Its rows belong to nobody: they are shared
	// content, so these are the only Store methods without a user argument that
	// return rows. Access is still authenticated at the HTTP boundary; it is
	// simply not scoped, and there is nothing here a caller could scope it by.

	// ListExercises returns one page of the catalogue in (sort_key, id) order.
	// Unpublished records are absent unless filter.IncludeUnpublished is set,
	// which nothing on the HTTP surface can do.
	ListExercises(ctx context.Context, filter ExerciseFilter) ([]Exercise, error)
	// ExerciseByID returns one record. An unpublished record answers
	// ErrNotFound unless includeUnpublished is set, so a draft is
	// indistinguishable from an identifier that does not exist.
	ExerciseByID(ctx context.Context, id string, includeUnpublished bool) (Exercise, error)
	// ExerciseCodes returns every dictionary entry, ordered by (kind,
	// sort_order, code), so the client builds its filters from the same
	// vocabulary the records are coded with.
	ExerciseCodes(ctx context.Context) ([]ExerciseCode, error)
	// SeedExercises applies one import file atomically: either the whole file
	// lands or none of it does.
	//
	// It refuses — with a *RenameError wrapping ErrExerciseRenamed, and writing
	// nothing — a file that would move a slug or a legacy number from one
	// exercise ID to another, because those IDs are already stored inside
	// recorded sets. A record whose content hash is unchanged is skipped rather
	// than rewritten, which is what makes a repeated import a no-op. A record
	// already stored that the file does not mention is left alone and counted
	// as absent; nothing is ever deleted.
	SeedExercises(ctx context.Context, seed ExerciseSeed) (ExerciseSeedReport, error)
}

// NormalizeEmail lowercases and trims an address so that lookups and the
// unique index agree on what "the same account" means.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// WeekStart returns the Monday 00:00 UTC of the ISO week containing t. It is
// the Go twin of date_trunc('week', … AT TIME ZONE 'UTC') in SQL.
func WeekStart(t time.Time) time.Time {
	u := t.UTC()
	offset := (int(u.Weekday()) + 6) % 7 // Monday = 0
	y, m, d := u.Date()
	return time.Date(y, m, d-offset, 0, 0, 0, 0, time.UTC)
}
