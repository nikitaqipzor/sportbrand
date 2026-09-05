// Package memory holds an in-process Store implementation used by tests and by
// `ATHLETICA_STORE_DRIVER=memory` local runs.
//
// It mirrors the PostgreSQL schema constraints on purpose: the unique keys on
// (email) and (user_id, client_mutation_id) are enforced inside the critical
// section, so behaviour matches the database rather than merely approximating it.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

type mutationKey struct {
	userID           string
	clientMutationID string
}

// Store is a concurrency-safe in-memory Store.
type Store struct {
	mu sync.RWMutex

	users        map[string]store.User // by id
	usersByEmail map[string]string     // email -> id

	refresh       map[string]store.RefreshToken // by id
	refreshByHash map[string]string             // hash -> id

	workouts     map[string]store.Workout // by id
	workoutOrder []string                 // insertion order, for stable listing

	sets         map[string]store.WorkoutSet // by id
	setsByMutKey map[mutationKey]string      // unique index (user_id, client_mutation_id)
	setOrder     []string

	now func() time.Time
}

// New returns an empty in-memory store.
func New() *Store {
	return &Store{
		users:         map[string]store.User{},
		usersByEmail:  map[string]string{},
		refresh:       map[string]store.RefreshToken{},
		refreshByHash: map[string]string{},
		workouts:      map[string]store.Workout{},
		sets:          map[string]store.WorkoutSet{},
		setsByMutKey:  map[mutationKey]string{},
		now:           time.Now,
	}
}

// SetClock overrides the clock; tests use it to make TTLs deterministic.
func (s *Store) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

var _ store.Store = (*Store)(nil)

// Ping always succeeds: the in-memory store is as available as the process.
func (s *Store) Ping(context.Context) error { return nil }

// Close is a no-op.
func (s *Store) Close() {}

// CreateUser inserts a user, enforcing the unique e-mail index.
func (s *Store) CreateUser(_ context.Context, user store.User) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	email := store.NormalizeEmail(user.Email)
	if _, exists := s.usersByEmail[email]; exists {
		return store.User{}, store.ErrEmailTaken
	}
	if user.ID == "" {
		user.ID = ids.NewUUID()
	}
	user.Email = email
	if user.CreatedAt.IsZero() {
		user.CreatedAt = s.now().UTC()
	}
	s.users[user.ID] = user
	s.usersByEmail[email] = user.ID
	return user, nil
}

// UserByEmail looks an account up by normalized e-mail.
func (s *Store) UserByEmail(_ context.Context, email string) (store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.usersByEmail[store.NormalizeEmail(email)]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return s.users[id], nil
}

// UserByID looks an account up by primary key.
func (s *Store) UserByID(_ context.Context, id string) (store.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return user, nil
}

// CreateRefreshToken stores a refresh-token handle.
func (s *Store) CreateRefreshToken(_ context.Context, token store.RefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if token.ID == "" {
		token.ID = ids.NewUUID()
	}
	s.refresh[token.ID] = token
	s.refreshByHash[token.TokenHash] = token.ID
	return nil
}

// RefreshTokenByHash resolves a presented token by its stored hash.
func (s *Store) RefreshTokenByHash(_ context.Context, hash string) (store.RefreshToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.refreshByHash[hash]
	if !ok {
		return store.RefreshToken{}, store.ErrNotFound
	}
	return s.refresh[id], nil
}

// RevokeRefreshToken marks a single token as spent, recording why.
func (s *Store) RevokeRefreshToken(_ context.Context, id, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, ok := s.refresh[id]
	if !ok {
		return store.ErrNotFound
	}
	if token.RevokedAt == nil {
		now := s.now().UTC()
		token.RevokedAt = &now
		token.RevokedReason = reason
		s.refresh[id] = token
	}
	return nil
}

// RevokeUserRefreshTokens revokes every refresh token of one user.
func (s *Store) RevokeUserRefreshTokens(_ context.Context, userID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now().UTC()
	for id, token := range s.refresh {
		if token.UserID == userID && token.RevokedAt == nil {
			token.RevokedAt = &now
			token.RevokedReason = reason
			s.refresh[id] = token
		}
	}
	return nil
}

// DeleteExpiredRefreshTokens drops rows that expired or were revoked before
// the cut-off, mirroring the SQL sweep.
func (s *Store) DeleteExpiredRefreshTokens(_ context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var deleted int64
	for id, token := range s.refresh {
		expired := token.ExpiresAt.Before(before)
		revoked := token.RevokedAt != nil && token.RevokedAt.Before(before)
		if !expired && !revoked {
			continue
		}
		delete(s.refresh, id)
		delete(s.refreshByHash, token.TokenHash)
		deleted++
	}
	return deleted, nil
}

// CreateWorkout stores a workout owned by workout.UserID, idempotently: a
// repeat of an ID this user already holds returns the stored row with
// created=false, and an ID held by somebody else is ErrNotFound rather than a
// signal that it exists.
func (s *Store) CreateWorkout(_ context.Context, workout store.Workout) (store.Workout, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if workout.ID != "" {
		if existing, taken := s.workouts[workout.ID]; taken {
			if existing.UserID != workout.UserID {
				return store.Workout{}, false, store.ErrNotFound
			}
			return existing, false, nil
		}
	}
	if workout.ID == "" {
		workout.ID = ids.NewUUID()
	}
	if workout.CreatedAt.IsZero() {
		workout.CreatedAt = s.now().UTC()
	}
	if workout.Status == "" {
		workout.Status = store.StatusActive
	}
	if workout.UpdatedAt.IsZero() {
		workout.UpdatedAt = workout.CreatedAt
	}
	// Mirrors workouts_ended_at_matches_status: ended_at exists exactly for the
	// terminal statuses.
	if isTerminal(workout.Status) && workout.EndedAt == nil {
		ended := workout.UpdatedAt
		workout.EndedAt = &ended
	}
	if !isTerminal(workout.Status) {
		workout.EndedAt = nil
	}
	s.workouts[workout.ID] = workout
	s.workoutOrder = append(s.workoutOrder, workout.ID)
	return workout, true, nil
}

func isTerminal(status string) bool {
	return status == store.StatusCompleted || status == store.StatusCancelled
}

// WorkoutForUser returns ErrNotFound for both missing and foreign workouts.
func (s *Store) WorkoutForUser(_ context.Context, userID, workoutID string) (store.Workout, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	workout, ok := s.workouts[workoutID]
	if !ok || workout.UserID != userID {
		return store.Workout{}, store.ErrNotFound
	}
	return workout, nil
}

// InsertWorkoutSet mirrors the PostgreSQL upsert: the unique key on
// (user_id, client_mutation_id) decides, inside one critical section, whether
// this is a new row or a replay of an already accepted mutation.
func (s *Store) InsertWorkoutSet(_ context.Context, set store.WorkoutSet) (store.WorkoutSet, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := mutationKey{userID: set.UserID, clientMutationID: set.ClientMutationID}
	if existingID, ok := s.setsByMutKey[key]; ok {
		return s.sets[existingID], false, nil
	}

	// Composite foreign key (workout_id, user_id) -> workouts (id, user_id).
	workout, ok := s.workouts[set.WorkoutID]
	if !ok || workout.UserID != set.UserID {
		return store.WorkoutSet{}, false, store.ErrNotFound
	}

	if set.ID == "" {
		set.ID = ids.NewUUID()
	}
	if set.CreatedAt.IsZero() {
		set.CreatedAt = s.now().UTC()
	}
	s.sets[set.ID] = set
	s.setsByMutKey[key] = set.ID
	s.setOrder = append(s.setOrder, set.ID)
	return set, true, nil
}

// ListWorkoutSets returns the caller's sets for one of the caller's workouts.
func (s *Store) ListWorkoutSets(_ context.Context, userID, workoutID string) ([]store.WorkoutSet, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	workout, ok := s.workouts[workoutID]
	if !ok || workout.UserID != userID {
		return nil, store.ErrNotFound
	}

	out := make([]store.WorkoutSet, 0, len(s.setOrder))
	for _, id := range s.setOrder {
		set := s.sets[id]
		if set.UserID == userID && set.WorkoutID == workoutID {
			out = append(out, set)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SetNumber < out[j].SetNumber })
	return out, nil
}

// CountWorkoutSets is a test helper: how many rows exist in total for a user.
func (s *Store) CountWorkoutSets(userID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := 0
	for _, set := range s.sets {
		if set.UserID == userID {
			n++
		}
	}
	return n
}
