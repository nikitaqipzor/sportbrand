// Package exercises implements the read side of the exercise reference book:
// the catalogue list with its filters and keyset pagination, one exercise card,
// and the dictionaries the client builds its filters from.
//
// Two rules shape everything here.
//
// **Identifiers are immutable.** `exerciseId` already left the phone inside
// `clientMutationId` (`workoutId:exerciseId:setNumber`) and is stored in
// recorded sets. The catalogue may gain records, correct their names and fill
// in their technique, but it may never rename one — see internal/store's
// DetectRenames and the import in seed.go.
//
// **Filtering is by machine code only.** Every filter value is a dictionary
// code, never a free-text fragment, and the one text input — `q` — matches a
// precomputed lowercase field as a literal substring, so nothing a client types
// can change the shape of a query.
package exercises

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"athletica.ai/api/internal/store"
)

// Paging and input bounds. They are part of the published contract.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
	// MaxSearchLen caps `q`. A longer query is not an error worth a 400 — it is
	// truncated, because a phone with a stuck key must not get an error screen.
	MaxSearchLen = 120
	// MaxCodeLen caps one filter value, so a caller cannot push megabytes of
	// codes into an ANY(...) array.
	MaxCodeLen = 64
	// MaxFilterValues caps how many codes one filter may carry.
	MaxFilterValues = 50
)

// ErrInvalidCursor is returned when a cursor is not one this service issued.
var ErrInvalidCursor = errors.New("exercises: malformed cursor")

// Service implements the catalogue use cases.
type Service struct {
	store store.Store
}

// NewService wires the catalogue service.
func NewService(st store.Store) *Service { return &Service{store: st} }

// ListQuery is a validated request for a page of the catalogue.
type ListQuery struct {
	Sports       []string
	Sections     []string
	Equipment    []string
	Muscles      []string
	Difficulties []string
	Search       string
	Limit        int
	Cursor       *store.ExerciseCursor
}

// Page is one page of the catalogue plus the cursor that continues it.
type Page struct {
	Items []store.Exercise
	// NextCursor is empty when the page is the last one.
	NextCursor string
}

// EncodeCursor renders a keyset position as an opaque, URL-safe string.
//
// The position is (sortKey, id) — the same total order both stores apply — and
// it carries no identity, because the catalogue has no owner to carry.
//
// The two halves are joined with a unit separator rather than a NUL: a sort key
// already contains a NUL of its own (SortKey joins the folded name and the
// identifier with one), so splitting on NUL would truncate the key and the
// cursor would silently point at the wrong place.
func EncodeCursor(cursor store.ExerciseCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursor.SortKey + cursorSeparator + cursor.ID))
}

// cursorSeparator is ASCII US (unit separator), which no sort key, identifier
// or exercise name contains.
const cursorSeparator = "\x1f"

// DecodeCursor parses a cursor produced by EncodeCursor.
func DecodeCursor(raw string) (store.ExerciseCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return store.ExerciseCursor{}, ErrInvalidCursor
	}
	sortKey, id, found := strings.Cut(string(decoded), cursorSeparator)
	if !found || strings.TrimSpace(id) == "" || len(id) > MaxCodeLen {
		return store.ExerciseCursor{}, ErrInvalidCursor
	}
	return store.ExerciseCursor{SortKey: sortKey, ID: id}, nil
}

// NormalizeSearch prepares `q` for the literal substring match both stores
// perform. Case folding happens here, in Go, and never in SQL: PostgreSQL's
// lower() under the C collation leaves Cyrillic untouched, and the in-memory
// store would then disagree with it about what matches.
//
// An empty or whitespace-only query is not an error and not a filter — it is
// simply absent, which is what the search box sends while a person is deleting
// what they typed.
func NormalizeSearch(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// Fold the separators a person types into the same shape the index holds,
	// so "жим лёжа" and "жим  лёжа" find the same thing.
	folded := strings.Join(strings.Fields(strings.ToLower(trimmed)), " ")
	if len(folded) > MaxSearchLen {
		folded = truncateUTF8(folded, MaxSearchLen)
	}
	return folded
}

// truncateUTF8 cuts at a rune boundary, so a clipped query is still valid text.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// NormalizeCodes cleans one filter's values: trimmed, lowercased, deduplicated
// and bounded.
//
// A value that is not a code in any dictionary is kept rather than rejected: it
// simply matches nothing. That is deliberate — an unknown code is a client
// built against an older dictionary, and answering it with an empty page is
// kinder than a 400 the user cannot act on.
func NormalizeCodes(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range values {
		// Accept both a repeated parameter and a comma-separated list, exactly
		// as GET /workouts accepts `status`.
		for _, part := range strings.Split(raw, ",") {
			code := strings.ToLower(strings.TrimSpace(part))
			if code == "" || len(code) > MaxCodeLen || seen[code] {
				continue
			}
			seen[code] = true
			out = append(out, code)
			if len(out) >= MaxFilterValues {
				return out
			}
		}
	}
	return out
}

// List returns one page of published exercises.
//
// Like GET /workouts it asks the store for Limit+1 rows: the extra row is what
// says whether a further page exists, without a second COUNT and without ever
// telling the client how many rows it cannot see.
func (s *Service) List(ctx context.Context, query ListQuery) (Page, error) {
	limit := query.Limit
	switch {
	case limit <= 0:
		limit = DefaultPageSize
	case limit > MaxPageSize:
		limit = MaxPageSize
	}

	rows, err := s.store.ListExercises(ctx, store.ExerciseFilter{
		Sports:       query.Sports,
		Sections:     query.Sections,
		Equipment:    query.Equipment,
		Muscles:      query.Muscles,
		Difficulties: query.Difficulties,
		Search:       query.Search,
		// Nothing on the HTTP surface can set this. An unpublished record is
		// invisible to an ordinary user, full stop.
		IncludeUnpublished: false,
		Limit:              limit + 1,
		Cursor:             query.Cursor,
	})
	if err != nil {
		return Page{}, err
	}

	page := Page{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = EncodeCursor(store.ExerciseCursor{SortKey: last.SortKey, ID: last.ID})
	}
	return page, nil
}

// Exercise returns one published record. An unpublished one is store.ErrNotFound,
// indistinguishable from an identifier the catalogue has never held.
func (s *Service) Exercise(ctx context.Context, id string) (store.Exercise, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" || len(id) > MaxCodeLen {
		return store.Exercise{}, store.ErrNotFound
	}
	return s.store.ExerciseByID(ctx, id, false)
}

// Dictionaries returns every machine code with its localized names, grouped by
// kind in the fixed order of store.CodeKinds. The client builds its filter
// controls from this and never from a hard-coded list.
func (s *Service) Dictionaries(ctx context.Context) ([]Dictionary, error) {
	codes, err := s.store.ExerciseCodes(ctx)
	if err != nil {
		return nil, err
	}
	byKind := map[string][]store.ExerciseCode{}
	for _, code := range codes {
		byKind[code.Kind] = append(byKind[code.Kind], code)
	}
	// Every known dictionary is returned, empty ones included: a client must be
	// able to tell "this filter has no values yet" from "this filter does not
	// exist", and an absent key cannot say that.
	out := make([]Dictionary, 0, len(store.CodeKinds))
	for _, kind := range store.CodeKinds {
		items := byKind[kind]
		if items == nil {
			items = []store.ExerciseCode{}
		}
		out = append(out, Dictionary{Kind: kind, Items: items})
	}
	return out, nil
}

// Dictionary is one machine-code vocabulary.
type Dictionary struct {
	Kind  string
	Items []store.ExerciseCode
}
