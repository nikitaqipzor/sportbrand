-- 0005_exercise_catalog: the exercise reference book (Э3, server half).
--
-- Two things make this table different from everything above it:
--
--   1. **Its rows belong to nobody.** The catalogue is shared content, not user
--      data, so there is no user_id here and no statement in this migration can
--      reach a person's rows. Access is still authenticated; it is simply not
--      scoped.
--
--   2. **Its primary key is immutable content, not a surrogate.** `exercise_id`
--      already left the phone inside `client_mutation_id`
--      (`workoutId:exerciseId:setNumber`) and is stored in `workout_sets`.
--      Renaming an identifier afterwards would silently detach recorded history
--      from the exercise it was performed with. There is deliberately no
--      foreign key from workout_sets to exercise for the same reason: a set
--      logged against an identifier the catalogue has not shipped yet, or has
--      retired, must never fail to record.
--
-- Filtering is by machine codes only ("никакой фильтрации по свободному
-- тексту"): every code lives in exercise_code and every use of one is a foreign
-- key, so a response can never carry a code the dictionary endpoint does not
-- also return.

-- ---------------------------------------------------------------------------
-- Dictionaries. One table, discriminated by `kind`, so a single endpoint can
-- serve every filter the client builds and one foreign key shape covers them
-- all.
-- ---------------------------------------------------------------------------
CREATE TABLE exercise_code (
    kind       text NOT NULL
               CHECK (kind IN ('sport', 'section', 'category', 'movement_pattern',
                               'equipment', 'muscle', 'joint', 'goal_tag',
                               'difficulty', 'laterality')),
    code       text NOT NULL
               CHECK (code ~ '^[a-z0-9]+(?:[-_][a-z0-9]+)*$' AND char_length(code) <= 64),
    name_ru    text NOT NULL CHECK (char_length(name_ru) BETWEEN 1 AND 200),
    name_en    text NOT NULL DEFAULT '' CHECK (char_length(name_en) <= 200),
    sort_order integer NOT NULL DEFAULT 0,
    PRIMARY KEY (kind, code)
);

-- ---------------------------------------------------------------------------
-- The exercises themselves.
--
-- Blocks A (identification) and B (classification) are columns, because the
-- catalogue filters and sorts on them. Blocks C–G are jsonb documents with a
-- shape fixed by the Go structs in internal/store: they are free text, and the
-- contract forbids filtering on free text, so a column each would buy nothing
-- and cost fifty of them. They are present and they start empty — the source
-- encyclopedia has no technique, no common errors and no contraindications,
-- and inventing them is worse than leaving them blank, because people train on
-- what this table says.
-- ---------------------------------------------------------------------------
CREATE TABLE exercise (
    -- A. Identification. `id` is the string the phone already sends.
    id              text COLLATE "C" PRIMARY KEY
                    CHECK (id ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$' AND char_length(id) BETWEEN 1 AND 64),
    slug            text NOT NULL
                    CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$' AND char_length(slug) BETWEEN 1 AND 96),
    legacy_number   integer CHECK (legacy_number > 0),
    schema_version  integer NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    content_version integer NOT NULL DEFAULT 1 CHECK (content_version > 0),
    content_locale  text    NOT NULL DEFAULT 'ru-RU' CHECK (char_length(content_locale) BETWEEN 2 AND 16),
    -- content_hash is the fingerprint of the record as the import file stated
    -- it. Re-importing an unchanged record compares equal and is skipped, which
    -- is what makes `api seed-exercises` idempotent without a diff of columns.
    content_hash    text    NOT NULL CHECK (char_length(content_hash) = 64),
    -- revision counts accepted changes to this record; it moves only when the
    -- hash moves, so a repeated import does not inflate it.
    revision        integer NOT NULL DEFAULT 1 CHECK (revision > 0),

    name_ru         text NOT NULL CHECK (char_length(name_ru) BETWEEN 1 AND 200),
    name_en         text NOT NULL DEFAULT '' CHECK (char_length(name_en) <= 200),
    aliases         text[] NOT NULL DEFAULT '{}',
    -- The 28 repeated names of the source are linked, never deleted.
    variant_of      text REFERENCES exercise (id) ON DELETE SET NULL,

    -- B. Classification. Every one of these is a machine code.
    sport            text NOT NULL,
    section          text NOT NULL,
    category         text,
    movement_pattern text,
    difficulty       text,
    laterality       text,

    -- Sort and search keys are computed by the importer in Go, never by SQL:
    -- PostgreSQL's lower() under the C collation does not fold Cyrillic, and
    -- the in-memory store must produce byte-identical ordering and matching.
    sort_key    text COLLATE "C" NOT NULL,
    search_text text COLLATE "C" NOT NULL DEFAULT '',

    -- C–G. Present, shaped, and empty until the encyclopedia fills them.
    technique   jsonb NOT NULL DEFAULT '{}'::jsonb,
    programming jsonb NOT NULL DEFAULT '{}'::jsonb,
    safety      jsonb NOT NULL DEFAULT '{}'::jsonb,
    media       jsonb NOT NULL DEFAULT '{}'::jsonb,
    qa          jsonb NOT NULL DEFAULT '{}'::jsonb,

    -- The three independent statuses of the master template.
    publication_status text NOT NULL DEFAULT 'draft'
                       CHECK (publication_status IN ('draft', 'in_review', 'ready', 'published', 'archived')),
    review_status      text NOT NULL DEFAULT 'draft'
                       CHECK (review_status IN ('draft', 'in_review', 'approved', 'rejected')),
    media_status       text NOT NULL DEFAULT 'draft'
                       CHECK (media_status IN ('draft', 'in_review', 'approved', 'rejected')),

    -- Publication is allowed only at ready + approved + approved. The lifecycle
    -- draft → in_review → ready → published → archived means `ready` is the
    -- state a record must hold *before* it flips; once it has flipped, the two
    -- approvals must still stand. Expressing it as a CHECK makes an unreviewed
    -- published row unrepresentable rather than merely unlikely.
    CONSTRAINT exercise_publication_requires_approvals
        CHECK (publication_status <> 'published'
               OR (review_status = 'approved' AND media_status = 'approved')),

    -- What "visible to an ordinary user" means, computed once and indexed, so
    -- no query has to remember the rule.
    is_published boolean NOT NULL
                 GENERATED ALWAYS AS (publication_status = 'published'
                                      AND review_status = 'approved'
                                      AND media_status = 'approved') STORED,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- Secondary identities. They are what a rename would move: the importer
    -- refuses a file whose LEGACY_NUMBER or SLUG has changed owner.
    CONSTRAINT exercise_slug_key UNIQUE (slug),
    CONSTRAINT exercise_legacy_number_key UNIQUE (legacy_number),

    -- Every code is a foreign key. The generated companion column is what lets
    -- one dictionary table serve them all; NULL codes are simply unconstrained,
    -- which is how "the source did not say" stays representable.
    sport_kind            text GENERATED ALWAYS AS ('sport') STORED,
    section_kind          text GENERATED ALWAYS AS ('section') STORED,
    category_kind         text GENERATED ALWAYS AS ('category') STORED,
    movement_pattern_kind text GENERATED ALWAYS AS ('movement_pattern') STORED,
    difficulty_kind       text GENERATED ALWAYS AS ('difficulty') STORED,
    laterality_kind       text GENERATED ALWAYS AS ('laterality') STORED,

    FOREIGN KEY (sport_kind, sport)                       REFERENCES exercise_code (kind, code),
    FOREIGN KEY (section_kind, section)                   REFERENCES exercise_code (kind, code),
    FOREIGN KEY (category_kind, category)                 REFERENCES exercise_code (kind, code),
    FOREIGN KEY (movement_pattern_kind, movement_pattern) REFERENCES exercise_code (kind, code),
    FOREIGN KEY (difficulty_kind, difficulty)             REFERENCES exercise_code (kind, code),
    FOREIGN KEY (laterality_kind, laterality)             REFERENCES exercise_code (kind, code)
);

-- The catalogue's one total order: sort_key then id, both in the C collation so
-- PostgreSQL and the in-memory store compare bytes the same way and a keyset
-- cursor cannot skip or repeat a row.
CREATE UNIQUE INDEX exercise_catalog_order_idx ON exercise (sort_key, id);
CREATE INDEX exercise_published_order_idx ON exercise (sort_key, id) WHERE is_published;
CREATE INDEX exercise_sport_section_idx ON exercise (sport, section);
CREATE INDEX exercise_variant_of_idx ON exercise (variant_of) WHERE variant_of IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Multi-valued codes: equipment, muscles, joints, goals. `relation` is the role
-- the code plays, `kind` the dictionary it comes from — primary and secondary
-- muscles are two roles drawing on the same dictionary.
-- ---------------------------------------------------------------------------
CREATE TABLE exercise_code_link (
    exercise_id text NOT NULL REFERENCES exercise (id) ON DELETE CASCADE,
    relation    text NOT NULL
                CHECK (relation IN ('equipment', 'primary_muscle', 'secondary_muscle', 'joint', 'goal_tag')),
    kind        text NOT NULL,
    code        text NOT NULL,
    position    integer NOT NULL DEFAULT 0,
    PRIMARY KEY (exercise_id, relation, code),
    CONSTRAINT exercise_code_link_relation_kind CHECK (
        (relation = 'equipment'         AND kind = 'equipment') OR
        (relation = 'primary_muscle'    AND kind = 'muscle')    OR
        (relation = 'secondary_muscle'  AND kind = 'muscle')    OR
        (relation = 'joint'             AND kind = 'joint')     OR
        (relation = 'goal_tag'          AND kind = 'goal_tag')
    ),
    FOREIGN KEY (kind, code) REFERENCES exercise_code (kind, code)
);

CREATE INDEX exercise_code_link_lookup_idx ON exercise_code_link (relation, code, exercise_id);

-- ---------------------------------------------------------------------------
-- Every accepted import, so "which version of the content is live" has an
-- answer that does not depend on somebody remembering.
-- ---------------------------------------------------------------------------
CREATE TABLE exercise_import (
    id              uuid PRIMARY KEY,
    source          text NOT NULL DEFAULT '',
    file_sha256     text NOT NULL CHECK (char_length(file_sha256) = 64),
    schema_version  integer NOT NULL,
    content_version integer NOT NULL,
    content_locale  text NOT NULL DEFAULT '',
    added           integer NOT NULL DEFAULT 0,
    updated         integer NOT NULL DEFAULT 0,
    skipped         integer NOT NULL DEFAULT 0,
    absent          integer NOT NULL DEFAULT 0,
    codes_written   integer NOT NULL DEFAULT 0,
    imported_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX exercise_import_imported_at_idx ON exercise_import (imported_at DESC);
