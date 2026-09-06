package memory

import (
	"context"

	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

// ListExercises returns one page of the catalogue in (sort_key, id) order.
//
// The whole filter lives in store.ExerciseFilter.MatchesExercise, which the SQL
// adapter mirrors clause for clause; keeping the definition in one place is
// what stops the two implementations from disagreeing about which rows a page
// contains.
func (s *Store) ListExercises(_ context.Context, filter store.ExerciseFilter) ([]store.Exercise, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := []store.Exercise{}
	for _, id := range s.exerciseOrder {
		exercise := s.exercises[id]
		if filter.MatchesExercise(exercise) {
			out = append(out, cloneExercise(exercise))
		}
	}
	store.SortExercises(out)
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

// ExerciseByID returns one record; an unpublished one is ErrNotFound, exactly
// like an identifier that was never imported.
func (s *Store) ExerciseByID(_ context.Context, id string, includeUnpublished bool) (store.Exercise, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exercise, ok := s.exercises[id]
	if !ok || (!includeUnpublished && !exercise.Published) {
		return store.Exercise{}, store.ErrNotFound
	}
	return cloneExercise(exercise), nil
}

// ExerciseCodes returns every dictionary entry in endpoint order.
func (s *Store) ExerciseCodes(_ context.Context) ([]store.ExerciseCode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]store.ExerciseCode, 0, len(s.exerciseCodes))
	for _, code := range s.exerciseCodes {
		out = append(out, code)
	}
	store.SortExerciseCodes(out)
	return out, nil
}

// SeedExercises applies one import file under the store's write lock, which is
// this implementation's equivalent of the SQL adapter's single transaction: the
// rename check, the dictionary check and the writes cannot be interleaved with
// another import.
func (s *Store) SeedExercises(_ context.Context, seed store.ExerciseSeed) (store.ExerciseSeedReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1. Refuse a file that would move an identifier. This happens before any
	// write, and nothing is written when it fires.
	existing := make([]store.ExerciseIdentity, 0, len(s.exercises))
	for _, e := range s.exercises {
		existing = append(existing, store.ExerciseIdentity{ID: e.ID, Slug: e.Slug, LegacyNumber: e.LegacyNumber})
	}
	incoming := make([]store.ExerciseIdentity, 0, len(seed.Exercises))
	for _, e := range seed.Exercises {
		incoming = append(incoming, store.ExerciseIdentity{ID: e.ID, Slug: e.Slug, LegacyNumber: e.LegacyNumber})
	}
	if conflicts := store.DetectRenames(existing, incoming); len(conflicts) > 0 {
		return store.ExerciseSeedReport{}, &store.RenameError{Conflicts: conflicts}
	}

	// 2. Refuse a record coded with something no dictionary defines — the file's
	// own dictionaries plus the ones already stored.
	known := map[store.CodeKey]bool{}
	for key := range s.exerciseCodes {
		known[key] = true
	}
	for _, code := range seed.Codes {
		known[store.CodeKey{Kind: code.Kind, Code: code.Code}] = true
	}
	if missing := store.MissingCodes(known, seed.Exercises); len(missing) > 0 {
		return store.ExerciseSeedReport{}, &store.UnknownCodeError{Missing: missing}
	}

	now := s.now().UTC()
	report := store.ExerciseSeedReport{ImportID: ids.NewUUID()}

	for _, code := range seed.Codes {
		key := store.CodeKey{Kind: code.Kind, Code: code.Code}
		if stored, ok := s.exerciseCodes[key]; !ok || stored != code {
			s.exerciseCodes[key] = code
			report.CodesWritten++
		}
	}

	for _, incomingExercise := range seed.Exercises {
		stored, exists := s.exercises[incomingExercise.ID]
		switch {
		case !exists:
			row := cloneExercise(incomingExercise)
			row.Published = publishedNow(row)
			row.Revision = 1
			row.CreatedAt = now
			row.UpdatedAt = now
			s.exercises[row.ID] = row
			s.exerciseOrder = append(s.exerciseOrder, row.ID)
			report.Added++
		case stored.ContentHash == incomingExercise.ContentHash:
			// Byte-identical to what is stored: touch nothing, not even
			// updated_at. This is what makes a repeated import a no-op.
			report.Skipped++
		default:
			row := cloneExercise(incomingExercise)
			row.Published = publishedNow(row)
			row.Revision = stored.Revision + 1
			row.CreatedAt = stored.CreatedAt
			row.UpdatedAt = now
			s.exercises[row.ID] = row
			report.Updated++
		}
	}

	// A record the file does not mention is left exactly as it is. Deleting it
	// would strand every recorded set that names it.
	mentioned := map[string]bool{}
	for _, e := range seed.Exercises {
		mentioned[e.ID] = true
	}
	for id := range s.exercises {
		if !mentioned[id] {
			report.Absent++
		}
	}

	s.exerciseImports = append(s.exerciseImports, report)
	return report, nil
}

// publishedNow is the Go twin of the generated is_published column: all three
// statuses must agree before an ordinary user may see the record.
func publishedNow(e store.Exercise) bool {
	return e.PublicationStatus == store.PublicationPublished &&
		e.ReviewStatus == store.ReviewApproved &&
		e.MediaStatus == store.MediaApproved
}

// cloneExercise copies the slices so a caller cannot mutate stored state
// through the value it was handed — the SQL adapter hands out fresh rows and
// this implementation must not be more permissive.
func cloneExercise(e store.Exercise) store.Exercise {
	out := e
	out.Aliases = cloneStrings(e.Aliases)
	out.Equipment = cloneStrings(e.Equipment)
	out.PrimaryMuscles = cloneStrings(e.PrimaryMuscles)
	out.SecondaryMuscles = cloneStrings(e.SecondaryMuscles)
	out.Joints = cloneStrings(e.Joints)
	out.GoalTags = cloneStrings(e.GoalTags)
	out.Technique.ExecutionSteps = cloneStrings(e.Technique.ExecutionSteps)
	out.Technique.KeyCues = cloneStrings(e.Technique.KeyCues)
	out.Safety.CommonErrors = cloneStrings(e.Safety.CommonErrors)
	out.Safety.StopSigns = cloneStrings(e.Safety.StopSigns)
	out.Safety.Contraindications = cloneStrings(e.Safety.Contraindications)
	out.Safety.Regressions = cloneStrings(e.Safety.Regressions)
	out.Safety.Progressions = cloneStrings(e.Safety.Progressions)
	out.Media.PhaseAssetIDs = cloneStrings(e.Media.PhaseAssetIDs)
	out.QA.Sources = cloneStrings(e.QA.Sources)
	out.QA.Reviewers = cloneStrings(e.QA.Reviewers)
	if e.VariantOf != nil {
		variant := *e.VariantOf
		out.VariantOf = &variant
	}
	if e.LegacyNumber != nil {
		legacy := *e.LegacyNumber
		out.LegacyNumber = &legacy
	}
	for _, field := range []struct{ src, dst **string }{
		{&e.Category, &out.Category},
		{&e.MovementPattern, &out.MovementPattern},
		{&e.Difficulty, &out.Difficulty},
		{&e.Laterality, &out.Laterality},
	} {
		if *field.src != nil {
			value := **field.src
			*field.dst = &value
		}
	}
	if e.QA.ReviewedAt != nil {
		at := *e.QA.ReviewedAt
		out.QA.ReviewedAt = &at
	}
	return out
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
