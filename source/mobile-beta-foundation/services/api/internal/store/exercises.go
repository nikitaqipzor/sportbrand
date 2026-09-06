package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Errors specific to the exercise catalogue.
var (
	// ErrExerciseRenamed is returned when an import would move a stable
	// secondary identity — LEGACY_NUMBER or SLUG — from one exercise ID to
	// another. It is refused rather than applied because exercise IDs have
	// already left the phone inside client_mutation_id and are stored in
	// recorded sets: a rename would silently detach history from the exercise
	// it was performed with.
	ErrExerciseRenamed = errors.New("store: import would rename an existing exercise identifier")
	// ErrUnknownExerciseCode is returned when an import uses a machine code no
	// dictionary in the same file (or already in the database) defines. The
	// catalogue must never answer with a code the dictionary endpoint omits.
	ErrUnknownExerciseCode = errors.New("store: exercise uses a code that is not in any dictionary")
)

// Dictionary kinds. Every coded field on an exercise names one of these, and
// every value of such a field is a row of exercise_code.
const (
	CodeSport           = "sport"
	CodeSection         = "section"
	CodeCategory        = "category"
	CodeMovementPattern = "movement_pattern"
	CodeEquipment       = "equipment"
	CodeMuscle          = "muscle"
	CodeJoint           = "joint"
	CodeGoalTag         = "goal_tag"
	CodeDifficulty      = "difficulty"
	CodeLaterality      = "laterality"
)

// CodeKinds is the fixed vocabulary of dictionaries, in the order the
// dictionary endpoint returns them.
var CodeKinds = []string{
	CodeSport, CodeSection, CodeCategory, CodeMovementPattern, CodeEquipment,
	CodeMuscle, CodeJoint, CodeGoalTag, CodeDifficulty, CodeLaterality,
}

// IsCodeKind reports whether kind is one of the ten dictionaries.
func IsCodeKind(kind string) bool {
	for _, known := range CodeKinds {
		if known == kind {
			return true
		}
	}
	return false
}

// Roles a code can play on an exercise. Primary and secondary muscles are two
// roles drawing on the same dictionary, which is why the two are separate.
const (
	RelationEquipment       = "equipment"
	RelationPrimaryMuscle   = "primary_muscle"
	RelationSecondaryMuscle = "secondary_muscle"
	RelationJoint           = "joint"
	RelationGoalTag         = "goal_tag"
)

// RelationKind maps a role to the dictionary its codes come from. It mirrors
// the exercise_code_link_relation_kind CHECK constraint.
var RelationKind = map[string]string{
	RelationEquipment:       CodeEquipment,
	RelationPrimaryMuscle:   CodeMuscle,
	RelationSecondaryMuscle: CodeMuscle,
	RelationJoint:           CodeJoint,
	RelationGoalTag:         CodeGoalTag,
}

// The publication lifecycle of the master template:
// draft → in_review → ready → published → archived.
const (
	PublicationDraft     = "draft"
	PublicationInReview  = "in_review"
	PublicationReady     = "ready"
	PublicationPublished = "published"
	PublicationArchived  = "archived"
)

// Expert review and media readiness are independent of publication and of each
// other; all three must agree before a record is visible.
const (
	ReviewDraft    = "draft"
	ReviewInReview = "in_review"
	ReviewApproved = "approved"
	ReviewRejected = "rejected"
)

const (
	MediaDraft    = "draft"
	MediaInReview = "in_review"
	MediaApproved = "approved"
	MediaRejected = "rejected"
)

// ExerciseCode is one dictionary entry: a machine code plus the localized names
// a person is shown. The app filters on Code and never on a name.
type ExerciseCode struct {
	Kind      string
	Code      string
	NameRu    string
	NameEn    string
	SortOrder int
}

// ExerciseTechnique is block C of the master template. Every field is empty in
// the source encyclopedia; the schema carries them so the content team has
// somewhere to put the real text, and the client shows "нет данных" until then.
type ExerciseTechnique struct {
	Setup          string   `json:"setup,omitempty"`
	StartPosition  string   `json:"startPosition,omitempty"`
	ExecutionSteps []string `json:"executionSteps,omitempty"`
	KeyCues        []string `json:"keyCues,omitempty"`
	Breathing      string   `json:"breathing,omitempty"`
	Tempo          string   `json:"tempo,omitempty"`
	RangeOfMotion  string   `json:"rangeOfMotion,omitempty"`
	FinishReturn   string   `json:"finishReturn,omitempty"`
}

// Empty reports whether the block carries nothing at all.
func (t ExerciseTechnique) Empty() bool {
	return t.Setup == "" && t.StartPosition == "" && len(t.ExecutionSteps) == 0 &&
		len(t.KeyCues) == 0 && t.Breathing == "" && t.Tempo == "" &&
		t.RangeOfMotion == "" && t.FinishReturn == ""
}

// ExerciseProgramming is block D: how much of the exercise to prescribe.
type ExerciseProgramming struct {
	VolumeType         string   `json:"volumeType,omitempty"`
	SetsMin            *int     `json:"setsMin,omitempty"`
	SetsMax            *int     `json:"setsMax,omitempty"`
	RepsMin            *int     `json:"repsMin,omitempty"`
	RepsMax            *int     `json:"repsMax,omitempty"`
	DurationMinSeconds *int     `json:"durationMinSeconds,omitempty"`
	DurationMaxSeconds *int     `json:"durationMaxSeconds,omitempty"`
	DistanceMinMeters  *float64 `json:"distanceMinMeters,omitempty"`
	DistanceMaxMeters  *float64 `json:"distanceMaxMeters,omitempty"`
	CyclesMin          *int     `json:"cyclesMin,omitempty"`
	CyclesMax          *int     `json:"cyclesMax,omitempty"`
	RestSeconds        *int     `json:"restSeconds,omitempty"`
	IntensityType      string   `json:"intensityType,omitempty"`
	IntensityMin       *float64 `json:"intensityMin,omitempty"`
	IntensityMax       *float64 `json:"intensityMax,omitempty"`
	StopCondition      string   `json:"stopCondition,omitempty"`
}

// ExerciseSafety is block E. Like block C it is empty in the source, and for the
// same reason it is never filled in with something plausible: a person follows
// what a contraindication field says.
type ExerciseSafety struct {
	CommonErrors      []string `json:"commonErrors,omitempty"`
	StopSigns         []string `json:"stopSigns,omitempty"`
	Contraindications []string `json:"contraindications,omitempty"`
	Regressions       []string `json:"regressions,omitempty"`
	Progressions      []string `json:"progressions,omitempty"`
	InjuryNotes       string   `json:"injuryNotes,omitempty"`
}

// Empty reports whether the block carries nothing at all.
func (s ExerciseSafety) Empty() bool {
	return len(s.CommonErrors) == 0 && len(s.StopSigns) == 0 &&
		len(s.Contraindications) == 0 && len(s.Regressions) == 0 &&
		len(s.Progressions) == 0 && s.InjuryNotes == ""
}

// ExerciseMedia is block F. MediaStatus is deliberately *not* here: the three
// statuses are structural and live in columns of their own.
type ExerciseMedia struct {
	MainAssetID        string   `json:"mainAssetId,omitempty"`
	PhaseAssetIDs      []string `json:"phaseAssetIds,omitempty"`
	MuscleLayerAssetID string   `json:"muscleLayerAssetId,omitempty"`
	AnimationAssetID   string   `json:"animationAssetId,omitempty"`
	VideoURL           string   `json:"videoUrl,omitempty"`
	CameraView         string   `json:"cameraView,omitempty"`
	CropFocalPoint     string   `json:"cropFocalPoint,omitempty"`
	AltText            string   `json:"altText,omitempty"`
	MediaRights        string   `json:"mediaRights,omitempty"`
	TechniqueVersion   string   `json:"techniqueVersion,omitempty"`
}

// ExerciseQA is block G: where the record came from and who stands behind it.
type ExerciseQA struct {
	Sources         []string   `json:"sources,omitempty"`
	Reviewers       []string   `json:"reviewers,omitempty"`
	AuthorID        string     `json:"authorId,omitempty"`
	EditorID        string     `json:"editorId,omitempty"`
	ReviewedAt      *time.Time `json:"reviewedAt,omitempty"`
	ReviewNotes     string     `json:"reviewNotes,omitempty"`
	RejectionReason string     `json:"rejectionReason,omitempty"`
}

// Exercise is one catalogue record: blocks A and B as fields the catalogue
// filters and sorts on, C–G as documents it only ever hands over whole.
type Exercise struct {
	// A. Identification. ID is the string the phone already sends inside
	// clientMutationId, and it never changes.
	ID             string
	Slug           string
	LegacyNumber   *int
	SchemaVersion  int
	ContentVersion int
	ContentLocale  string
	// ContentHash fingerprints the record as the import file stated it, which
	// is what makes a repeated import a no-op instead of a rewrite.
	ContentHash string
	// Revision counts accepted changes; it moves only when ContentHash moves.
	Revision int

	NameRu  string
	NameEn  string
	Aliases []string
	// VariantOf links one of the source's 28 repeated names to the record it
	// varies, per the master template's variant rule. Nothing is ever deleted.
	VariantOf *string

	// B. Classification — machine codes, every one of them in a dictionary.
	Sport            string
	Section          string
	Category         *string
	MovementPattern  *string
	Difficulty       *string
	Laterality       *string
	Equipment        []string
	PrimaryMuscles   []string
	SecondaryMuscles []string
	Joints           []string
	GoalTags         []string

	// C–G.
	Technique   ExerciseTechnique
	Programming ExerciseProgramming
	Safety      ExerciseSafety
	Media       ExerciseMedia
	QA          ExerciseQA

	// The three independent statuses. Published is the computed conjunction and
	// is what "an ordinary user can see this" means.
	PublicationStatus string
	ReviewStatus      string
	MediaStatus       string
	Published         bool

	// SortKey is the catalogue's ordering key, computed in Go so PostgreSQL and
	// the in-memory store order identically. SearchText is what `q` matches.
	SortKey    string
	SearchText string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// PublicationAllowed reports whether the master template's rule lets this
// record be published: ready + approved + approved. It is the precondition the
// importer checks before flipping publication_status, and the CHECK constraint
// exercise_publication_requires_approvals is what enforces the invariant once
// the flip has happened.
func PublicationAllowed(publication, review, media string) bool {
	return (publication == PublicationReady || publication == PublicationPublished) &&
		review == ReviewApproved && media == MediaApproved
}

// SortKeySeparator joins the folded name and the identifier inside a sort key.
//
// It is ASCII SOH, not NUL: PostgreSQL's text type cannot hold a NUL byte at
// all (SQLSTATE 22021), and it sorts below every character that can appear in
// an exercise name, so `name + SEP + id` orders exactly as `(name, id)` does.
const SortKeySeparator = "\x01"

// ExerciseCursor is the keyset position of the catalogue list. The order is
// (sort_key, id) ascending, which is total, so no row can be skipped or
// repeated while content is being re-imported underneath a paging client.
type ExerciseCursor struct {
	SortKey string
	ID      string
}

// After reports whether e sits strictly after the cursor in catalogue order.
func (c ExerciseCursor) After(e Exercise) bool {
	if e.SortKey != c.SortKey {
		return e.SortKey > c.SortKey
	}
	return e.ID > c.ID
}

// ExerciseFilter narrows the catalogue. Every field but Search and the paging
// controls is a set of machine codes; a value that is not a code simply matches
// nothing, and there is no free-text filter anywhere.
type ExerciseFilter struct {
	Sports       []string
	Sections     []string
	Equipment    []string
	Muscles      []string
	Difficulties []string
	// Search is already normalized (lowercased, trimmed) by the caller and is
	// matched as a literal substring of SearchText — never as LIKE and never as
	// a regular expression, so '%', '_' and '\' carry no meaning.
	Search string
	// IncludeUnpublished is false for every request an ordinary user can make.
	// Nothing on the HTTP surface sets it; it exists for the importer and for
	// tests that must observe a draft.
	IncludeUnpublished bool

	Limit  int
	Cursor *ExerciseCursor
}

// MatchesExercise reports whether e satisfies the filter. It is the single
// definition of the filter semantics, shared by the in-memory store and by the
// tests that pin the SQL adapter against it.
func (f ExerciseFilter) MatchesExercise(e Exercise) bool {
	if !f.IncludeUnpublished && !e.Published {
		return false
	}
	if len(f.Sports) > 0 && !containsAny([]string{e.Sport}, f.Sports) {
		return false
	}
	if len(f.Sections) > 0 && !containsAny([]string{e.Section}, f.Sections) {
		return false
	}
	if len(f.Equipment) > 0 && !containsAny(e.Equipment, f.Equipment) {
		return false
	}
	if len(f.Muscles) > 0 && !containsAny(append(append([]string{}, e.PrimaryMuscles...), e.SecondaryMuscles...), f.Muscles) {
		return false
	}
	if len(f.Difficulties) > 0 {
		if e.Difficulty == nil || !containsAny([]string{*e.Difficulty}, f.Difficulties) {
			return false
		}
	}
	if f.Search != "" && !strings.Contains(e.SearchText, f.Search) {
		return false
	}
	if f.Cursor != nil && !f.Cursor.After(e) {
		return false
	}
	return true
}

func containsAny(have, want []string) bool {
	for _, h := range have {
		for _, w := range want {
			if h == w {
				return true
			}
		}
	}
	return false
}

// ExerciseSeed is one parsed import file: the dictionaries it defines and the
// records that use them. It is applied atomically — either the whole file lands
// or none of it does.
type ExerciseSeed struct {
	Source         string
	FileSHA256     string
	SchemaVersion  int
	ContentVersion int
	ContentLocale  string
	Codes          []ExerciseCode
	Exercises      []Exercise
}

// ExerciseSeedReport says what an import did. `Absent` counts records already in
// the database that the file did not mention: they are left alone, never
// deleted, because a stored set may still name one.
type ExerciseSeedReport struct {
	ImportID     string
	Added        int
	Updated      int
	Skipped      int
	Absent       int
	CodesWritten int
}

// ExerciseIdentity is the immutable part of a catalogue record: the identifier
// the phone sends, and the two secondary keys that must keep pointing at it.
type ExerciseIdentity struct {
	ID           string
	Slug         string
	LegacyNumber *int
}

// RenameConflict names a refusal: the file gives Key (a slug or a legacy
// number) to WantID, while the database has already given it to HaveID.
type RenameConflict struct {
	Kind   string // "slug" or "legacyNumber"
	Key    string
	HaveID string
	WantID string
}

func (c RenameConflict) String() string {
	return fmt.Sprintf("%s %q already identifies exercise %q, the file gives it to %q",
		c.Kind, c.Key, c.HaveID, c.WantID)
}

// RenameError is the typed refusal returned by an import that would move an
// identifier. It wraps ErrExerciseRenamed so callers can match on the sentinel.
type RenameError struct {
	Conflicts []RenameConflict
}

func (e *RenameError) Error() string {
	parts := make([]string, 0, len(e.Conflicts))
	for _, c := range e.Conflicts {
		parts = append(parts, c.String())
	}
	return ErrExerciseRenamed.Error() + ": " + strings.Join(parts, "; ")
}

func (e *RenameError) Unwrap() error { return ErrExerciseRenamed }

// DetectRenames compares the identities already stored with the ones a file
// carries and reports every identifier the file would move.
//
// This is the guarantee the whole import exists to protect. `exerciseId` left
// the phone inside `clientMutationId` and is stored in workout_sets; a
// catalogue that renamed one would silently detach recorded history from the
// exercise it was performed with. It is written once, here, and both store
// implementations call it inside the critical section that writes, so the check
// and the write cannot be separated by a concurrent import.
func DetectRenames(existing, incoming []ExerciseIdentity) []RenameConflict {
	bySlug := map[string]string{}
	byLegacy := map[int]string{}
	for _, e := range existing {
		if e.Slug != "" {
			bySlug[e.Slug] = e.ID
		}
		if e.LegacyNumber != nil {
			byLegacy[*e.LegacyNumber] = e.ID
		}
	}

	var conflicts []RenameConflict
	for _, in := range incoming {
		if in.Slug != "" {
			if have, ok := bySlug[in.Slug]; ok && have != in.ID {
				conflicts = append(conflicts, RenameConflict{
					Kind: "slug", Key: in.Slug, HaveID: have, WantID: in.ID,
				})
			}
		}
		if in.LegacyNumber != nil {
			if have, ok := byLegacy[*in.LegacyNumber]; ok && have != in.ID {
				conflicts = append(conflicts, RenameConflict{
					Kind: "legacyNumber", Key: fmt.Sprintf("%d", *in.LegacyNumber), HaveID: have, WantID: in.ID,
				})
			}
		}
	}
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Kind != conflicts[j].Kind {
			return conflicts[i].Kind < conflicts[j].Kind
		}
		return conflicts[i].Key < conflicts[j].Key
	})
	return conflicts
}

// CodeKey identifies one dictionary entry.
type CodeKey struct {
	Kind string
	Code string
}

// ExerciseCodeUses lists every dictionary entry an exercise refers to, so one
// function decides what "the codes in this record" means for both stores.
func ExerciseCodeUses(e Exercise) []CodeKey {
	uses := []CodeKey{{CodeSport, e.Sport}, {CodeSection, e.Section}}
	for kind, value := range map[string]*string{
		CodeCategory:        e.Category,
		CodeMovementPattern: e.MovementPattern,
		CodeDifficulty:      e.Difficulty,
		CodeLaterality:      e.Laterality,
	} {
		if value != nil && *value != "" {
			uses = append(uses, CodeKey{kind, *value})
		}
	}
	for _, group := range []struct {
		kind  string
		codes []string
	}{
		{CodeEquipment, e.Equipment},
		{CodeMuscle, e.PrimaryMuscles},
		{CodeMuscle, e.SecondaryMuscles},
		{CodeJoint, e.Joints},
		{CodeGoalTag, e.GoalTags},
	} {
		for _, code := range group.codes {
			uses = append(uses, CodeKey{group.kind, code})
		}
	}
	sort.Slice(uses, func(i, j int) bool {
		if uses[i].Kind != uses[j].Kind {
			return uses[i].Kind < uses[j].Kind
		}
		return uses[i].Code < uses[j].Code
	})
	return uses
}

// UnknownCodeError is the refusal of an import that codes a record with
// something no dictionary defines.
type UnknownCodeError struct {
	Missing []CodeKey
}

func (e *UnknownCodeError) Error() string {
	parts := make([]string, 0, len(e.Missing))
	for _, m := range e.Missing {
		parts = append(parts, m.Kind+"="+m.Code)
	}
	return ErrUnknownExerciseCode.Error() + ": " + strings.Join(parts, ", ")
}

func (e *UnknownCodeError) Unwrap() error { return ErrUnknownExerciseCode }

// MissingCodes returns, sorted and deduplicated, every dictionary entry the
// exercises refer to that `known` does not contain. `known` must already be the
// union of the dictionaries in the import file and the ones already stored.
func MissingCodes(known map[CodeKey]bool, exercises []Exercise) []CodeKey {
	seen := map[CodeKey]bool{}
	var missing []CodeKey
	for _, e := range exercises {
		for _, use := range ExerciseCodeUses(e) {
			if known[use] || seen[use] {
				continue
			}
			seen[use] = true
			missing = append(missing, use)
		}
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Kind != missing[j].Kind {
			return missing[i].Kind < missing[j].Kind
		}
		return missing[i].Code < missing[j].Code
	})
	return missing
}

// SortExercises puts records into catalogue order: (sort_key, id) ascending,
// compared byte-wise. PostgreSQL sorts the same two columns under the C
// collation, so the two implementations cannot disagree about where a page ends
// — which is what stops a keyset cursor from skipping or repeating a row.
func SortExercises(rows []Exercise) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].SortKey != rows[j].SortKey {
			return rows[i].SortKey < rows[j].SortKey
		}
		return rows[i].ID < rows[j].ID
	})
}

// SortExerciseCodes puts dictionary entries into the order the endpoint returns
// them: kind, then the curator's sort order, then the code itself.
func SortExerciseCodes(codes []ExerciseCode) {
	sort.Slice(codes, func(i, j int) bool {
		if codes[i].Kind != codes[j].Kind {
			return codes[i].Kind < codes[j].Kind
		}
		if codes[i].SortOrder != codes[j].SortOrder {
			return codes[i].SortOrder < codes[j].SortOrder
		}
		return codes[i].Code < codes[j].Code
	})
}
