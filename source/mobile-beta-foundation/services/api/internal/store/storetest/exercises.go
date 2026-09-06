package storetest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"athletica.ai/api/internal/store"
)

// The exercise reference book. Everything below runs against the in-memory
// store on every `go test ./...` and against real PostgreSQL when
// ATHLETICA_TEST_DATABASE_URL is set, so the SQL adapter and the in-process one
// cannot disagree about what the catalogue contains.

// seedDictionaries is the vocabulary every fixture below codes against. It is
// deliberately larger than any single test needs: a code that no exercise uses
// must still come back from ExerciseCodes, because the client builds its filter
// controls from the dictionary and not from the rows.
func seedDictionaries() []store.ExerciseCode {
	return []store.ExerciseCode{
		{Kind: store.CodeSport, Code: "strength", NameRu: "Силовая тренировка", SortOrder: 1},
		{Kind: store.CodeSport, Code: "swimming", NameRu: "Плавание", SortOrder: 2},
		{Kind: store.CodeSection, Code: "legs", NameRu: "Ноги", SortOrder: 1},
		{Kind: store.CodeSection, Code: "back", NameRu: "Спина", SortOrder: 2},
		{Kind: store.CodeSection, Code: "core", NameRu: "Корпус", SortOrder: 3},
		{Kind: store.CodeEquipment, Code: "barbell", NameRu: "Штанга", SortOrder: 1},
		{Kind: store.CodeEquipment, Code: "cable", NameRu: "Блочный тренажёр", SortOrder: 2},
		{Kind: store.CodeEquipment, Code: "bodyweight", NameRu: "Собственный вес", SortOrder: 3},
		{Kind: store.CodeMuscle, Code: "quadriceps", NameRu: "Квадрицепс", SortOrder: 1},
		{Kind: store.CodeMuscle, Code: "glutes", NameRu: "Ягодичные", SortOrder: 2},
		{Kind: store.CodeMuscle, Code: "lats", NameRu: "Широчайшие", SortOrder: 3},
		{Kind: store.CodeMuscle, Code: "abs", NameRu: "Мышцы живота", SortOrder: 4},
		{Kind: store.CodeMovementPattern, Code: "squat", NameRu: "Приседание", SortOrder: 1},
		{Kind: store.CodeMovementPattern, Code: "vertical-pull", NameRu: "Вертикальная тяга", SortOrder: 2},
		{Kind: store.CodeDifficulty, Code: "beginner", NameRu: "Начальный", SortOrder: 1},
		{Kind: store.CodeDifficulty, Code: "intermediate", NameRu: "Средний", SortOrder: 2},
		{Kind: store.CodeDifficulty, Code: "advanced", NameRu: "Продвинутый", SortOrder: 3},
		{Kind: store.CodeLaterality, Code: "bilateral", NameRu: "Двустороннее", SortOrder: 1},
		{Kind: store.CodeGoalTag, Code: "hypertrophy", NameRu: "Гипертрофия", SortOrder: 1},
	}
}

// fixtureExercise builds a published record. The sort key and the search text
// are computed exactly as the importer computes them, because a store must be
// handed the same shape the importer produces.
func fixtureExercise(id, nameRu, section string, opts ...func(*store.Exercise)) store.Exercise {
	e := store.Exercise{
		ID:                id,
		Slug:              id,
		SchemaVersion:     1,
		ContentVersion:    1,
		ContentLocale:     "ru-RU",
		ContentHash:       hashOf(id + "|v1"),
		Revision:          1,
		NameRu:            nameRu,
		Sport:             "strength",
		Section:           section,
		Equipment:         []string{"barbell"},
		PrimaryMuscles:    []string{"quadriceps"},
		PublicationStatus: store.PublicationPublished,
		ReviewStatus:      store.ReviewApproved,
		MediaStatus:       store.MediaApproved,
		Published:         true,
	}
	e.SortKey = lowerFold(nameRu) + "\x00" + id
	e.SearchText = "\n" + lowerFold(nameRu) + "\n" + id + "\n"
	for _, opt := range opts {
		opt(&e)
	}
	return e
}

func withDifficulty(code string) func(*store.Exercise) {
	return func(e *store.Exercise) { e.Difficulty = &code }
}

func withEquipment(codes ...string) func(*store.Exercise) {
	return func(e *store.Exercise) { e.Equipment = codes }
}

func withMuscles(primary, secondary []string) func(*store.Exercise) {
	return func(e *store.Exercise) { e.PrimaryMuscles = primary; e.SecondaryMuscles = secondary }
}

func withLegacyNumber(n int) func(*store.Exercise) {
	return func(e *store.Exercise) { e.LegacyNumber = &n }
}

func unpublished(publication, review, media string) func(*store.Exercise) {
	return func(e *store.Exercise) {
		e.PublicationStatus = publication
		e.ReviewStatus = review
		e.MediaStatus = media
		e.Published = false
	}
}

func withContent(hash, nameRu string) func(*store.Exercise) {
	return func(e *store.Exercise) {
		e.NameRu = nameRu
		e.ContentHash = hashOf(hash)
		e.SortKey = lowerFold(nameRu) + "\x00" + e.ID
		e.SearchText = "\n" + lowerFold(nameRu) + "\n" + e.ID + "\n"
	}
}

func mustSeed(t *testing.T, st store.Store, seed store.ExerciseSeed) store.ExerciseSeedReport {
	t.Helper()
	report, err := st.SeedExercises(context.Background(), seed)
	if err != nil {
		t.Fatalf("seed exercises: %v", err)
	}
	return report
}

func baseSeed(exercises ...store.Exercise) store.ExerciseSeed {
	return store.ExerciseSeed{
		Source:         "storetest",
		FileSHA256:     hashOf("storetest"),
		SchemaVersion:  1,
		ContentVersion: 1,
		ContentLocale:  "ru-RU",
		Codes:          seedDictionaries(),
		Exercises:      exercises,
	}
}

// testExerciseSeedIsIdempotent is the property the whole importer exists for:
// running the same file twice must not produce a second copy of anything, and
// must not even touch the rows it already stored.
func testExerciseSeedIsIdempotent(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()

	seed := baseSeed(
		fixtureExercise("back-squat", "Приседания со штангой", "legs"),
		fixtureExercise("lat-pulldown", "Тяга верхнего блока", "back"),
	)

	first := mustSeed(t, st, seed)
	if first.Added != 2 || first.Updated != 0 || first.Skipped != 0 {
		t.Fatalf("first import = %+v, want 2 added", first)
	}

	second := mustSeed(t, st, seed)
	if second.Added != 0 || second.Updated != 0 || second.Skipped != 2 {
		t.Fatalf("second import = %+v, want 2 skipped and nothing written", second)
	}

	rows, err := st.ListExercises(ctx, store.ExerciseFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("a repeated import left %d rows, want 2", len(rows))
	}
	for _, row := range rows {
		if row.Revision != 1 {
			t.Fatalf("%s is at revision %d after a no-op import, want 1", row.ID, row.Revision)
		}
	}

	// A record whose content really did change is updated, and only that one.
	changed := baseSeed(
		fixtureExercise("back-squat", "Приседания со штангой", "legs", withContent("back-squat|v2", "Приседания со штангой (низкий гриф)")),
		fixtureExercise("lat-pulldown", "Тяга верхнего блока", "back"),
	)
	third := mustSeed(t, st, changed)
	if third.Added != 0 || third.Updated != 1 || third.Skipped != 1 {
		t.Fatalf("third import = %+v, want exactly one update", third)
	}
	updated, err := st.ExerciseByID(ctx, "back-squat", false)
	if err != nil {
		t.Fatalf("load updated exercise: %v", err)
	}
	if updated.Revision != 2 {
		t.Fatalf("revision = %d after one real change, want 2", updated.Revision)
	}
	if updated.NameRu != "Приседания со штангой (низкий гриф)" {
		t.Fatalf("name = %q, the update did not land", updated.NameRu)
	}
}

// testExerciseRenameIsRefused is the invariant that protects recorded history.
//
// `exerciseId` already left the phone inside clientMutationId and is stored in
// workout_sets. A file that gives an existing slug or legacy number to a
// different identifier is refused whole — and nothing it carried is written,
// including the parts that were not about the rename at all.
func testExerciseRenameIsRefused(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()

	mustSeed(t, st, baseSeed(
		fixtureExercise("back-squat", "Приседания со штангой", "legs", withLegacyNumber(1)),
		fixtureExercise("lat-pulldown", "Тяга верхнего блока", "back", withLegacyNumber(2)),
	))

	// The same exercise under a new identifier: same slug, new EXERCISE_ID.
	renamedSlug := fixtureExercise("barbell-back-squat", "Приседания со штангой", "legs", withLegacyNumber(1))
	renamedSlug.Slug = "back-squat"

	_, err := st.SeedExercises(ctx, baseSeed(
		renamedSlug,
		fixtureExercise("front-squat", "Фронтальные приседания", "legs", withLegacyNumber(9)),
	))
	if !errors.Is(err, store.ErrExerciseRenamed) {
		t.Fatalf("err = %v, want ErrExerciseRenamed", err)
	}
	var renameErr *store.RenameError
	if !errors.As(err, &renameErr) || len(renameErr.Conflicts) == 0 {
		t.Fatalf("the refusal must name the conflicting identifiers, got %v", err)
	}

	// Nothing landed: not the rename, and not the innocent record beside it.
	rows, err := st.ListExercises(ctx, store.ExerciseFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("a refused import wrote %d rows, want the original 2", len(rows))
	}
	if _, err := st.ExerciseByID(ctx, "barbell-back-squat", true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the renamed identifier was stored anyway: %v", err)
	}
	if _, err := st.ExerciseByID(ctx, "front-squat", true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a refused file is refused whole; front-squat should not exist: %v", err)
	}
	if _, err := st.ExerciseByID(ctx, "back-squat", false); err != nil {
		t.Fatalf("the original identifier must survive untouched: %v", err)
	}

	// A legacy number moving to another identifier is the same refusal.
	movedLegacy := fixtureExercise("cable-pulldown", "Тяга верхнего блока", "back", withLegacyNumber(2))
	if _, err := st.SeedExercises(ctx, baseSeed(movedLegacy)); !errors.Is(err, store.ErrExerciseRenamed) {
		t.Fatalf("err = %v, want ErrExerciseRenamed for a moved LEGACY_NUMBER", err)
	}

	// Adding a genuinely new record is still fine — the guard refuses renames,
	// not growth.
	report := mustSeed(t, st, baseSeed(
		fixtureExercise("back-squat", "Приседания со штангой", "legs", withLegacyNumber(1)),
		fixtureExercise("lat-pulldown", "Тяга верхнего блока", "back", withLegacyNumber(2)),
		fixtureExercise("plank", "Планка", "core", withLegacyNumber(3)),
	))
	if report.Added != 1 || report.Skipped != 2 {
		t.Fatalf("growing the catalogue = %+v, want 1 added and 2 skipped", report)
	}
}

// testUnpublishedExercisesAreInvisible pins the three-status rule: publication
// needs ready/published *and* both approvals, and anything short of that is
// indistinguishable from an identifier that does not exist.
func testUnpublishedExercisesAreInvisible(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()

	mustSeed(t, st, baseSeed(
		fixtureExercise("back-squat", "Приседания со штангой", "legs"),
		fixtureExercise("draft-only", "Черновик", "legs",
			unpublished(store.PublicationDraft, store.ReviewDraft, store.MediaDraft)),
		fixtureExercise("ready-unreviewed", "Готово без проверки", "legs",
			unpublished(store.PublicationReady, store.ReviewDraft, store.MediaApproved)),
		fixtureExercise("ready-no-media", "Готово без медиа", "legs",
			unpublished(store.PublicationReady, store.ReviewApproved, store.MediaInReview)),
		fixtureExercise("approved-but-not-published", "Одобрено, но не опубликовано", "legs",
			unpublished(store.PublicationReady, store.ReviewApproved, store.MediaApproved)),
		fixtureExercise("archived", "Снято с публикации", "legs",
			unpublished(store.PublicationArchived, store.ReviewApproved, store.MediaApproved)),
	))

	visible, err := st.ListExercises(ctx, store.ExerciseFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(visible) != 1 || visible[0].ID != "back-squat" {
		t.Fatalf("the catalogue showed %d rows (%v), want only the published one", len(visible), idsOf(visible))
	}

	for _, hidden := range []string{"draft-only", "ready-unreviewed", "ready-no-media", "approved-but-not-published", "archived"} {
		if _, err := st.ExerciseByID(ctx, hidden, false); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("%s answered %v, want ErrNotFound — an unpublished record must look missing", hidden, err)
		}
		if _, err := st.ExerciseByID(ctx, hidden, true); err != nil {
			t.Fatalf("%s must still be readable to the importer: %v", hidden, err)
		}
	}

	// Approving the review and the media, and moving publication to published,
	// is what makes it appear — one status short is still invisible.
	published := fixtureExercise("ready-no-media", "Готово без медиа", "legs")
	published.ContentHash = hashOf("ready-no-media|approved")
	mustSeed(t, st, baseSeed(published))
	if _, err := st.ExerciseByID(ctx, "ready-no-media", false); err != nil {
		t.Fatalf("a fully approved record must be visible: %v", err)
	}
}

// testExerciseFilteringAndPaging walks the catalogue one row at a time and
// checks that the pages together are exactly the unpaged answer: no row lost,
// no row seen twice, with a filter and a search applied on top.
func testExerciseFilteringAndPaging(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()

	// Two records deliberately share a name, so the cursor's tiebreaker on the
	// identifier is exercised rather than assumed.
	mustSeed(t, st, baseSeed(
		fixtureExercise("back-squat", "Приседания со штангой", "legs", withDifficulty("intermediate")),
		fixtureExercise("front-squat", "Приседания со штангой", "legs", withDifficulty("advanced")),
		fixtureExercise("goblet-squat", "Приседания с гантелью", "legs", withEquipment("bodyweight"), withDifficulty("beginner")),
		fixtureExercise("lat-pulldown", "Тяга верхнего блока", "back", withEquipment("cable"),
			withMuscles([]string{"lats"}, []string{"quadriceps"}), withDifficulty("beginner")),
		fixtureExercise("pull-up", "Подтягивания", "back", withEquipment("bodyweight"),
			withMuscles([]string{"lats"}, nil), withDifficulty("advanced")),
		fixtureExercise("plank", "Планка", "core", withEquipment("bodyweight"),
			withMuscles([]string{"abs"}, nil), withDifficulty("beginner")),
		fixtureExercise("hidden", "Скрытое упражнение", "core",
			unpublished(store.PublicationDraft, store.ReviewDraft, store.MediaDraft)),
	))

	cases := []struct {
		name   string
		filter store.ExerciseFilter
		want   []string
	}{
		{"everything published", store.ExerciseFilter{},
			[]string{"plank", "pull-up", "goblet-squat", "back-squat", "front-squat", "lat-pulldown"}},
		{"by section", store.ExerciseFilter{Sections: []string{"back"}},
			[]string{"pull-up", "lat-pulldown"}},
		{"by two sections", store.ExerciseFilter{Sections: []string{"back", "core"}},
			[]string{"plank", "pull-up", "lat-pulldown"}},
		{"by equipment", store.ExerciseFilter{Equipment: []string{"bodyweight"}},
			[]string{"plank", "pull-up", "goblet-squat"}},
		{"by muscle, primary or secondary", store.ExerciseFilter{Muscles: []string{"quadriceps"}},
			[]string{"goblet-squat", "back-squat", "front-squat", "lat-pulldown"}},
		{"by difficulty", store.ExerciseFilter{Difficulties: []string{"beginner"}},
			[]string{"plank", "goblet-squat", "lat-pulldown"}},
		{"by sport", store.ExerciseFilter{Sports: []string{"strength"}},
			[]string{"plank", "pull-up", "goblet-squat", "back-squat", "front-squat", "lat-pulldown"}},
		{"by a sport nothing uses", store.ExerciseFilter{Sports: []string{"swimming"}}, nil},
		{"search", store.ExerciseFilter{Search: "присед"}, []string{"goblet-squat", "back-squat", "front-squat"}},
		{"search and filter together", store.ExerciseFilter{Search: "присед", Difficulties: []string{"advanced"}},
			[]string{"front-squat"}},
		{"search matching nothing", store.ExerciseFilter{Search: "гребля"}, nil},
		// An empty search is not a filter — it is the search box mid-deletion.
		{"empty search", store.ExerciseFilter{Search: ""},
			[]string{"plank", "pull-up", "goblet-squat", "back-squat", "front-squat", "lat-pulldown"}},
		// None of these may be read as a pattern, an escape or an operator.
		{"percent is a literal", store.ExerciseFilter{Search: "%"}, nil},
		{"underscore is a literal", store.ExerciseFilter{Search: "_"}, nil},
		{"backslash is a literal", store.ExerciseFilter{Search: `\`}, nil},
		{"quote is a literal", store.ExerciseFilter{Search: "'"}, nil},
		{"a whole injection is a literal", store.ExerciseFilter{Search: "'; DROP TABLE exercise; --"}, nil},
		{"a wildcard sandwich matches nothing", store.ExerciseFilter{Search: "%присед%"}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := st.ListExercises(ctx, tc.filter)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if got := idsOf(rows); !equalStrings(got, tc.want) {
				t.Fatalf("filter returned %v, want %v", got, tc.want)
			}
			// The same filter, walked one row per page, must produce exactly the
			// same sequence: nothing skipped, nothing repeated.
			if got := walk(t, st, tc.filter, 1); !equalStrings(got, tc.want) {
				t.Fatalf("paging one row at a time returned %v, want %v", got, tc.want)
			}
			if got := walk(t, st, tc.filter, 2); !equalStrings(got, tc.want) {
				t.Fatalf("paging two rows at a time returned %v, want %v", got, tc.want)
			}
			// A page size that lands exactly on the boundary is the case that
			// breaks naive cursors.
			if len(tc.want) > 0 {
				if got := walk(t, st, tc.filter, len(tc.want)); !equalStrings(got, tc.want) {
					t.Fatalf("paging at the exact boundary returned %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// walk pages the whole catalogue with the given page size, following the cursor
// exactly as a client would, and returns every identifier it saw in order.
func walk(t *testing.T, st store.Store, filter store.ExerciseFilter, pageSize int) []string {
	t.Helper()
	ctx := context.Background()

	var (
		seen   []string
		cursor *store.ExerciseCursor
	)
	for page := 0; page < 100; page++ {
		filter.Limit = pageSize
		filter.Cursor = cursor
		rows, err := st.ListExercises(ctx, filter)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(rows) == 0 {
			return seen
		}
		if len(rows) > pageSize {
			t.Fatalf("page %d returned %d rows, more than the requested limit %d", page, len(rows), pageSize)
		}
		seen = append(seen, idsOf(rows)...)
		last := rows[len(rows)-1]
		cursor = &store.ExerciseCursor{SortKey: last.SortKey, ID: last.ID}
	}
	t.Fatal("paging did not terminate; the cursor is not advancing")
	return nil
}

// testExerciseCodesExistInDictionaries is the rule that keeps the client's
// filters honest: every machine code an answer carries must be a code the
// dictionary endpoint also returns, and a record coded with something unknown
// is refused at import rather than served.
func testExerciseCodesExistInDictionaries(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()

	mustSeed(t, st, baseSeed(
		fixtureExercise("back-squat", "Приседания со штангой", "legs", withDifficulty("intermediate")),
		fixtureExercise("lat-pulldown", "Тяга верхнего блока", "back", withEquipment("cable"),
			withMuscles([]string{"lats"}, []string{"quadriceps"})),
	))

	codes, err := st.ExerciseCodes(ctx)
	if err != nil {
		t.Fatalf("dictionaries: %v", err)
	}
	known := map[store.CodeKey]bool{}
	for _, code := range codes {
		if !store.IsCodeKind(code.Kind) {
			t.Fatalf("dictionary %q is not one of the known kinds", code.Kind)
		}
		if code.NameRu == "" {
			t.Fatalf("code %s/%s has no Russian name; the app would show a machine token", code.Kind, code.Code)
		}
		known[store.CodeKey{Kind: code.Kind, Code: code.Code}] = true
	}

	rows, err := st.ListExercises(ctx, store.ExerciseFilter{IncludeUnpublished: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows to check")
	}
	for _, row := range rows {
		for _, use := range store.ExerciseCodeUses(row) {
			if !known[use] {
				t.Fatalf("%s carries %s=%q, which the dictionary endpoint does not return", row.ID, use.Kind, use.Code)
			}
		}
	}

	// A record coded with something no dictionary defines never reaches the
	// catalogue at all.
	unknown := fixtureExercise("mystery", "Загадка", "legs")
	unknown.Equipment = []string{"time-machine"}
	if _, err := st.SeedExercises(ctx, baseSeed(unknown)); !errors.Is(err, store.ErrUnknownExerciseCode) {
		t.Fatalf("err = %v, want ErrUnknownExerciseCode", err)
	}
	if _, err := st.ExerciseByID(ctx, "mystery", true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a record with an unknown code was stored anyway: %v", err)
	}
}

// testExerciseImportNeverDeletes: a record the file does not mention is counted
// and left alone. Deleting it would strand every recorded set that names it.
func testExerciseImportNeverDeletes(t *testing.T, newStore Factory) {
	st := newStore(t)
	ctx := context.Background()

	mustSeed(t, st, baseSeed(
		fixtureExercise("back-squat", "Приседания со штангой", "legs"),
		fixtureExercise("plank", "Планка", "core"),
	))

	report := mustSeed(t, st, baseSeed(fixtureExercise("back-squat", "Приседания со штангой", "legs")))
	if report.Absent != 1 {
		t.Fatalf("absent = %d, want 1 — the omitted record must be reported", report.Absent)
	}
	if _, err := st.ExerciseByID(ctx, "plank", false); err != nil {
		t.Fatalf("an omitted record must survive the import: %v", err)
	}
}

func idsOf(rows []store.Exercise) []string {
	if len(rows) == 0 {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ID)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// hashOf produces a content hash the way the importer does: SHA-256 of the
// record as stated, hex encoded, which is the width the schema demands.
func hashOf(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

// lowerFold mirrors the importer's case folding so a fixture sorts and matches
// the way an imported record would.
func lowerFold(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
		case r >= 'А' && r <= 'Я':
			out = append(out, r+32)
		case r == 'Ё':
			out = append(out, 'ё')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
