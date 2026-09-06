package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"athletica.ai/api/internal/ids"
	"athletica.ai/api/internal/store"
)

// exerciseColumns is the projection every catalogue query shares. The five
// jsonb blocks come back as raw bytes and are decoded in Go, so the shape of
// blocks C–G is defined in exactly one place: the structs in internal/store.
const exerciseColumns = `id, slug, legacy_number, schema_version, content_version, content_locale,
	content_hash, revision, name_ru, name_en, aliases, variant_of,
	sport, section, category, movement_pattern, difficulty, laterality,
	sort_key, search_text, technique, programming, safety, media, qa,
	publication_status, review_status, media_status, is_published, created_at, updated_at`

func scanExercise(row scanner) (store.Exercise, error) {
	var (
		e                                         store.Exercise
		technique, programming, safety, media, qa []byte
	)
	err := row.Scan(&e.ID, &e.Slug, &e.LegacyNumber, &e.SchemaVersion, &e.ContentVersion, &e.ContentLocale,
		&e.ContentHash, &e.Revision, &e.NameRu, &e.NameEn, &e.Aliases, &e.VariantOf,
		&e.Sport, &e.Section, &e.Category, &e.MovementPattern, &e.Difficulty, &e.Laterality,
		&e.SortKey, &e.SearchText, &technique, &programming, &safety, &media, &qa,
		&e.PublicationStatus, &e.ReviewStatus, &e.MediaStatus, &e.Published, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return store.Exercise{}, err
	}
	for _, block := range []struct {
		raw []byte
		dst any
	}{
		{technique, &e.Technique}, {programming, &e.Programming},
		{safety, &e.Safety}, {media, &e.Media}, {qa, &e.QA},
	} {
		if len(block.raw) == 0 {
			continue
		}
		if err := json.Unmarshal(block.raw, block.dst); err != nil {
			return store.Exercise{}, fmt.Errorf("postgres: decode exercise block: %w", err)
		}
	}
	return e, nil
}

// ListExercises pages the catalogue with a keyset cursor.
//
// Every clause mirrors store.ExerciseFilter.MatchesExercise. Two details are
// load-bearing:
//
//   - the ordering columns are declared COLLATE "C", so PostgreSQL compares the
//     same bytes Go compares and a page boundary lands in the same place in both
//     implementations;
//   - the search is `position($n IN search_text) > 0`, a literal substring
//     match. It is not LIKE and not a regular expression, so '%', '_', '\' and
//     every other special character are ordinary letters and no input can
//     change the shape of the query.
func (s *Store) ListExercises(ctx context.Context, filter store.ExerciseFilter) ([]store.Exercise, error) {
	var (
		args  []any
		where []string
	)
	next := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	if !filter.IncludeUnpublished {
		where = append(where, "is_published")
	}
	if len(filter.Sports) > 0 {
		where = append(where, "sport = ANY("+next(filter.Sports)+")")
	}
	if len(filter.Sections) > 0 {
		where = append(where, "section = ANY("+next(filter.Sections)+")")
	}
	if len(filter.Difficulties) > 0 {
		where = append(where, "difficulty = ANY("+next(filter.Difficulties)+")")
	}
	if len(filter.Equipment) > 0 {
		where = append(where, `EXISTS (SELECT 1 FROM exercise_code_link l
		         WHERE l.exercise_id = exercise.id AND l.relation = 'equipment'
		           AND l.code = ANY(`+next(filter.Equipment)+`))`)
	}
	if len(filter.Muscles) > 0 {
		// A muscle matches whether it is loaded primarily or secondarily: the
		// athlete filtering for "spine erectors" wants both.
		where = append(where, `EXISTS (SELECT 1 FROM exercise_code_link l
		         WHERE l.exercise_id = exercise.id
		           AND l.relation IN ('primary_muscle', 'secondary_muscle')
		           AND l.code = ANY(`+next(filter.Muscles)+`))`)
	}
	if filter.Search != "" {
		where = append(where, "position("+next(filter.Search)+" IN search_text) > 0")
	}
	if filter.Cursor != nil {
		where = append(where, "(sort_key, id) > ("+next(filter.Cursor.SortKey)+", "+next(filter.Cursor.ID)+")")
	}
	if len(where) == 0 {
		where = append(where, "TRUE")
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT ` + exerciseColumns + ` FROM exercise WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY sort_key, id LIMIT ` + next(limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: list exercises: %w", err)
	}
	defer rows.Close()

	out := []store.Exercise{}
	for rows.Next() {
		exercise, err := scanExercise(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan exercise: %w", err)
		}
		out = append(out, exercise)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list exercises: %w", err)
	}
	return s.attachCodeLinks(ctx, out)
}

// ExerciseByID returns one record. An unpublished one answers ErrNotFound, so a
// draft is indistinguishable from an identifier that was never imported.
func (s *Store) ExerciseByID(ctx context.Context, id string, includeUnpublished bool) (store.Exercise, error) {
	q := `SELECT ` + exerciseColumns + ` FROM exercise WHERE id = $1`
	if !includeUnpublished {
		q += ` AND is_published`
	}
	exercise, err := scanExercise(s.pool.QueryRow(ctx, q, id))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return store.Exercise{}, store.ErrNotFound
	case err != nil:
		return store.Exercise{}, fmt.Errorf("postgres: load exercise: %w", err)
	}
	withLinks, err := s.attachCodeLinks(ctx, []store.Exercise{exercise})
	if err != nil {
		return store.Exercise{}, err
	}
	return withLinks[0], nil
}

// attachCodeLinks fills the multi-valued code fields in one extra query rather
// than one per row, keeping the page a two-statement read however long it is.
func (s *Store) attachCodeLinks(ctx context.Context, rows []store.Exercise) ([]store.Exercise, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	index := make(map[string]int, len(rows))
	idList := make([]string, 0, len(rows))
	for i, row := range rows {
		index[row.ID] = i
		idList = append(idList, row.ID)
	}

	const q = `SELECT exercise_id, relation, code FROM exercise_code_link
	           WHERE exercise_id = ANY($1) ORDER BY exercise_id, relation, position, code`
	linkRows, err := s.pool.Query(ctx, q, idList)
	if err != nil {
		return nil, fmt.Errorf("postgres: load exercise codes: %w", err)
	}
	defer linkRows.Close()

	for linkRows.Next() {
		var exerciseID, relation, code string
		if err := linkRows.Scan(&exerciseID, &relation, &code); err != nil {
			return nil, fmt.Errorf("postgres: scan exercise code: %w", err)
		}
		i, ok := index[exerciseID]
		if !ok {
			continue
		}
		switch relation {
		case store.RelationEquipment:
			rows[i].Equipment = append(rows[i].Equipment, code)
		case store.RelationPrimaryMuscle:
			rows[i].PrimaryMuscles = append(rows[i].PrimaryMuscles, code)
		case store.RelationSecondaryMuscle:
			rows[i].SecondaryMuscles = append(rows[i].SecondaryMuscles, code)
		case store.RelationJoint:
			rows[i].Joints = append(rows[i].Joints, code)
		case store.RelationGoalTag:
			rows[i].GoalTags = append(rows[i].GoalTags, code)
		}
	}
	if err := linkRows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: load exercise codes: %w", err)
	}
	return rows, nil
}

// ExerciseCodes returns every dictionary entry in endpoint order.
func (s *Store) ExerciseCodes(ctx context.Context) ([]store.ExerciseCode, error) {
	const q = `SELECT kind, code, name_ru, name_en, sort_order FROM exercise_code
	           ORDER BY kind, sort_order, code`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("postgres: list exercise codes: %w", err)
	}
	defer rows.Close()

	out := []store.ExerciseCode{}
	for rows.Next() {
		var code store.ExerciseCode
		if err := rows.Scan(&code.Kind, &code.Code, &code.NameRu, &code.NameEn, &code.SortOrder); err != nil {
			return nil, fmt.Errorf("postgres: scan exercise code: %w", err)
		}
		out = append(out, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list exercise codes: %w", err)
	}
	return out, nil
}

// SeedExercises applies one import file in a single transaction: either the
// whole file lands or none of it does.
//
// The rename check runs inside that transaction, against rows locked for the
// duration, so the check and the write cannot be separated by a concurrent
// import. Nothing is deleted — a record the file omits is counted and left
// alone, because a stored set may still name it.
func (s *Store) SeedExercises(ctx context.Context, seed store.ExerciseSeed) (store.ExerciseSeedReport, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.ExerciseSeedReport{}, fmt.Errorf("postgres: begin exercise import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// One writer at a time. The advisory lock is transaction-scoped, so it is
	// released by the commit or the rollback and cannot be leaked.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, exerciseImportLockID); err != nil {
		return store.ExerciseSeedReport{}, fmt.Errorf("postgres: lock exercise import: %w", err)
	}

	existing, err := loadExerciseIdentities(ctx, tx)
	if err != nil {
		return store.ExerciseSeedReport{}, err
	}
	incoming := make([]store.ExerciseIdentity, 0, len(seed.Exercises))
	for _, e := range seed.Exercises {
		incoming = append(incoming, store.ExerciseIdentity{ID: e.ID, Slug: e.Slug, LegacyNumber: e.LegacyNumber})
	}
	if conflicts := store.DetectRenames(existing, incoming); len(conflicts) > 0 {
		return store.ExerciseSeedReport{}, &store.RenameError{Conflicts: conflicts}
	}

	known, err := loadKnownCodes(ctx, tx)
	if err != nil {
		return store.ExerciseSeedReport{}, err
	}
	for _, code := range seed.Codes {
		known[store.CodeKey{Kind: code.Kind, Code: code.Code}] = true
	}
	if missing := store.MissingCodes(known, seed.Exercises); len(missing) > 0 {
		return store.ExerciseSeedReport{}, &store.UnknownCodeError{Missing: missing}
	}

	report := store.ExerciseSeedReport{ImportID: ids.NewUUID()}
	for _, code := range seed.Codes {
		const upsert = `INSERT INTO exercise_code (kind, code, name_ru, name_en, sort_order)
		                VALUES ($1, $2, $3, $4, $5)
		                ON CONFLICT (kind, code) DO UPDATE
		                   SET name_ru = EXCLUDED.name_ru,
		                       name_en = EXCLUDED.name_en,
		                       sort_order = EXCLUDED.sort_order
		                 WHERE (exercise_code.name_ru, exercise_code.name_en, exercise_code.sort_order)
		                    IS DISTINCT FROM (EXCLUDED.name_ru, EXCLUDED.name_en, EXCLUDED.sort_order)`
		tag, err := tx.Exec(ctx, upsert, code.Kind, code.Code, code.NameRu, code.NameEn, code.SortOrder)
		if err != nil {
			return store.ExerciseSeedReport{}, fmt.Errorf("postgres: write dictionary entry %s/%s: %w", code.Kind, code.Code, err)
		}
		report.CodesWritten += int(tag.RowsAffected())
	}

	// VARIANT_OF is a self-reference: an exercise may vary one that appears
	// later in the same file, so the links are attached after every row exists.
	deferredVariants := map[string]string{}
	for _, exercise := range seed.Exercises {
		outcome, err := upsertExercise(ctx, tx, exercise)
		if err != nil {
			return store.ExerciseSeedReport{}, err
		}
		switch outcome {
		case seedAdded:
			report.Added++
		case seedUpdated:
			report.Updated++
		default:
			report.Skipped++
		}
		if outcome != seedSkipped {
			if err := replaceCodeLinks(ctx, tx, exercise); err != nil {
				return store.ExerciseSeedReport{}, err
			}
		}
		if exercise.VariantOf != nil && *exercise.VariantOf != "" {
			deferredVariants[exercise.ID] = *exercise.VariantOf
		}
	}
	for id, variantOf := range deferredVariants {
		if _, err := tx.Exec(ctx, `UPDATE exercise SET variant_of = $2 WHERE id = $1`, id, variantOf); err != nil {
			return store.ExerciseSeedReport{}, fmt.Errorf("postgres: link variant %s -> %s: %w", id, variantOf, err)
		}
	}

	mentioned := make([]string, 0, len(seed.Exercises))
	for _, e := range seed.Exercises {
		mentioned = append(mentioned, e.ID)
	}
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM exercise WHERE NOT (id = ANY($1))`, mentioned).Scan(&report.Absent); err != nil {
		return store.ExerciseSeedReport{}, fmt.Errorf("postgres: count untouched exercises: %w", err)
	}

	const recordImport = `INSERT INTO exercise_import
	    (id, source, file_sha256, schema_version, content_version, content_locale,
	     added, updated, skipped, absent, codes_written)
	    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	if _, err := tx.Exec(ctx, recordImport, report.ImportID, seed.Source, seed.FileSHA256,
		seed.SchemaVersion, seed.ContentVersion, seed.ContentLocale,
		report.Added, report.Updated, report.Skipped, report.Absent, report.CodesWritten); err != nil {
		return store.ExerciseSeedReport{}, fmt.Errorf("postgres: record exercise import: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return store.ExerciseSeedReport{}, fmt.Errorf("postgres: commit exercise import: %w", err)
	}
	return report, nil
}

// exerciseImportLockID is the advisory-lock key that serializes imports. It is
// distinct from the migration runner's key.
const exerciseImportLockID int64 = 8_531_207_004

type seedOutcome int

const (
	seedAdded seedOutcome = iota
	seedUpdated
	seedSkipped
)

// upsertExercise writes one record, or reports that it was already stored
// unchanged. The `WHERE content_hash IS DISTINCT FROM` clause is what makes the
// import idempotent: a byte-identical record touches no row, so updated_at and
// revision do not move on a repeated run.
func upsertExercise(ctx context.Context, tx pgx.Tx, e store.Exercise) (seedOutcome, error) {
	blocks, err := marshalBlocks(e)
	if err != nil {
		return seedSkipped, err
	}

	const q = `INSERT INTO exercise (
	        id, slug, legacy_number, schema_version, content_version, content_locale,
	        content_hash, revision, name_ru, name_en, aliases,
	        sport, section, category, movement_pattern, difficulty, laterality,
	        sort_key, search_text, technique, programming, safety, media, qa,
	        publication_status, review_status, media_status, created_at, updated_at)
	    VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8, $9, $10,
	            $11, $12, $13, $14, $15, $16, $17, $18,
	            $19, $20, $21, $22, $23, $24, $25, $26, $27, $27)
	    ON CONFLICT (id) DO UPDATE SET
	        slug = EXCLUDED.slug,
	        legacy_number = EXCLUDED.legacy_number,
	        schema_version = EXCLUDED.schema_version,
	        content_version = EXCLUDED.content_version,
	        content_locale = EXCLUDED.content_locale,
	        content_hash = EXCLUDED.content_hash,
	        revision = exercise.revision + 1,
	        name_ru = EXCLUDED.name_ru,
	        name_en = EXCLUDED.name_en,
	        aliases = EXCLUDED.aliases,
	        sport = EXCLUDED.sport,
	        section = EXCLUDED.section,
	        category = EXCLUDED.category,
	        movement_pattern = EXCLUDED.movement_pattern,
	        difficulty = EXCLUDED.difficulty,
	        laterality = EXCLUDED.laterality,
	        sort_key = EXCLUDED.sort_key,
	        search_text = EXCLUDED.search_text,
	        technique = EXCLUDED.technique,
	        programming = EXCLUDED.programming,
	        safety = EXCLUDED.safety,
	        media = EXCLUDED.media,
	        qa = EXCLUDED.qa,
	        publication_status = EXCLUDED.publication_status,
	        review_status = EXCLUDED.review_status,
	        media_status = EXCLUDED.media_status,
	        updated_at = EXCLUDED.updated_at
	      WHERE exercise.content_hash IS DISTINCT FROM EXCLUDED.content_hash
	    RETURNING (xmax = 0) AS inserted`

	now := time.Now().UTC()
	var inserted bool
	err = tx.QueryRow(ctx, q,
		e.ID, e.Slug, e.LegacyNumber, e.SchemaVersion, e.ContentVersion, e.ContentLocale,
		e.ContentHash, e.NameRu, e.NameEn, e.Aliases,
		e.Sport, e.Section, e.Category, e.MovementPattern, e.Difficulty, e.Laterality,
		e.SortKey, e.SearchText, blocks.technique, blocks.programming, blocks.safety, blocks.media, blocks.qa,
		e.PublicationStatus, e.ReviewStatus, e.MediaStatus, now).Scan(&inserted)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// The WHERE on the DO UPDATE suppressed the write: the stored record is
		// byte-identical to the one in the file.
		return seedSkipped, nil
	case err != nil:
		return seedSkipped, fmt.Errorf("postgres: write exercise %q: %w", e.ID, err)
	case inserted:
		return seedAdded, nil
	default:
		return seedUpdated, nil
	}
}

type exerciseBlocks struct{ technique, programming, safety, media, qa []byte }

func marshalBlocks(e store.Exercise) (exerciseBlocks, error) {
	var out exerciseBlocks
	for _, block := range []struct {
		src any
		dst *[]byte
	}{
		{e.Technique, &out.technique}, {e.Programming, &out.programming},
		{e.Safety, &out.safety}, {e.Media, &out.media}, {e.QA, &out.qa},
	} {
		raw, err := json.Marshal(block.src)
		if err != nil {
			return exerciseBlocks{}, fmt.Errorf("postgres: encode exercise block of %q: %w", e.ID, err)
		}
		*block.dst = raw
	}
	return out, nil
}

// replaceCodeLinks rewrites the multi-valued codes of one record. Deleting and
// re-inserting is deliberate: a code the new content dropped must disappear,
// and the whole thing is inside the import's transaction.
func replaceCodeLinks(ctx context.Context, tx pgx.Tx, e store.Exercise) error {
	if _, err := tx.Exec(ctx, `DELETE FROM exercise_code_link WHERE exercise_id = $1`, e.ID); err != nil {
		return fmt.Errorf("postgres: clear codes of %q: %w", e.ID, err)
	}
	groups := []struct {
		relation string
		codes    []string
	}{
		{store.RelationEquipment, e.Equipment},
		{store.RelationPrimaryMuscle, e.PrimaryMuscles},
		{store.RelationSecondaryMuscle, e.SecondaryMuscles},
		{store.RelationJoint, e.Joints},
		{store.RelationGoalTag, e.GoalTags},
	}
	for _, group := range groups {
		kind := store.RelationKind[group.relation]
		for position, code := range group.codes {
			const q = `INSERT INTO exercise_code_link (exercise_id, relation, kind, code, position)
			           VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`
			if _, err := tx.Exec(ctx, q, e.ID, group.relation, kind, code, position); err != nil {
				return fmt.Errorf("postgres: link %s %q to %q: %w", group.relation, code, e.ID, err)
			}
		}
	}
	return nil
}

func loadExerciseIdentities(ctx context.Context, tx pgx.Tx) ([]store.ExerciseIdentity, error) {
	// FOR UPDATE holds the identities still while the import decides, so a
	// concurrent import cannot slip a rename past the check.
	const q = `SELECT id, slug, legacy_number FROM exercise FOR UPDATE`
	rows, err := tx.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("postgres: load exercise identities: %w", err)
	}
	defer rows.Close()

	out := []store.ExerciseIdentity{}
	for rows.Next() {
		var identity store.ExerciseIdentity
		if err := rows.Scan(&identity.ID, &identity.Slug, &identity.LegacyNumber); err != nil {
			return nil, fmt.Errorf("postgres: scan exercise identity: %w", err)
		}
		out = append(out, identity)
	}
	return out, rows.Err()
}

func loadKnownCodes(ctx context.Context, tx pgx.Tx) (map[store.CodeKey]bool, error) {
	rows, err := tx.Query(ctx, `SELECT kind, code FROM exercise_code`)
	if err != nil {
		return nil, fmt.Errorf("postgres: load dictionaries: %w", err)
	}
	defer rows.Close()

	known := map[store.CodeKey]bool{}
	for rows.Next() {
		var key store.CodeKey
		if err := rows.Scan(&key.Kind, &key.Code); err != nil {
			return nil, fmt.Errorf("postgres: scan dictionary entry: %w", err)
		}
		known[key] = true
	}
	return known, rows.Err()
}
