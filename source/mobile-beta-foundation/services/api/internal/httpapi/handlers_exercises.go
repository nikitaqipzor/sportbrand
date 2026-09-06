package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"athletica.ai/api/internal/exercises"
	"athletica.ai/api/internal/store"
)

// exerciseSummary is one row of the catalogue (prototype screen 13).
//
// It carries identity, the localized names and every machine code — enough to
// render a list, group it and filter it client-side — and none of the narrative
// blocks, which only the card needs.
type exerciseSummary struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	LegacyNumber *int     `json:"legacyNumber"`
	NameRu       string   `json:"nameRu"`
	NameEn       string   `json:"nameEn"`
	Aliases      []string `json:"aliases"`
	VariantOf    *string  `json:"variantOf"`

	Sport            string   `json:"sport"`
	Section          string   `json:"section"`
	Category         *string  `json:"category"`
	MovementPattern  *string  `json:"movementPattern"`
	Difficulty       *string  `json:"difficulty"`
	Laterality       *string  `json:"laterality"`
	Equipment        []string `json:"equipment"`
	PrimaryMuscles   []string `json:"primaryMuscles"`
	SecondaryMuscles []string `json:"secondaryMuscles"`
	Joints           []string `json:"joints"`
	GoalTags         []string `json:"goalTags"`

	ContentVersion int    `json:"contentVersion"`
	Revision       int    `json:"revision"`
	UpdatedAt      string `json:"updatedAt"`

	// hasTechnique and hasSafety let the catalogue mark a record whose card is
	// still empty *without* the client having to guess from an empty array.
	// They are false for every record until the encyclopedia is imported, and
	// saying so is the point: a screen must not imply guidance it does not have.
	HasTechnique bool `json:"hasTechnique"`
	HasSafety    bool `json:"hasSafety"`
}

// exerciseCard is one exercise in full (prototype screens 14 and 15).
type exerciseCard struct {
	exerciseSummary

	Technique   exerciseTechniqueBody   `json:"technique"`
	Programming exerciseProgrammingBody `json:"programming"`
	Safety      exerciseSafetyBody      `json:"safety"`
	Media       exerciseMediaBody       `json:"media"`

	// The three independent statuses of the master template are reported, not
	// hidden: a client that shows an empty technique block should be able to
	// say *why* it is empty.
	PublicationStatus string `json:"publicationStatus"`
	ReviewStatus      string `json:"reviewStatus"`
	MediaStatus       string `json:"mediaStatus"`

	ContentLocale string `json:"contentLocale"`
	SchemaVersion int    `json:"schemaVersion"`
}

type exerciseTechniqueBody struct {
	Setup          string   `json:"setup"`
	StartPosition  string   `json:"startPosition"`
	ExecutionSteps []string `json:"executionSteps"`
	KeyCues        []string `json:"keyCues"`
	Breathing      string   `json:"breathing"`
	Tempo          string   `json:"tempo"`
	RangeOfMotion  string   `json:"rangeOfMotion"`
	FinishReturn   string   `json:"finishReturn"`
}

type exerciseProgrammingBody struct {
	VolumeType         string   `json:"volumeType"`
	SetsMin            *int     `json:"setsMin"`
	SetsMax            *int     `json:"setsMax"`
	RepsMin            *int     `json:"repsMin"`
	RepsMax            *int     `json:"repsMax"`
	DurationMinSeconds *int     `json:"durationMinSeconds"`
	DurationMaxSeconds *int     `json:"durationMaxSeconds"`
	DistanceMinMeters  *float64 `json:"distanceMinMeters"`
	DistanceMaxMeters  *float64 `json:"distanceMaxMeters"`
	CyclesMin          *int     `json:"cyclesMin"`
	CyclesMax          *int     `json:"cyclesMax"`
	RestSeconds        *int     `json:"restSeconds"`
	IntensityType      string   `json:"intensityType"`
	IntensityMin       *float64 `json:"intensityMin"`
	IntensityMax       *float64 `json:"intensityMax"`
	StopCondition      string   `json:"stopCondition"`
}

type exerciseSafetyBody struct {
	CommonErrors      []string `json:"commonErrors"`
	StopSigns         []string `json:"stopSigns"`
	Contraindications []string `json:"contraindications"`
	Regressions       []string `json:"regressions"`
	Progressions      []string `json:"progressions"`
	InjuryNotes       string   `json:"injuryNotes"`
}

type exerciseMediaBody struct {
	MainAssetID        string   `json:"mainAssetId"`
	PhaseAssetIDs      []string `json:"phaseAssetIds"`
	MuscleLayerAssetID string   `json:"muscleLayerAssetId"`
	AnimationAssetID   string   `json:"animationAssetId"`
	VideoURL           string   `json:"videoUrl"`
	CameraView         string   `json:"cameraView"`
	AltText            string   `json:"altText"`
	MediaRights        string   `json:"mediaRights"`
	TechniqueVersion   string   `json:"techniqueVersion"`
}

type exerciseListResponse struct {
	Items []exerciseSummary `json:"items"`
	// NextCursor is null on the last page. It is opaque: echo it back untouched.
	NextCursor *string `json:"nextCursor"`
}

type exerciseCodeBody struct {
	Code      string `json:"code"`
	NameRu    string `json:"nameRu"`
	NameEn    string `json:"nameEn"`
	SortOrder int    `json:"sortOrder"`
}

type exerciseDictionaryBody struct {
	Kind  string             `json:"kind"`
	Items []exerciseCodeBody `json:"items"`
}

type exerciseDictionariesResponse struct {
	Dictionaries []exerciseDictionaryBody `json:"dictionaries"`
}

func toExerciseSummary(e store.Exercise) exerciseSummary {
	return exerciseSummary{
		ID:               e.ID,
		Slug:             e.Slug,
		LegacyNumber:     e.LegacyNumber,
		NameRu:           e.NameRu,
		NameEn:           e.NameEn,
		Aliases:          orEmpty(e.Aliases),
		VariantOf:        e.VariantOf,
		Sport:            e.Sport,
		Section:          e.Section,
		Category:         e.Category,
		MovementPattern:  e.MovementPattern,
		Difficulty:       e.Difficulty,
		Laterality:       e.Laterality,
		Equipment:        orEmpty(e.Equipment),
		PrimaryMuscles:   orEmpty(e.PrimaryMuscles),
		SecondaryMuscles: orEmpty(e.SecondaryMuscles),
		Joints:           orEmpty(e.Joints),
		GoalTags:         orEmpty(e.GoalTags),
		ContentVersion:   e.ContentVersion,
		Revision:         e.Revision,
		UpdatedAt:        e.UpdatedAt.UTC().Format(time.RFC3339),
		HasTechnique:     !e.Technique.Empty(),
		HasSafety:        !e.Safety.Empty(),
	}
}

func toExerciseCard(e store.Exercise) exerciseCard {
	return exerciseCard{
		exerciseSummary: toExerciseSummary(e),
		Technique: exerciseTechniqueBody{
			Setup:          e.Technique.Setup,
			StartPosition:  e.Technique.StartPosition,
			ExecutionSteps: orEmpty(e.Technique.ExecutionSteps),
			KeyCues:        orEmpty(e.Technique.KeyCues),
			Breathing:      e.Technique.Breathing,
			Tempo:          e.Technique.Tempo,
			RangeOfMotion:  e.Technique.RangeOfMotion,
			FinishReturn:   e.Technique.FinishReturn,
		},
		Programming: exerciseProgrammingBody{
			VolumeType:         e.Programming.VolumeType,
			SetsMin:            e.Programming.SetsMin,
			SetsMax:            e.Programming.SetsMax,
			RepsMin:            e.Programming.RepsMin,
			RepsMax:            e.Programming.RepsMax,
			DurationMinSeconds: e.Programming.DurationMinSeconds,
			DurationMaxSeconds: e.Programming.DurationMaxSeconds,
			DistanceMinMeters:  e.Programming.DistanceMinMeters,
			DistanceMaxMeters:  e.Programming.DistanceMaxMeters,
			CyclesMin:          e.Programming.CyclesMin,
			CyclesMax:          e.Programming.CyclesMax,
			RestSeconds:        e.Programming.RestSeconds,
			IntensityType:      e.Programming.IntensityType,
			IntensityMin:       e.Programming.IntensityMin,
			IntensityMax:       e.Programming.IntensityMax,
			StopCondition:      e.Programming.StopCondition,
		},
		Safety: exerciseSafetyBody{
			CommonErrors:      orEmpty(e.Safety.CommonErrors),
			StopSigns:         orEmpty(e.Safety.StopSigns),
			Contraindications: orEmpty(e.Safety.Contraindications),
			Regressions:       orEmpty(e.Safety.Regressions),
			Progressions:      orEmpty(e.Safety.Progressions),
			InjuryNotes:       e.Safety.InjuryNotes,
		},
		Media: exerciseMediaBody{
			MainAssetID:        e.Media.MainAssetID,
			PhaseAssetIDs:      orEmpty(e.Media.PhaseAssetIDs),
			MuscleLayerAssetID: e.Media.MuscleLayerAssetID,
			AnimationAssetID:   e.Media.AnimationAssetID,
			VideoURL:           e.Media.VideoURL,
			CameraView:         e.Media.CameraView,
			AltText:            e.Media.AltText,
			MediaRights:        e.Media.MediaRights,
			TechniqueVersion:   e.Media.TechniqueVersion,
		},
		PublicationStatus: e.PublicationStatus,
		ReviewStatus:      e.ReviewStatus,
		MediaStatus:       e.MediaStatus,
		ContentLocale:     e.ContentLocale,
		SchemaVersion:     e.SchemaVersion,
	}
}

// orEmpty renders an absent list as `[]` rather than `null`, so a client never
// has to handle two shapes of "nothing".
func orEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// handleListExercises pages the catalogue behind prototype screen 13.
//
// Filters — `sport`, `section`, `equipment`, `muscle`, `difficulty` — are
// machine codes, repeatable or comma separated. `q` searches the names. The
// catalogue is shared content, so unlike GET /workouts there is nothing here to
// scope by user; the endpoint is authenticated all the same.
func (s *Server) handleListExercises(w http.ResponseWriter, r *http.Request, _ store.User) {
	query := r.URL.Query()

	limit, err := optionalInt(query.Get("limit"), 0)
	if err != nil || limit < 0 || limit > exercises.MaxPageSize {
		writeError(w, s.log, http.StatusBadRequest, codeInvalidRequest,
			"limit must be an integer between 1 and "+strconv.Itoa(exercises.MaxPageSize))
		return
	}

	listQuery := exercises.ListQuery{
		Sports:       exercises.NormalizeCodes(query["sport"]),
		Sections:     exercises.NormalizeCodes(query["section"]),
		Equipment:    exercises.NormalizeCodes(query["equipment"]),
		Muscles:      exercises.NormalizeCodes(query["muscle"]),
		Difficulties: exercises.NormalizeCodes(query["difficulty"]),
		// An empty or whitespace-only `q` is not a filter and not an error: it
		// is what the search box sends while a person deletes what they typed.
		Search: exercises.NormalizeSearch(query.Get("q")),
		Limit:  limit,
	}
	if raw := strings.TrimSpace(query.Get("cursor")); raw != "" {
		cursor, err := exercises.DecodeCursor(raw)
		if err != nil {
			writeError(w, s.log, http.StatusBadRequest, codeInvalidCursor,
				"cursor is not one this API issued; start the list again without it")
			return
		}
		listQuery.Cursor = &cursor
	}

	page, err := s.exercises.List(r.Context(), listQuery)
	if err != nil {
		s.internal(w, r, "list exercises failed", err)
		return
	}

	body := exerciseListResponse{Items: make([]exerciseSummary, 0, len(page.Items))}
	for _, exercise := range page.Items {
		body.Items = append(body.Items, toExerciseSummary(exercise))
	}
	if page.NextCursor != "" {
		next := page.NextCursor
		body.NextCursor = &next
	}
	writeJSON(w, s.log, http.StatusOK, body)
}

// handleGetExercise returns one card. A record that is not published answers
// exactly like an identifier the catalogue has never held.
func (s *Server) handleGetExercise(w http.ResponseWriter, r *http.Request, _ store.User) {
	exercise, err := s.exercises.Exercise(r.Context(), r.PathValue("exerciseId"))
	switch {
	case err == nil:
		writeJSON(w, s.log, http.StatusOK, toExerciseCard(exercise))
	case errors.Is(err, store.ErrNotFound):
		writeError(w, s.log, http.StatusNotFound, codeNotFound, "exercise not found")
	default:
		s.internal(w, r, "load exercise failed", err)
	}
}

// handleExerciseDictionaries returns every machine code with its localized
// names. It exists so the client builds its filter controls from the server's
// vocabulary instead of a hard-coded list that drifts.
func (s *Server) handleExerciseDictionaries(w http.ResponseWriter, r *http.Request, _ store.User) {
	dictionaries, err := s.exercises.Dictionaries(r.Context())
	if err != nil {
		s.internal(w, r, "list exercise dictionaries failed", err)
		return
	}
	body := exerciseDictionariesResponse{Dictionaries: make([]exerciseDictionaryBody, 0, len(dictionaries))}
	for _, dictionary := range dictionaries {
		out := exerciseDictionaryBody{Kind: dictionary.Kind, Items: make([]exerciseCodeBody, 0, len(dictionary.Items))}
		for _, code := range dictionary.Items {
			out.Items = append(out.Items, exerciseCodeBody{
				Code: code.Code, NameRu: code.NameRu, NameEn: code.NameEn, SortOrder: code.SortOrder,
			})
		}
		body.Dictionaries = append(body.Dictionaries, out)
	}
	writeJSON(w, s.log, http.StatusOK, body)
}
