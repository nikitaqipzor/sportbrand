package exercises_test

import (
	"strings"
	"testing"

	"athletica.ai/api/internal/exercises"
	"athletica.ai/api/internal/store"
)

func TestCursorRoundTrip(t *testing.T) {
	// A sort key holds a NUL separator and Cyrillic; the encoding must survive
	// both, and must survive being put in a URL.
	original := store.ExerciseCursor{SortKey: "приседания со штангой\x00back-squat", ID: "back-squat"}
	encoded := exercises.EncodeCursor(original)
	if strings.ContainsAny(encoded, "+/=") {
		t.Fatalf("cursor %q is not URL-safe", encoded)
	}
	decoded, err := exercises.DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded != original {
		t.Fatalf("round trip produced %+v, want %+v", decoded, original)
	}
}

func TestDecodeCursorRefusesWhatThisAPIDidNotIssue(t *testing.T) {
	for _, raw := range []string{
		"not base64!!",
		"YWJj",   // decodes, but has no separator
		"AGFiYw", // an empty sort key and an id is fine, but…
		exercises.EncodeCursor(store.ExerciseCursor{SortKey: "x", ID: strings.Repeat("a", 200)}),
	} {
		if _, err := exercises.DecodeCursor(raw); err == nil {
			t.Fatalf("cursor %q was accepted", raw)
		}
	}
}

// A search box sends whatever a person types, including nothing at all and
// including characters that are operators in some other query language.
func TestNormalizeSearch(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"\t\n", ""},
		{"Приседания", "приседания"},
		{"  ЖИМ   ЛЁЖА  ", "жим лёжа"},
		{"%", "%"},
		{"_", "_"},
		{`\`, `\`},
		{"'; DROP TABLE exercise; --", "'; drop table exercise; --"},
	}
	for _, tc := range cases {
		if got := exercises.NormalizeSearch(tc.in); got != tc.want {
			t.Fatalf("NormalizeSearch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// A stuck key must not produce an error screen; the query is simply cut,
	// and it is cut at a rune boundary so it stays valid text.
	long := exercises.NormalizeSearch(strings.Repeat("ё", 500))
	if len(long) > exercises.MaxSearchLen {
		t.Fatalf("a very long query was not bounded: %d bytes", len(long))
	}
	if !isValidUTF8(long) {
		t.Fatal("truncation cut a rune in half")
	}
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}

func TestNormalizeCodes(t *testing.T) {
	got := exercises.NormalizeCodes([]string{"Barbell, CABLE", " barbell ", "", ",,", "bodyweight"})
	want := []string{"barbell", "cable", "bodyweight"}
	if len(got) != len(want) {
		t.Fatalf("NormalizeCodes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeCodes = %v, want %v", got, want)
		}
	}

	// A caller cannot push an unbounded array into the query.
	many := make([]string, 500)
	for i := range many {
		many[i] = "code" + string(rune('a'+i%26))
	}
	if bounded := exercises.NormalizeCodes(many); len(bounded) > exercises.MaxFilterValues {
		t.Fatalf("a filter accepted %d values", len(bounded))
	}
}
