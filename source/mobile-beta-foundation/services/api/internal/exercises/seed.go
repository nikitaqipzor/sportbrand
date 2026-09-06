package exercises

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"athletica.ai/api/internal/store"
)

// SeedSchemaVersion is the import contract this build understands. A file
// declaring a newer major shape is refused rather than half-read.
const SeedSchemaVersion = 1

// identifierPattern is the shape of an EXERCISE_ID, a SLUG and every machine
// code: lowercase ASCII words joined by single hyphens. It mirrors the CHECK
// constraints in migration 0005.
var identifierPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// codePattern additionally allows an underscore, because the master template's
// own code vocabulary uses one (`movement_pattern`, `goal_tag`).
var codePattern = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)

// seedFile is the wire form of the JSON contract produced in content/.
//
// Field names are the master template's machine names (EXERCISE_ID, NAME_RU,
// PUBLICATION_STATUS …). They are matched case-insensitively and ignoring
// separators, so `EXERCISE_ID`, `exercise_id` and `exerciseId` are the same
// field: the content pipeline is being written in parallel with this importer
// and must not be able to miss by a convention.
type seedFile struct {
	SchemaVersion  int                   `json:"schemaversion"`
	ContentVersion int                   `json:"contentversion"`
	ContentLocale  string                `json:"contentlocale"`
	GeneratedAt    string                `json:"generatedat"`
	Dictionaries   map[string][]seedCode `json:"dictionaries"`
	Exercises      []map[string]any      `json:"exercises"`
}

type seedCode struct {
	Code      string `json:"code"`
	NameRu    string `json:"nameru"`
	NameEn    string `json:"nameen"`
	SortOrder int    `json:"sortorder"`
}

// seedExercise is one card, all seven blocks of the master template.
type seedExercise struct {
	// A. Identification
	ExerciseID     string   `json:"exerciseid"`
	Slug           string   `json:"slug"`
	LegacyNumber   *int     `json:"legacynumber"`
	SchemaVersion  int      `json:"schemaversion"`
	ContentVersion int      `json:"contentversion"`
	ContentLocale  string   `json:"contentlocale"`
	NameRu         string   `json:"nameru"`
	NameEn         string   `json:"nameen"`
	Aliases        []string `json:"aliases"`
	VariantOf      string   `json:"variantof"`

	// B. Classification and anatomy
	Sport            string   `json:"sport"`
	Section          string   `json:"section"`
	Category         string   `json:"category"`
	MovementPattern  string   `json:"movementpattern"`
	GoalTags         []string `json:"goaltags"`
	Equipment        []string `json:"equipment"`
	Difficulty       string   `json:"difficulty"`
	PrimaryMuscles   []string `json:"primarymuscles"`
	SecondaryMuscles []string `json:"secondarymuscles"`
	Joints           []string `json:"joints"`
	Laterality       string   `json:"laterality"`

	// C. Technique — absent from the source encyclopedia.
	Setup          string   `json:"setup"`
	StartPosition  string   `json:"startposition"`
	ExecutionSteps []string `json:"executionsteps"`
	KeyCues        []string `json:"keycues"`
	Breathing      string   `json:"breathing"`
	Tempo          string   `json:"tempo"`
	RangeOfMotion  string   `json:"rangeofmotion"`
	FinishReturn   string   `json:"finishreturn"`

	// D. Programming
	VolumeType         string   `json:"volumetype"`
	SetsMin            *int     `json:"setsmin"`
	SetsMax            *int     `json:"setsmax"`
	RepsMin            *int     `json:"repsmin"`
	RepsMax            *int     `json:"repsmax"`
	DurationMinSeconds *int     `json:"durationminseconds"`
	DurationMaxSeconds *int     `json:"durationmaxseconds"`
	DistanceMinMeters  *float64 `json:"distanceminmeters"`
	DistanceMaxMeters  *float64 `json:"distancemaxmeters"`
	CyclesMin          *int     `json:"cyclesmin"`
	CyclesMax          *int     `json:"cyclesmax"`
	RestSeconds        *int     `json:"restseconds"`
	IntensityType      string   `json:"intensitytype"`
	IntensityMin       *float64 `json:"intensitymin"`
	IntensityMax       *float64 `json:"intensitymax"`
	StopCondition      string   `json:"stopcondition"`

	// E. Safety — absent from the source encyclopedia.
	CommonErrors      []string `json:"commonerrors"`
	StopSigns         []string `json:"stopsigns"`
	Contraindications []string `json:"contraindications"`
	Regressions       []string `json:"regressions"`
	Progressions      []string `json:"progressions"`
	InjuryNotes       string   `json:"injurynotes"`

	// F. Media
	MainAssetID        string   `json:"mainassetid"`
	PhaseAssetIDs      []string `json:"phaseassetids"`
	MuscleLayerAssetID string   `json:"musclelayerassetid"`
	AnimationAssetID   string   `json:"animationassetid"`
	VideoURL           string   `json:"videourl"`
	CameraView         string   `json:"cameraview"`
	CropFocalPoint     string   `json:"cropfocalpoint"`
	AltText            string   `json:"alttext"`
	MediaRights        string   `json:"mediarights"`
	TechniqueVersion   string   `json:"techniqueversion"`
	MediaStatus        string   `json:"mediastatus"`

	// G. QA and provenance
	Sources         []string `json:"sources"`
	Reviewers       []string `json:"reviewers"`
	AuthorID        string   `json:"authorid"`
	EditorID        string   `json:"editorid"`
	ReviewStatus    string   `json:"reviewstatus"`
	ReviewedAt      string   `json:"reviewedat"`
	ReviewNotes     string   `json:"reviewnotes"`
	RejectionReason string   `json:"rejectionreason"`

	PublicationStatus string `json:"publicationstatus"`
}

// SeedError collects everything wrong with a file. An import either applies
// whole or is refused whole, so reporting one problem at a time would make a
// 918-record file take 918 runs to fix.
type SeedError struct {
	Source   string
	Problems []string
}

func (e *SeedError) Error() string {
	head := "exercise import file is invalid"
	if e.Source != "" {
		head += " (" + e.Source + ")"
	}
	return head + ":\n  - " + strings.Join(e.Problems, "\n  - ")
}

// ParseSeed reads one import file into the store's seed form, validating it
// against the master template's rules.
//
// It never touches the database: everything it can decide from the file alone
// is decided here, and the two checks that need stored state — the rename guard
// and the dictionary check — happen inside the store's transaction.
func ParseSeed(source string, raw []byte) (store.ExerciseSeed, error) {
	sum := sha256.Sum256(raw)

	normalized, err := normalizeKeys(raw)
	if err != nil {
		return store.ExerciseSeed{}, &SeedError{Source: source, Problems: []string{err.Error()}}
	}
	var file seedFile
	decoder := json.NewDecoder(strings.NewReader(string(normalized)))
	if err := decoder.Decode(&file); err != nil {
		return store.ExerciseSeed{}, &SeedError{Source: source, Problems: []string{"not a JSON object matching the import contract: " + err.Error()}}
	}

	problems := []string{}
	if file.SchemaVersion == 0 {
		file.SchemaVersion = SeedSchemaVersion
	}
	if file.SchemaVersion != SeedSchemaVersion {
		problems = append(problems, fmt.Sprintf("schemaVersion is %d, this build reads %d", file.SchemaVersion, SeedSchemaVersion))
	}
	if file.ContentVersion <= 0 {
		problems = append(problems, "contentVersion is required and must be a positive integer")
	}
	if strings.TrimSpace(file.ContentLocale) == "" {
		file.ContentLocale = "ru-RU"
	}
	if len(file.Exercises) == 0 {
		problems = append(problems, "the file carries no exercises")
	}

	seed := store.ExerciseSeed{
		Source:         source,
		FileSHA256:     hex.EncodeToString(sum[:]),
		SchemaVersion:  file.SchemaVersion,
		ContentVersion: file.ContentVersion,
		ContentLocale:  strings.TrimSpace(file.ContentLocale),
	}

	kinds := make([]string, 0, len(file.Dictionaries))
	for kind := range file.Dictionaries {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		if !store.IsCodeKind(kind) {
			problems = append(problems, fmt.Sprintf("dictionaries.%s is not one of %s", kind, strings.Join(store.CodeKinds, ", ")))
			continue
		}
		for i, entry := range file.Dictionaries[kind] {
			code := strings.ToLower(strings.TrimSpace(entry.Code))
			switch {
			case code == "":
				problems = append(problems, fmt.Sprintf("dictionaries.%s[%d] has no code", kind, i))
				continue
			case !codePattern.MatchString(code) || len(code) > MaxCodeLen:
				problems = append(problems, fmt.Sprintf("dictionaries.%s[%d]: code %q must be lowercase words joined by - or _", kind, i, entry.Code))
				continue
			}
			name := strings.TrimSpace(entry.NameRu)
			if name == "" {
				// The whole point of a dictionary is that the app filters by
				// code and shows a person a name. A code with no name would put
				// a machine token on the screen.
				problems = append(problems, fmt.Sprintf("dictionaries.%s[%d] (%s) has no nameRu", kind, i, code))
				continue
			}
			seed.Codes = append(seed.Codes, store.ExerciseCode{
				Kind: kind, Code: code, NameRu: name,
				NameEn: strings.TrimSpace(entry.NameEn), SortOrder: entry.SortOrder,
			})
		}
	}

	seenID := map[string]int{}
	seenSlug := map[string]string{}
	seenLegacy := map[int]string{}
	ids := map[string]bool{}

	for i, rawExercise := range file.Exercises {
		encoded, err := json.Marshal(rawExercise)
		if err != nil {
			problems = append(problems, fmt.Sprintf("exercises[%d] cannot be re-encoded: %v", i, err))
			continue
		}
		var in seedExercise
		if err := json.Unmarshal(encoded, &in); err != nil {
			problems = append(problems, fmt.Sprintf("exercises[%d] does not match the card contract: %v", i, err))
			continue
		}

		exercise, itemProblems := convertExercise(i, in, encoded)
		problems = append(problems, itemProblems...)
		if len(itemProblems) > 0 {
			continue
		}

		if first, dup := seenID[exercise.ID]; dup {
			problems = append(problems, fmt.Sprintf("exercises[%d]: EXERCISE_ID %q was already used by exercises[%d]", i, exercise.ID, first))
			continue
		}
		seenID[exercise.ID] = i
		ids[exercise.ID] = true

		if owner, dup := seenSlug[exercise.Slug]; dup {
			problems = append(problems, fmt.Sprintf("exercises[%d]: SLUG %q is already the slug of %q inside this file", i, exercise.Slug, owner))
		}
		seenSlug[exercise.Slug] = exercise.ID
		if exercise.LegacyNumber != nil {
			if owner, dup := seenLegacy[*exercise.LegacyNumber]; dup {
				problems = append(problems, fmt.Sprintf("exercises[%d]: LEGACY_NUMBER %d is already used by %q inside this file", i, *exercise.LegacyNumber, owner))
			}
			seenLegacy[*exercise.LegacyNumber] = exercise.ID
		}

		if exercise.SchemaVersion == 0 {
			exercise.SchemaVersion = file.SchemaVersion
		}
		if exercise.ContentVersion == 0 {
			exercise.ContentVersion = file.ContentVersion
		}
		if exercise.ContentLocale == "" {
			exercise.ContentLocale = seed.ContentLocale
		}
		seed.Exercises = append(seed.Exercises, exercise)
	}

	// VARIANT_OF must point at a record. Within a file that is checkable here;
	// pointing at a record only the database holds is caught by the foreign key.
	for _, exercise := range seed.Exercises {
		if exercise.VariantOf == nil {
			continue
		}
		if *exercise.VariantOf == exercise.ID {
			problems = append(problems, fmt.Sprintf("%s: VARIANT_OF points at itself", exercise.ID))
		}
	}

	if len(problems) > 0 {
		return store.ExerciseSeed{}, &SeedError{Source: source, Problems: problems}
	}
	return seed, nil
}

// convertExercise turns one decoded card into a store row, or reports what is
// wrong with it.
//
// `encoded` is the record exactly as the file stated it, and its SHA-256 is the
// content hash: a re-import of an unchanged record compares equal and is
// skipped, which is where the importer's idempotence comes from.
func convertExercise(index int, in seedExercise, encoded []byte) (store.Exercise, []string) {
	var problems []string
	fail := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf("exercises[%d]: ", index)+fmt.Sprintf(format, args...))
	}

	id := strings.ToLower(strings.TrimSpace(in.ExerciseID))
	switch {
	case id == "":
		fail("EXERCISE_ID is required — it is the string already stored inside recorded sets")
	case len(id) > MaxCodeLen:
		fail("EXERCISE_ID %q is longer than %d characters", id, MaxCodeLen)
	case !identifierPattern.MatchString(id):
		fail("EXERCISE_ID %q must be lowercase words joined by single hyphens", in.ExerciseID)
	}

	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if slug == "" {
		slug = id
	}
	if slug != "" && !identifierPattern.MatchString(slug) {
		fail("SLUG %q must be lowercase words joined by single hyphens", in.Slug)
	}
	if in.LegacyNumber != nil && *in.LegacyNumber <= 0 {
		fail("LEGACY_NUMBER must be positive, got %d", *in.LegacyNumber)
	}
	if strings.TrimSpace(in.NameRu) == "" {
		fail("NAME_RU is required — the app filters by code and shows a person a name")
	}

	sport := strings.ToLower(strings.TrimSpace(in.Sport))
	section := strings.ToLower(strings.TrimSpace(in.Section))
	if sport == "" {
		fail("SPORT is required")
	}
	if section == "" {
		fail("SECTION is required")
	}

	publication := defaultStatus(in.PublicationStatus, store.PublicationDraft)
	review := defaultStatus(in.ReviewStatus, store.ReviewDraft)
	media := defaultStatus(in.MediaStatus, store.MediaDraft)
	if !oneOf(publication, store.PublicationDraft, store.PublicationInReview, store.PublicationReady, store.PublicationPublished, store.PublicationArchived) {
		fail("PUBLICATION_STATUS %q is not part of draft → in_review → ready → published → archived", in.PublicationStatus)
	}
	if !oneOf(review, store.ReviewDraft, store.ReviewInReview, store.ReviewApproved, store.ReviewRejected) {
		fail("REVIEW_STATUS %q is not one of draft, in_review, approved, rejected", in.ReviewStatus)
	}
	if !oneOf(media, store.MediaDraft, store.MediaInReview, store.MediaApproved, store.MediaRejected) {
		fail("MEDIA_STATUS %q is not one of draft, in_review, approved, rejected", in.MediaStatus)
	}
	// The master template's rule, refused here as well as in the CHECK
	// constraint: a published record must carry both approvals.
	if publication == store.PublicationPublished && !store.PublicationAllowed(publication, review, media) {
		fail("PUBLICATION_STATUS is published while REVIEW_STATUS=%q and MEDIA_STATUS=%q; publication needs both approved", review, media)
	}

	var reviewedAt *time.Time
	if raw := strings.TrimSpace(in.ReviewedAt); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			fail("REVIEWED_AT %q is not an RFC 3339 timestamp", raw)
		} else {
			utc := parsed.UTC()
			reviewedAt = &utc
		}
	}

	equipment, bad := normalizeCodeList(in.Equipment)
	problems = append(problems, badCodes(index, "EQUIPMENT", bad)...)
	primary, bad := normalizeCodeList(in.PrimaryMuscles)
	problems = append(problems, badCodes(index, "PRIMARY_MUSCLES", bad)...)
	secondary, bad := normalizeCodeList(in.SecondaryMuscles)
	problems = append(problems, badCodes(index, "SECONDARY_MUSCLES", bad)...)
	joints, bad := normalizeCodeList(in.Joints)
	problems = append(problems, badCodes(index, "JOINTS", bad)...)
	goals, bad := normalizeCodeList(in.GoalTags)
	problems = append(problems, badCodes(index, "GOAL_TAGS", bad)...)

	if len(problems) > 0 {
		return store.Exercise{}, problems
	}

	hash := sha256.Sum256(encoded)
	nameRu := strings.TrimSpace(in.NameRu)
	nameEn := strings.TrimSpace(in.NameEn)
	aliases := trimAll(in.Aliases)

	exercise := store.Exercise{
		ID:             id,
		Slug:           slug,
		LegacyNumber:   in.LegacyNumber,
		SchemaVersion:  in.SchemaVersion,
		ContentVersion: in.ContentVersion,
		ContentLocale:  strings.TrimSpace(in.ContentLocale),
		ContentHash:    hex.EncodeToString(hash[:]),
		Revision:       1,
		NameRu:         nameRu,
		NameEn:         nameEn,
		Aliases:        aliases,
		VariantOf:      optionalCode(in.VariantOf),

		Sport:            sport,
		Section:          section,
		Category:         optionalCode(in.Category),
		MovementPattern:  optionalCode(in.MovementPattern),
		Difficulty:       optionalCode(in.Difficulty),
		Laterality:       optionalCode(in.Laterality),
		Equipment:        equipment,
		PrimaryMuscles:   primary,
		SecondaryMuscles: secondary,
		Joints:           joints,
		GoalTags:         goals,

		Technique: store.ExerciseTechnique{
			Setup:          strings.TrimSpace(in.Setup),
			StartPosition:  strings.TrimSpace(in.StartPosition),
			ExecutionSteps: trimAll(in.ExecutionSteps),
			KeyCues:        trimAll(in.KeyCues),
			Breathing:      strings.TrimSpace(in.Breathing),
			Tempo:          strings.TrimSpace(in.Tempo),
			RangeOfMotion:  strings.TrimSpace(in.RangeOfMotion),
			FinishReturn:   strings.TrimSpace(in.FinishReturn),
		},
		Programming: store.ExerciseProgramming{
			VolumeType:         strings.TrimSpace(in.VolumeType),
			SetsMin:            in.SetsMin,
			SetsMax:            in.SetsMax,
			RepsMin:            in.RepsMin,
			RepsMax:            in.RepsMax,
			DurationMinSeconds: in.DurationMinSeconds,
			DurationMaxSeconds: in.DurationMaxSeconds,
			DistanceMinMeters:  in.DistanceMinMeters,
			DistanceMaxMeters:  in.DistanceMaxMeters,
			CyclesMin:          in.CyclesMin,
			CyclesMax:          in.CyclesMax,
			RestSeconds:        in.RestSeconds,
			IntensityType:      strings.TrimSpace(in.IntensityType),
			IntensityMin:       in.IntensityMin,
			IntensityMax:       in.IntensityMax,
			StopCondition:      strings.TrimSpace(in.StopCondition),
		},
		Safety: store.ExerciseSafety{
			CommonErrors:      trimAll(in.CommonErrors),
			StopSigns:         trimAll(in.StopSigns),
			Contraindications: trimAll(in.Contraindications),
			Regressions:       trimAll(in.Regressions),
			Progressions:      trimAll(in.Progressions),
			InjuryNotes:       strings.TrimSpace(in.InjuryNotes),
		},
		Media: store.ExerciseMedia{
			MainAssetID:        strings.TrimSpace(in.MainAssetID),
			PhaseAssetIDs:      trimAll(in.PhaseAssetIDs),
			MuscleLayerAssetID: strings.TrimSpace(in.MuscleLayerAssetID),
			AnimationAssetID:   strings.TrimSpace(in.AnimationAssetID),
			VideoURL:           strings.TrimSpace(in.VideoURL),
			CameraView:         strings.TrimSpace(in.CameraView),
			CropFocalPoint:     strings.TrimSpace(in.CropFocalPoint),
			AltText:            strings.TrimSpace(in.AltText),
			MediaRights:        strings.TrimSpace(in.MediaRights),
			TechniqueVersion:   strings.TrimSpace(in.TechniqueVersion),
		},
		QA: store.ExerciseQA{
			Sources:         trimAll(in.Sources),
			Reviewers:       trimAll(in.Reviewers),
			AuthorID:        strings.TrimSpace(in.AuthorID),
			EditorID:        strings.TrimSpace(in.EditorID),
			ReviewedAt:      reviewedAt,
			ReviewNotes:     strings.TrimSpace(in.ReviewNotes),
			RejectionReason: strings.TrimSpace(in.RejectionReason),
		},

		PublicationStatus: publication,
		ReviewStatus:      review,
		MediaStatus:       media,
		Published:         publication == store.PublicationPublished && review == store.ReviewApproved && media == store.MediaApproved,
	}
	exercise.SortKey = SortKey(nameRu, id)
	exercise.SearchText = SearchText(nameRu, nameEn, aliases, id)
	return exercise, nil
}

// SortKey is the catalogue's ordering key: the Russian name folded to lower
// case, with the identifier appended so records that share a name still have a
// deterministic order. It is computed in Go, never in SQL, because PostgreSQL's
// lower() under the C collation would leave Cyrillic untouched and the two
// stores would then disagree about where a page ends.
func SortKey(nameRu, id string) string {
	return strings.ToLower(strings.TrimSpace(nameRu)) + "\x00" + id
}

// SearchText is what `q` matches: every name a person might type, folded to
// lower case and joined. The identifier is included so a developer can find a
// record by the string the phone sends.
func SearchText(nameRu, nameEn string, aliases []string, id string) string {
	parts := []string{nameRu, nameEn, id}
	parts = append(parts, aliases...)
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		folded := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(part))), " ")
		if folded != "" {
			cleaned = append(cleaned, folded)
		}
	}
	return "\n" + strings.Join(cleaned, "\n") + "\n"
}

// normalizeKeys rewrites every object key to lower case with `_` and `-`
// removed, so the master template's UPPER_SNAKE names, snake_case and camelCase
// all decode into the same struct.
func normalizeKeys(raw []byte) ([]byte, error) {
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("not valid JSON: %w", err)
	}
	return json.Marshal(normalizeNode(document, false))
}

// normalizeNode walks the document. `dictionaries` is the one object whose keys
// are data rather than field names — they are dictionary kinds such as
// `movement_pattern` — so its immediate children are only case-folded, never
// stripped of their separators.
func normalizeNode(node any, keysAreData bool) any {
	switch value := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			normalized := key
			if keysAreData {
				normalized = strings.ToLower(strings.TrimSpace(key))
			} else {
				normalized = normalizeKey(key)
			}
			out[normalized] = normalizeNode(child, !keysAreData && normalized == "dictionaries")
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = normalizeNode(child, false)
		}
		return out
	default:
		return node
	}
}

func normalizeKey(key string) string {
	folded := strings.ToLower(key)
	folded = strings.ReplaceAll(folded, "_", "")
	folded = strings.ReplaceAll(folded, "-", "")
	folded = strings.ReplaceAll(folded, " ", "")
	return folded
}

func defaultStatus(raw, fallback string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return fallback
	}
	return value
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// optionalCode returns nil for an absent code, so "the source did not say"
// stays representable and never becomes an empty string masquerading as data.
func optionalCode(raw string) *string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return nil
	}
	return &value
}

func normalizeCodeList(in []string) ([]string, []string) {
	var bad []string
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range in {
		code := strings.ToLower(strings.TrimSpace(raw))
		if code == "" {
			continue
		}
		if !codePattern.MatchString(code) || len(code) > MaxCodeLen {
			bad = append(bad, raw)
			continue
		}
		if seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	return out, bad
}

func badCodes(index int, field string, bad []string) []string {
	out := make([]string, 0, len(bad))
	for _, value := range bad {
		out = append(out, fmt.Sprintf("exercises[%d]: %s contains %q, which is not a machine code", index, field, value))
	}
	return out
}

func trimAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, value := range in {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
