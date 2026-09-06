package exercises_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"athletica.ai/api/internal/exercises"
	"athletica.ai/api/internal/store"
	"athletica.ai/api/seed"
)

// minimalFile is a valid one-record import file in the master template's own
// UPPER_SNAKE spelling.
const minimalFile = `{
  "SCHEMA_VERSION": 1,
  "CONTENT_VERSION": 3,
  "CONTENT_LOCALE": "ru-RU",
  "DICTIONARIES": {
    "sport":            [{"CODE": "strength", "NAME_RU": "Силовая тренировка"}],
    "section":          [{"CODE": "legs", "NAME_RU": "Ноги"}],
    "equipment":        [{"CODE": "barbell", "NAME_RU": "Штанга"}],
    "muscle":           [{"CODE": "quadriceps", "NAME_RU": "Квадрицепс"}],
    "movement_pattern": [{"CODE": "squat", "NAME_RU": "Приседание"}]
  },
  "EXERCISES": [
    {
      "EXERCISE_ID": "back-squat",
      "LEGACY_NUMBER": 1,
      "NAME_RU": "Приседания со штангой",
      "SPORT": "strength",
      "SECTION": "legs",
      "MOVEMENT_PATTERN": "squat",
      "EQUIPMENT": ["barbell"],
      "PRIMARY_MUSCLES": ["quadriceps"],
      "PUBLICATION_STATUS": "published",
      "REVIEW_STATUS": "approved",
      "MEDIA_STATUS": "approved"
    }
  ]
}`

func TestParseSeedReadsTheMasterTemplateSpelling(t *testing.T) {
	parsed, err := exercises.ParseSeed("test.json", []byte(minimalFile))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Exercises) != 1 {
		t.Fatalf("parsed %d exercises, want 1", len(parsed.Exercises))
	}
	e := parsed.Exercises[0]
	if e.ID != "back-squat" || e.NameRu != "Приседания со штангой" {
		t.Fatalf("parsed %+v", e)
	}
	if e.Slug != "back-squat" {
		t.Fatalf("slug = %q, want it to default to the identifier", e.Slug)
	}
	if e.LegacyNumber == nil || *e.LegacyNumber != 1 {
		t.Fatalf("legacyNumber = %v, want 1", e.LegacyNumber)
	}
	if !e.Published {
		t.Fatal("ready + approved + approved must be published")
	}
	if e.ContentVersion != 3 {
		t.Fatalf("contentVersion = %d, want the file's 3 to be inherited", e.ContentVersion)
	}
	// Technique and safety are absent from the source and must stay absent.
	if !e.Technique.Empty() || !e.Safety.Empty() {
		t.Fatalf("the parser invented technique or safety content: %+v / %+v", e.Technique, e.Safety)
	}
	if len(parsed.Codes) != 5 {
		t.Fatalf("parsed %d dictionary entries, want 5", len(parsed.Codes))
	}
}

// The content pipeline is being written in parallel with this importer, so all
// three plausible spellings of a field name must decode into the same record.
func TestParseSeedAcceptsEverySpellingOfAFieldName(t *testing.T) {
	camel := strings.NewReplacer(
		`"EXERCISE_ID"`, `"exerciseId"`,
		`"NAME_RU"`, `"nameRu"`,
		`"LEGACY_NUMBER"`, `"legacyNumber"`,
		`"PRIMARY_MUSCLES"`, `"primaryMuscles"`,
		`"PUBLICATION_STATUS"`, `"publicationStatus"`,
		`"REVIEW_STATUS"`, `"reviewStatus"`,
		`"MEDIA_STATUS"`, `"mediaStatus"`,
		`"MOVEMENT_PATTERN"`, `"movementPattern"`,
		`"CONTENT_VERSION"`, `"contentVersion"`,
	).Replace(minimalFile)

	upper, err := exercises.ParseSeed("upper.json", []byte(minimalFile))
	if err != nil {
		t.Fatalf("parse upper: %v", err)
	}
	lower, err := exercises.ParseSeed("camel.json", []byte(camel))
	if err != nil {
		t.Fatalf("parse camel: %v", err)
	}
	if upper.Exercises[0].ID != lower.Exercises[0].ID || upper.Exercises[0].NameRu != lower.Exercises[0].NameRu {
		t.Fatalf("the two spellings decoded differently: %+v vs %+v", upper.Exercises[0], lower.Exercises[0])
	}
	// The content hash is a fingerprint of the record's *content*, not of how
	// its keys were spelled, so a re-spelled file must not look like a change.
	if upper.Exercises[0].ContentHash != lower.Exercises[0].ContentHash {
		t.Fatal("re-spelling the field names changed the content hash; a cosmetic diff would rewrite every row")
	}
	// The dictionary kind `movement_pattern` is data, not a field name, and
	// must keep its underscore.
	var seenPattern bool
	for _, code := range lower.Codes {
		if code.Kind == store.CodeMovementPattern {
			seenPattern = true
		}
	}
	if !seenPattern {
		t.Fatal("the movement_pattern dictionary lost its kind")
	}
}

func TestParseSeedRefusesInvalidFiles(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"no contentVersion", func(m map[string]any) { delete(m, "CONTENT_VERSION") }, "contentVersion"},
		{"unknown schema version", func(m map[string]any) { m["SCHEMA_VERSION"] = 99 }, "schemaVersion"},
		{"no exercises", func(m map[string]any) { m["EXERCISES"] = []any{} }, "no exercises"},
		{"unknown dictionary", func(m map[string]any) {
			m["DICTIONARIES"].(map[string]any)["mood"] = []any{map[string]any{"CODE": "happy", "NAME_RU": "Радость"}}
		}, "mood"},
		{"a code with no Russian name", func(m map[string]any) {
			m["DICTIONARIES"].(map[string]any)["sport"] = []any{map[string]any{"CODE": "strength"}}
		}, "nameRu"},
		{"no EXERCISE_ID", func(m map[string]any) {
			m["EXERCISES"].([]any)[0].(map[string]any)["EXERCISE_ID"] = ""
		}, "EXERCISE_ID is required"},
		{"an identifier that is not a slug", func(m map[string]any) {
			m["EXERCISES"].([]any)[0].(map[string]any)["EXERCISE_ID"] = "Back Squat!"
		}, "lowercase words"},
		{"no NAME_RU", func(m map[string]any) {
			m["EXERCISES"].([]any)[0].(map[string]any)["NAME_RU"] = "  "
		}, "NAME_RU is required"},
		{"no SPORT", func(m map[string]any) {
			m["EXERCISES"].([]any)[0].(map[string]any)["SPORT"] = ""
		}, "SPORT is required"},
		{"a status outside the lifecycle", func(m map[string]any) {
			m["EXERCISES"].([]any)[0].(map[string]any)["PUBLICATION_STATUS"] = "live"
		}, "PUBLICATION_STATUS"},
		// The master template's rule: publication needs both approvals.
		{"published without an expert review", func(m map[string]any) {
			m["EXERCISES"].([]any)[0].(map[string]any)["REVIEW_STATUS"] = "in_review"
		}, "publication needs both approved"},
		{"published without approved media", func(m map[string]any) {
			m["EXERCISES"].([]any)[0].(map[string]any)["MEDIA_STATUS"] = "draft"
		}, "publication needs both approved"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal([]byte(minimalFile), &document); err != nil {
				t.Fatalf("fixture: %v", err)
			}
			tc.mutate(document)
			raw, err := json.Marshal(document)
			if err != nil {
				t.Fatalf("re-encode: %v", err)
			}
			_, err = exercises.ParseSeed("test.json", raw)
			if err == nil {
				t.Fatal("the file was accepted, want a refusal")
			}
			var seedErr *exercises.SeedError
			if !errors.As(err, &seedErr) {
				t.Fatalf("err = %T %v, want *exercises.SeedError", err, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the refusal does not mention %q:\n%v", tc.want, err)
			}
		})
	}
}

// A file that names the same identifier twice is refused before it reaches the
// store: which of the two records would win is not something to guess at.
func TestParseSeedRefusesDuplicateIdentitiesInsideOneFile(t *testing.T) {
	for _, tc := range []struct {
		name         string
		second       string
		wantFragment string
	}{
		{"the same EXERCISE_ID twice", `"EXERCISE_ID": "back-squat", "SLUG": "other", "LEGACY_NUMBER": 2`, "was already used"},
		{"two records sharing a SLUG", `"EXERCISE_ID": "front-squat", "SLUG": "back-squat", "LEGACY_NUMBER": 2`, "is already the slug"},
		{"two records sharing a LEGACY_NUMBER", `"EXERCISE_ID": "front-squat", "LEGACY_NUMBER": 1`, "is already used by"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doubled := strings.Replace(minimalFile, `  "EXERCISES": [
    {`, `  "EXERCISES": [
    {
      `+tc.second+`,
      "NAME_RU": "Второе",
      "SPORT": "strength",
      "SECTION": "legs"
    },
    {`, 1)
			_, err := exercises.ParseSeed("test.json", []byte(doubled))
			if err == nil || !strings.Contains(err.Error(), tc.wantFragment) {
				t.Fatalf("err = %v, want a refusal mentioning %q", err, tc.wantFragment)
			}
		})
	}
}

// The embedded starter set is shipped in the repository and must always parse:
// a broken one would take the whole `seed-exercises` command down with it.
func TestEmbeddedStarterSetIsAValidImportFile(t *testing.T) {
	parsed, err := exercises.ParseSeed(seed.StarterSource, seed.StarterExercises)
	if err != nil {
		t.Fatalf("the embedded starter set does not parse: %v", err)
	}
	if len(parsed.Exercises) != 20 {
		t.Fatalf("the starter set holds %d exercises, want the 20 the app shipped with", len(parsed.Exercises))
	}

	known := map[store.CodeKey]bool{}
	for _, code := range parsed.Codes {
		known[store.CodeKey{Kind: code.Kind, Code: code.Code}] = true
	}
	if missing := store.MissingCodes(known, parsed.Exercises); len(missing) > 0 {
		t.Fatalf("the starter set uses codes its own dictionaries do not define: %v", missing)
	}

	for _, e := range parsed.Exercises {
		if !e.Published {
			t.Fatalf("%s is not published; the client would see nothing", e.ID)
		}
		if e.NameRu == "" {
			t.Fatalf("%s has no Russian name", e.ID)
		}
		// The starter set is a name and a classification. It carries no
		// methodology, and a test is the right place to keep it that way: a
		// plausible invention is worse than a blank, because people train on it.
		if !e.Technique.Empty() {
			t.Fatalf("%s carries technique the source never provided: %+v", e.ID, e.Technique)
		}
		if !e.Safety.Empty() {
			t.Fatalf("%s carries safety guidance the source never provided: %+v", e.ID, e.Safety)
		}
		if e.Difficulty != nil {
			t.Fatalf("%s claims a difficulty level; the source column has not been converted yet", e.ID)
		}
	}
}

// The starter set must carry exactly the identifiers the app already sends,
// because they are inside clientMutationId in every recorded set.
func TestStarterSetKeepsTheIdentifiersTheAppAlreadySends(t *testing.T) {
	// Taken verbatim from apps/mobile/src/features/workout/exercise-catalog.ts.
	appCatalog := []string{
		"back-squat", "front-squat", "deadlift", "romanian-deadlift", "bench-press",
		"incline-bench-press", "overhead-press", "barbell-row", "lat-pulldown", "seated-row",
		"pull-up", "dip", "leg-press", "leg-curl", "lunge", "hip-thrust",
		"biceps-curl", "triceps-pushdown", "lateral-raise", "plank",
	}

	parsed, err := exercises.ParseSeed(seed.StarterSource, seed.StarterExercises)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	have := map[string]bool{}
	for _, e := range parsed.Exercises {
		have[e.ID] = true
	}
	for _, id := range appCatalog {
		if !have[id] {
			t.Fatalf("the starter set dropped %q, an identifier already stored inside recorded sets", id)
		}
	}
	if len(have) != len(appCatalog) {
		t.Fatalf("the starter set holds %d identifiers, the app has %d", len(have), len(appCatalog))
	}
}
