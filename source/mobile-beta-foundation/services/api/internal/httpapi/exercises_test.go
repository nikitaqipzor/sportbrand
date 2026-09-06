package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"athletica.ai/api/internal/exercises"
	"athletica.ai/api/internal/store"
	"athletica.ai/api/seed"
)

// seedStarter loads the embedded starter catalogue into the harness's store —
// the same file `api seed-exercises` loads, through the same parser, so the
// tests exercise what actually ships.
func seedStarter(t *testing.T, h *harness) {
	t.Helper()
	parsed, err := exercises.ParseSeed(seed.StarterSource, seed.StarterExercises)
	if err != nil {
		t.Fatalf("parse the starter set: %v", err)
	}
	if _, err := h.store.SeedExercises(context.Background(), parsed); err != nil {
		t.Fatalf("seed the starter set: %v", err)
	}
}

// seedExtra adds one record to a store that already holds the starter set.
func seedExtra(t *testing.T, h *harness, mutate func(*store.Exercise)) {
	t.Helper()
	parsed, err := exercises.ParseSeed(seed.StarterSource, seed.StarterExercises)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	extra := parsed.Exercises[0]
	extra.ID = "secret-lift"
	extra.Slug = "secret-lift"
	extra.LegacyNumber = nil
	extra.NameRu = "Черновик упражнения"
	extra.SortKey = exercises.SortKey(extra.NameRu, extra.ID)
	extra.SearchText = exercises.SearchText(extra.NameRu, "", nil, extra.ID)
	extra.ContentHash = parsed.Exercises[1].ContentHash
	mutate(&extra)
	parsed.Exercises = append(parsed.Exercises, extra)
	if _, err := h.store.SeedExercises(context.Background(), parsed); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

type exerciseListBody struct {
	Items []struct {
		ID               string   `json:"id"`
		NameRu           string   `json:"nameRu"`
		Sport            string   `json:"sport"`
		Section          string   `json:"section"`
		Difficulty       *string  `json:"difficulty"`
		MovementPattern  *string  `json:"movementPattern"`
		Laterality       *string  `json:"laterality"`
		Equipment        []string `json:"equipment"`
		PrimaryMuscles   []string `json:"primaryMuscles"`
		SecondaryMuscles []string `json:"secondaryMuscles"`
		Joints           []string `json:"joints"`
		GoalTags         []string `json:"goalTags"`
		HasTechnique     bool     `json:"hasTechnique"`
		HasSafety        bool     `json:"hasSafety"`
	} `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

func decodeList(t *testing.T, res response) exerciseListBody {
	t.Helper()
	var body exerciseListBody
	if err := json.Unmarshal(res.body, &body); err != nil {
		t.Fatalf("decode %q: %v", res.body, err)
	}
	return body
}

func (h *harness) listExercises(token, query string) response {
	h.t.Helper()
	return h.send(request{method: http.MethodGet, path: basePath + "/exercises" + query, token: token})
}

// The catalogue is shared content, but it is not public: an anonymous request
// gets the same 401 as every other authenticated route.
func TestExerciseEndpointsRequireAToken(t *testing.T) {
	h := newHarness(t, nil)
	seedStarter(t, h)

	for _, path := range []string{"/exercises", "/exercises/back-squat", "/exercise-dictionaries"} {
		res := h.send(request{method: http.MethodGet, path: basePath + path})
		if res.status != http.StatusUnauthorized {
			t.Fatalf("GET %s without a token: status %d, want 401", path, res.status)
		}
	}
}

// The catalogue is the same for everybody: two accounts see the same rows.
func TestCatalogueIsSharedBetweenAccounts(t *testing.T) {
	h := newHarness(t, nil)
	seedStarter(t, h)
	alice := h.register("alice@example.com", "correct-horse-battery")
	bob := h.register("bob@example.com", "correct-horse-battery")

	first := decodeList(t, h.listExercises(alice.accessToken, "?limit=200"))
	second := decodeList(t, h.listExercises(bob.accessToken, "?limit=200"))
	if len(first.Items) != 20 || len(first.Items) != len(second.Items) {
		t.Fatalf("alice saw %d rows, bob %d; the catalogue belongs to nobody", len(first.Items), len(second.Items))
	}
	for i := range first.Items {
		if first.Items[i].ID != second.Items[i].ID {
			t.Fatalf("row %d differs between accounts: %q vs %q", i, first.Items[i].ID, second.Items[i].ID)
		}
	}
}

func TestExerciseFiltersAndSearch(t *testing.T) {
	h := newHarness(t, nil)
	seedStarter(t, h)
	user := h.register("athlete@example.com", "correct-horse-battery")

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"by section", "?section=core", []string{"plank"}},
		{"by two sections, repeated parameter", "?section=core&section=arms",
			[]string{"biceps-curl", "plank", "triceps-pushdown"}},
		{"by two sections, comma separated", "?section=core,arms",
			[]string{"biceps-curl", "plank", "triceps-pushdown"}},
		{"by equipment", "?equipment=machine", []string{"leg-curl", "leg-press"}},
		{"by muscle", "?muscle=biceps", []string{"biceps-curl"}},
		{"by sport", "?sport=strength", nil}, // all twenty; checked by count below
		{"section and equipment together", "?section=back&equipment=cable",
			[]string{"seated-row", "lat-pulldown"}},
		{"an unknown code matches nothing rather than failing", "?equipment=jetpack", []string{}},
		// "жим" is a substring, not a word: it also matches "Отжимания на
		// брусьях", which is right — a person typing it wants that too.
		{"search", "?q=%D0%B6%D0%B8%D0%BC",
			[]string{"bench-press", "leg-press", "overhead-press", "incline-bench-press", "dip"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := h.listExercises(user.accessToken, tc.query+"&limit=200")
			if res.status != http.StatusOK {
				t.Fatalf("status %d, body %s", res.status, res.body)
			}
			body := decodeList(t, res)
			if tc.want == nil {
				if len(body.Items) != 20 {
					t.Fatalf("got %d rows, want all 20", len(body.Items))
				}
				return
			}
			got := map[string]bool{}
			for _, item := range body.Items {
				got[item.ID] = true
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d rows (%v), want %d (%v)", len(got), keysOf(got), len(tc.want), tc.want)
			}
			for _, id := range tc.want {
				if !got[id] {
					t.Fatalf("%q is missing from %v", id, keysOf(got))
				}
			}
		})
	}
}

// A search box sends whatever a person types. None of it may 500, and none of
// it may be read as a pattern.
func TestSearchSurvivesEmptyAndHostileInput(t *testing.T) {
	h := newHarness(t, nil)
	seedStarter(t, h)
	user := h.register("athlete@example.com", "correct-horse-battery")

	full := decodeList(t, h.listExercises(user.accessToken, "?limit=200"))
	if len(full.Items) != 20 {
		t.Fatalf("the unfiltered catalogue has %d rows, want 20", len(full.Items))
	}

	// An empty or whitespace-only query is not a filter: it is the search box
	// while a person is deleting what they typed.
	for _, query := range []string{"?q=", "?q=%20%20", "?q=%09"} {
		res := h.listExercises(user.accessToken, query+"&limit=200")
		if res.status != http.StatusOK {
			t.Fatalf("q=%q: status %d, body %s", query, res.status, res.body)
		}
		if got := len(decodeList(t, res).Items); got != 20 {
			t.Fatalf("q=%q narrowed the catalogue to %d rows; an empty query must not filter", query, got)
		}
	}

	// Every one of these is an ordinary character in a name, and none of them
	// matches anything in the starter set.
	hostile := []string{
		"%25", // %
		"_",   // _
		"%5C", // \
		"%27", // '
		"%27%3B%20DROP%20TABLE%20exercise%3B%20--",
		"%25%D0%B6%D0%B8%D0%BC%25", // %жим%
		"%00",
		"%D1%8F" + "%D1%8F%D1%8F%D1%8F%D1%8F", // just letters that match nothing
	}
	for _, query := range hostile {
		res := h.listExercises(user.accessToken, "?q="+query+"&limit=200")
		if res.status != http.StatusOK {
			t.Fatalf("q=%s: status %d, body %s", query, res.status, res.body)
		}
		if got := len(decodeList(t, res).Items); got != 0 {
			t.Fatalf("q=%s matched %d rows; it must be a literal that matches nothing", query, got)
		}
	}

	// A query far longer than any name is truncated, not rejected.
	long := "?q="
	for range 400 {
		long += "%D1%91"
	}
	if res := h.listExercises(user.accessToken, long); res.status != http.StatusOK {
		t.Fatalf("a very long query answered %d, want 200", res.status)
	}
}

// Paging must not lose a row and must not show one twice, at every page size —
// including one that lands exactly on the last row.
func TestExercisePagingLosesAndRepeatsNothing(t *testing.T) {
	h := newHarness(t, nil)
	seedStarter(t, h)
	user := h.register("athlete@example.com", "correct-horse-battery")

	unpaged := decodeList(t, h.listExercises(user.accessToken, "?limit=200"))
	want := make([]string, 0, len(unpaged.Items))
	for _, item := range unpaged.Items {
		want = append(want, item.ID)
	}
	if len(want) != 20 {
		t.Fatalf("the catalogue has %d rows, want 20", len(want))
	}
	if unpaged.NextCursor != nil {
		t.Fatal("a page holding everything must not offer a next cursor")
	}

	for _, size := range []int{1, 2, 3, 7, 19, 20, 21} {
		t.Run("pageSize="+itoa(size), func(t *testing.T) {
			var (
				seen   []string
				cursor string
			)
			for page := 0; page < 100; page++ {
				query := "?limit=" + itoa(size)
				if cursor != "" {
					query += "&cursor=" + cursor
				}
				res := h.listExercises(user.accessToken, query)
				if res.status != http.StatusOK {
					t.Fatalf("page %d: status %d, body %s", page, res.status, res.body)
				}
				body := decodeList(t, res)
				if len(body.Items) > size {
					t.Fatalf("page %d returned %d rows, more than limit=%d", page, len(body.Items), size)
				}
				for _, item := range body.Items {
					seen = append(seen, item.ID)
				}
				if body.NextCursor == nil {
					break
				}
				cursor = *body.NextCursor
			}
			if len(seen) != len(want) {
				t.Fatalf("paging saw %d rows, want %d: %v", len(seen), len(want), seen)
			}
			for i := range want {
				if seen[i] != want[i] {
					t.Fatalf("paged order diverged at %d: %q vs %q", i, seen[i], want[i])
				}
			}
		})
	}

	// A cursor this API did not issue is a 400 with its own code, never a 500
	// and never a silently different page.
	res := h.listExercises(user.accessToken, "?cursor=not-a-cursor%21")
	if res.status != http.StatusBadRequest || res.str(t, "error", "code") != "invalid_cursor" {
		t.Fatalf("a forged cursor answered %d %s", res.status, res.body)
	}
	// A limit outside the contract is refused rather than silently clamped.
	if res := h.listExercises(user.accessToken, "?limit=100000"); res.status != http.StatusBadRequest {
		t.Fatalf("limit=100000 answered %d, want 400", res.status)
	}
	if res := h.listExercises(user.accessToken, "?limit=abc"); res.status != http.StatusBadRequest {
		t.Fatalf("limit=abc answered %d, want 400", res.status)
	}
}

// An unpublished record is invisible, and invisible means indistinguishable
// from an identifier the catalogue has never held.
func TestUnpublishedExercisesAreNotServed(t *testing.T) {
	h := newHarness(t, nil)
	user := h.register("athlete@example.com", "correct-horse-battery")

	for _, tc := range []struct {
		name                       string
		publication, review, media string
	}{
		{"a draft", store.PublicationDraft, store.ReviewDraft, store.MediaDraft},
		{"ready but unreviewed", store.PublicationReady, store.ReviewInReview, store.MediaApproved},
		{"ready with unapproved media", store.PublicationReady, store.ReviewApproved, store.MediaDraft},
		{"reviewed and approved but not published", store.PublicationReady, store.ReviewApproved, store.MediaApproved},
		{"archived", store.PublicationArchived, store.ReviewApproved, store.MediaApproved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			user := h.register("athlete@example.com", "correct-horse-battery")
			seedExtra(t, h, func(e *store.Exercise) {
				e.PublicationStatus = tc.publication
				e.ReviewStatus = tc.review
				e.MediaStatus = tc.media
				e.Published = false
			})

			body := decodeList(t, h.listExercises(user.accessToken, "?limit=200"))
			for _, item := range body.Items {
				if item.ID == "secret-lift" {
					t.Fatal("an unpublished record appeared in the catalogue")
				}
			}
			if len(body.Items) != 20 {
				t.Fatalf("the catalogue has %d rows, want the 20 published ones", len(body.Items))
			}

			res := h.send(request{method: http.MethodGet, path: basePath + "/exercises/secret-lift", token: user.accessToken})
			if res.status != http.StatusNotFound {
				t.Fatalf("the card answered %d, want 404", res.status)
			}
			missing := h.send(request{method: http.MethodGet, path: basePath + "/exercises/never-existed", token: user.accessToken})
			if string(res.body) != string(missing.body) {
				t.Fatalf("an unpublished record answers %q while a missing one answers %q; the two must be identical",
					res.body, missing.body)
			}

			// Searching for it by name does not reveal it either.
			found := decodeList(t, h.listExercises(user.accessToken, "?q=%D1%87%D0%B5%D1%80%D0%BD%D0%BE%D0%B2%D0%B8%D0%BA&limit=200"))
			if len(found.Items) != 0 {
				t.Fatalf("search revealed an unpublished record: %v", found.Items)
			}
		})
	}

	seedStarter(t, h)
	if res := h.send(request{method: http.MethodGet, path: basePath + "/exercises/back-squat", token: user.accessToken}); res.status != http.StatusOK {
		t.Fatalf("a published record must be served: %d %s", res.status, res.body)
	}
}

// The card behind prototype screens 14 and 15. It carries the three statuses
// and empty technique and safety blocks — and saying so honestly is the point.
func TestExerciseCard(t *testing.T) {
	h := newHarness(t, nil)
	seedStarter(t, h)
	user := h.register("athlete@example.com", "correct-horse-battery")

	res := h.send(request{method: http.MethodGet, path: basePath + "/exercises/back-squat", token: user.accessToken})
	if res.status != http.StatusOK {
		t.Fatalf("status %d, body %s", res.status, res.body)
	}
	var card struct {
		ID        string `json:"id"`
		NameRu    string `json:"nameRu"`
		Sport     string `json:"sport"`
		Technique struct {
			ExecutionSteps []string `json:"executionSteps"`
			KeyCues        []string `json:"keyCues"`
			Setup          string   `json:"setup"`
		} `json:"technique"`
		Safety struct {
			CommonErrors      []string `json:"commonErrors"`
			Contraindications []string `json:"contraindications"`
		} `json:"safety"`
		PublicationStatus string `json:"publicationStatus"`
		ReviewStatus      string `json:"reviewStatus"`
		MediaStatus       string `json:"mediaStatus"`
		HasTechnique      bool   `json:"hasTechnique"`
		HasSafety         bool   `json:"hasSafety"`
	}
	if err := json.Unmarshal(res.body, &card); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if card.ID != "back-squat" || card.NameRu != "Приседания со штангой" {
		t.Fatalf("card = %+v", card)
	}
	if card.PublicationStatus != store.PublicationPublished ||
		card.ReviewStatus != store.ReviewApproved || card.MediaStatus != store.MediaApproved {
		t.Fatalf("a served card must be published and approved: %+v", card)
	}
	// Absent guidance is rendered as an empty list, never as null and never as
	// invented text: the screen must be able to say "нет данных".
	if card.Technique.ExecutionSteps == nil || len(card.Technique.ExecutionSteps) != 0 {
		t.Fatalf("executionSteps = %v, want an empty array", card.Technique.ExecutionSteps)
	}
	if card.Safety.Contraindications == nil || len(card.Safety.Contraindications) != 0 {
		t.Fatalf("contraindications = %v, want an empty array", card.Safety.Contraindications)
	}
	if card.HasTechnique || card.HasSafety {
		t.Fatal("the starter set claims technique or safety it does not have")
	}

	if res := h.send(request{method: http.MethodGet, path: basePath + "/exercises/no-such-lift", token: user.accessToken}); res.status != http.StatusNotFound {
		t.Fatalf("an unknown identifier answered %d, want 404", res.status)
	}
}

// The dictionary endpoint is what lets the client stop hard-coding filters, so
// every code any answer carries must appear in it.
func TestEveryCodeInAnAnswerExistsInTheDictionaries(t *testing.T) {
	h := newHarness(t, nil)
	seedStarter(t, h)
	user := h.register("athlete@example.com", "correct-horse-battery")

	res := h.send(request{method: http.MethodGet, path: basePath + "/exercise-dictionaries", token: user.accessToken})
	if res.status != http.StatusOK {
		t.Fatalf("status %d, body %s", res.status, res.body)
	}
	var dictionaries struct {
		Dictionaries []struct {
			Kind  string `json:"kind"`
			Items []struct {
				Code   string `json:"code"`
				NameRu string `json:"nameRu"`
			} `json:"items"`
		} `json:"dictionaries"`
	}
	if err := json.Unmarshal(res.body, &dictionaries); err != nil {
		t.Fatalf("decode: %v", err)
	}

	known := map[string]bool{}
	kinds := map[string]bool{}
	for _, dictionary := range dictionaries.Dictionaries {
		if !store.IsCodeKind(dictionary.Kind) {
			t.Fatalf("dictionary %q is not a known kind", dictionary.Kind)
		}
		kinds[dictionary.Kind] = true
		for _, item := range dictionary.Items {
			if item.NameRu == "" {
				t.Fatalf("%s/%s has no Russian name; the filter would show a machine token", dictionary.Kind, item.Code)
			}
			known[dictionary.Kind+"/"+item.Code] = true
		}
	}
	// Every kind is present, empty ones included, so a client can tell "no
	// values yet" from "no such filter".
	for _, kind := range store.CodeKinds {
		if !kinds[kind] {
			t.Fatalf("the dictionary endpoint omits %q entirely", kind)
		}
	}
	// The difficulty vocabulary ships even though the starter set uses none of
	// it: the client builds the filter now, the encyclopedia fills it later.
	if !known[store.CodeDifficulty+"/beginner"] {
		t.Fatal("the difficulty dictionary is missing, so the client cannot build that filter")
	}

	body := decodeList(t, h.listExercises(user.accessToken, "?limit=200"))
	for _, item := range body.Items {
		check := func(kind, code string) {
			if code == "" {
				return
			}
			if !known[kind+"/"+code] {
				t.Fatalf("%s carries %s=%q, which the dictionary endpoint does not return", item.ID, kind, code)
			}
		}
		check(store.CodeSport, item.Sport)
		check(store.CodeSection, item.Section)
		for _, code := range item.Equipment {
			check(store.CodeEquipment, code)
		}
		for _, code := range append(item.PrimaryMuscles, item.SecondaryMuscles...) {
			check(store.CodeMuscle, code)
		}
		for _, code := range item.Joints {
			check(store.CodeJoint, code)
		}
		for _, code := range item.GoalTags {
			check(store.CodeGoalTag, code)
		}
		if item.Difficulty != nil {
			check(store.CodeDifficulty, *item.Difficulty)
		}
		if item.MovementPattern != nil {
			check(store.CodeMovementPattern, *item.MovementPattern)
		}
		if item.Laterality != nil {
			check(store.CodeLaterality, *item.Laterality)
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
