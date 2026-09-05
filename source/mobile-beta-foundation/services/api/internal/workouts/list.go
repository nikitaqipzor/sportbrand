package workouts

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"athletica.ai/api/internal/store"
)

// Paging bounds for GET /workouts. They are part of the published contract.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// ErrInvalidCursor is returned when a cursor is not one this service issued.
var ErrInvalidCursor = errors.New("workouts: malformed cursor")

// ListQuery is a validated request for a page of the caller's workouts.
type ListQuery struct {
	Statuses []string
	From     *time.Time
	To       *time.Time
	Limit    int
	Cursor   *store.WorkoutCursor
}

// Page is one page of workouts plus the cursor that continues it.
type Page struct {
	Items []store.Workout
	// NextCursor is empty when the page is the last one.
	NextCursor string
}

// EncodeCursor renders a keyset position as an opaque, URL-safe string. The
// encoding carries no user ID: a cursor from another account still only ever
// selects rows inside the caller's own `user_id = $1` scope.
func EncodeCursor(cursor store.WorkoutCursor) string {
	raw := cursor.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + cursor.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses a cursor produced by EncodeCursor.
func DecodeCursor(raw string) (store.WorkoutCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return store.WorkoutCursor{}, ErrInvalidCursor
	}
	timestamp, id, found := strings.Cut(string(decoded), "|")
	if !found || strings.TrimSpace(id) == "" || len(id) > MaxIdentifierLen {
		return store.WorkoutCursor{}, ErrInvalidCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return store.WorkoutCursor{}, ErrInvalidCursor
	}
	return store.WorkoutCursor{CreatedAt: createdAt.UTC(), ID: id}, nil
}

// ListWorkouts returns one page of the caller's workouts, newest first.
//
// It asks the store for Limit+1 rows: the extra row is what tells the service
// whether a further page exists, without a second COUNT query and without ever
// telling the client how many rows it cannot see.
func (s *Service) ListWorkouts(ctx context.Context, userID string, query ListQuery) (Page, error) {
	limit := query.Limit
	switch {
	case limit <= 0:
		limit = DefaultPageSize
	case limit > MaxPageSize:
		limit = MaxPageSize
	}

	rows, err := s.store.ListWorkouts(ctx, userID, store.WorkoutFilter{
		Statuses: query.Statuses,
		From:     query.From,
		To:       query.To,
		Limit:    limit + 1,
		Cursor:   query.Cursor,
	})
	if err != nil {
		return Page{}, err
	}

	page := Page{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = EncodeCursor(store.WorkoutCursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return page, nil
}
