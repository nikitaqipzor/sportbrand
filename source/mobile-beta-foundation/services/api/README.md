# Athletica AI — API service

Go service for the Phase 1 beta loop: **health, authentication with real logout, the
workout lifecycle, idempotent set logging, workout history, progress aggregates and the
exercise reference book** —
everything behind `Сегодня → активная тренировка → лог подхода → офлайн → синхронизация →
Итоги → Прогресс`. Nothing beyond that is implemented, on purpose (see
[Deliberately deferred](#deliberately-deferred)).

`api/openapi.yaml` is the source of truth at the boundary (ADR 0001); this README explains
how to run what the contract describes.

## What it does

| Endpoint | Notes |
| --- | --- |
| `GET /api/v1/health`, `GET /health` | Liveness plus a real database ping; `503` when the store is unreachable |
| `POST /api/v1/auth/register` | bcrypt-hashed account, returns an access + refresh token pair |
| `POST /api/v1/auth/login` | Identical answer for unknown address and wrong password |
| `POST /api/v1/auth/refresh` | Rotating refresh tokens; replaying a rotated token revokes the whole family |
| `POST /api/v1/auth/logout` | Revokes the presented refresh token; `204` whatever was presented |
| `POST /api/v1/auth/logout-all` | Revokes every session of the account behind the access token |
| `GET /api/v1/auth/me` | The account behind the current access token |
| `POST /api/v1/workouts` | Starts a workout owned by the caller |
| `GET /api/v1/workouts` | The caller's history: status/date filters, keyset pagination |
| `GET /api/v1/workouts/{workoutId}` | One workout with its sets and totals — the "Итоги" screen |
| `PATCH /api/v1/workouts/{workoutId}` | Names a workout started offline without one |
| `POST /api/v1/workouts/{workoutId}/status` | The lifecycle: pause, resume, complete, **cancel** |
| `POST /api/v1/workouts/{workoutId}/sets` | **The core write**: idempotent, user-scoped, domain-validated |
| `GET /api/v1/workouts/{workoutId}/sets` | The caller's sets of the caller's workout |
| `PATCH /api/v1/workouts/{workoutId}/sets/{setId}` | **Corrects** a mistyped weight, repetitions or RIR — idempotently |
| `DELETE /api/v1/workouts/{workoutId}/sets/{setId}` | **Removes** a set, softly; repeating it is safe |
| `GET /api/v1/progress` | Strength records, weekly volume and adherence — the "Прогресс" screen |
| `GET /api/v1/exercises` | The **exercise catalogue**: code filters, name search, keyset pagination |
| `GET /api/v1/exercises/{exerciseId}` | One exercise card — the "Упражнение" and "Техника" screens |
| `GET /api/v1/exercise-dictionaries` | Every machine code with its Russian name, so filters are not hard-coded |

The base path is configurable and defaults to `/api/v1`, which is what the Android client
expects at `http://10.0.2.2:8080/api/v1`.

## Three invariants worth stating explicitly

1. **The user ID comes from the access token and nowhere else.** `POST /workouts/{id}/sets`
   ignores a `userId` in the body; the handler only ever sees the subject of the verified JWT.
2. **Idempotency is a database guarantee, not a Go check.** `workout_sets` has a unique index on
   `(user_id, client_mutation_id)` and the insert is `ON CONFLICT DO NOTHING … RETURNING`, so two
   concurrent retries of the same outbox entry produce one row, not two. A replay answers `409`
   with the originally stored set.
3. **A foreign workout is indistinguishable from a missing one.** Both answer `404` with the same
   body, and the composite foreign key `(workout_id, user_id) → workouts (id, user_id)` makes a
   cross-user write impossible even if the application check were skipped. This holds on every
   new route too: a foreign workout is absent from `GET /workouts`, answers `404` on
   `GET /workouts/{id}` and on a status change, and contributes nothing to `GET /progress`.
   A pagination cursor carries no identity, so one copied from another account still only moves
   a position inside the caller's own rows.

A fourth rule joined them with the lifecycle: **a forbidden state change is a `409`, never a
`500` and never a silent no-op.**

## Correcting what was logged

A human regularly mistypes a weight or a rep count, and until the "Итоги" and "Прогресс"
screens can be fixed they simply lie. `PATCH` and `DELETE` on a set are how they are fixed —
and both are ordinary offline-outbox mutations, not admin operations:

* **Both carry their own `clientMutationId`,** and both are settled by a unique index —
  `client_mutations (user_id, client_mutation_id)`, which migration `0004` adds — rather than
  by a read-then-write check in Go. Claiming the ID and applying the change happen in one
  transaction, so sixteen concurrent retries of one queued edit apply it exactly once.
  A replayed edit answers `409 duplicate_client_mutation` with the stored set; recycling an ID
  for a *different* set, or for a different kind of change, is the same `409` and writes
  nothing.
* **Deletion is soft, and repeating it is safe.** The row stays and keeps holding its
  `(user_id, client_mutation_id)` slot, which is what stops a replayed *creation* out of the
  outbox from resurrecting a set the athlete removed — that replay answers `409` with
  `deletedAt` set. A second `DELETE` answers `200` with the already-deleted set, because the
  state it asks for is the state that already holds; that is the one place in this API where a
  replay is not a `409`. A deleted set is absent from the workout detail, from the set list and
  from every figure in `GET /progress` — a partial index `WHERE deleted_at IS NULL` is what
  removes it, so no aggregate has to remember to.
* **Set numbers never shift.** Deleting set 2 of 4 leaves `1, 3, 4`. The client's mutation ID
  is `workoutId:exerciseId:setNumber`, so renumbering would make an already-spent ID name a
  different set, and the next set logged as "max + 1" would collide with one already accepted.
  The numbers are data the athlete produced, not positions in a list; gaps are normal. For the
  same reason `exerciseId` and `setNumber` are not correctable at all — a set logged against
  the wrong exercise is deleted and logged again.
* **A completed workout is still correctable.** The typo is usually noticed *on* the "Итоги"
  screen, which only exists once the session is finished; refusing the edit there would leave
  the wrong number permanently unfixable, which is exactly the problem this endpoint solves.
  An offline queue also delivers an edit after the completion it was queued behind. Only a
  `cancelled` workout is closed to edits (`409 workout_not_editable`): that session was thrown
  away and its sets already count towards nothing, so rewriting them would only add noise to a
  discarded log.
* A correction is held to the **same domain bounds** as the original write (weight 0–1000 kg,
  1–100 repetitions, RIR 0–10), so editing is not a way around them — in the service *and* in
  the `CHECK` constraints underneath.

```bash
SET=$(curl -s -X POST $BASE/workouts/$WORKOUT/sets -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' -d "$BODY" | jq -r .id)

# Correct it; the replay is a 409 and is not applied twice
curl -s -X PATCH $BASE/workouts/$WORKOUT/sets/$SET -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' \
  -d '{"weightKg":65,"repetitions":9,"rir":1,"clientMutationId":"outbox-1:update:1"}' | jq .weightKg

# Remove it; the second call answers 200 with the same deleted set
curl -s -X DELETE $BASE/workouts/$WORKOUT/sets/$SET -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' -d '{"clientMutationId":"outbox-1:delete"}' | jq .deletedAt

# Name a session that was started offline
curl -s -X PATCH $BASE/workouts/$WORKOUT -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' -d '{"title":"Pull day"}' | jq -r .title
```

## The exercise reference book

918 exercises live in a Word file the app cannot read. Converting them into a versioned
JSON contract is a separate job in `content/`; this service is what stores the result,
serves it and — above all — refuses to break it.

### The one rule everything else follows from

**An `exerciseId` is immutable.** It already left the phone inside `clientMutationId`
(`workoutId:exerciseId:setNumber`) and is stored in `workout_sets`. A catalogue that
renamed an identifier would silently detach an athlete's recorded history from the
exercise it was performed with — the numbers would still be there, attached to nothing.

Three consequences, all of them deliberate:

* **The importer refuses a rename.** If a file gives an existing `SLUG` or `LEGACY_NUMBER`
  to a different `EXERCISE_ID`, the whole file is rejected and nothing is written — not
  even the records that had nothing to do with the rename. The refusal names both
  identifiers. `DetectRenames` in `internal/store` is where it lives, and both store
  implementations call it *inside* the critical section that writes, so a concurrent
  import cannot slip past the check.
* **The importer deletes nothing.** A record already stored that a file does not mention
  is counted as `absent` and left alone. A set may still name it.
* **There is no foreign key from `workout_sets` to `exercise`.** A set logged against an
  identifier the catalogue has not shipped yet — or has retired — still records. The
  training history outranks the reference book, and `POST /workouts/{id}/sets` must never
  fail because a curator has not got to that exercise yet.

### Three independent statuses

The master template keeps publication, expert review and media readiness apart, and so
does the schema:

| Column | Vocabulary |
| --- | --- |
| `publication_status` | `draft` → `in_review` → `ready` → `published` → `archived` |
| `review_status` | `draft`, `in_review`, `approved`, `rejected` |
| `media_status` | `draft`, `in_review`, `approved`, `rejected` |

A record is visible to an ordinary user only at `published` + `approved` + `approved`.
That is a generated column (`is_published`), so no query has to remember the rule, and a
`CHECK` makes an unreviewed *published* row unrepresentable rather than merely unlikely —
`ready` is the state a record holds before it flips, and both approvals must still stand
after it has. Anything short of all three answers `404`, byte-identical to an identifier
that never existed, so a draft cannot be probed for.

### Empty is honest

Step-by-step technique, common errors, stop signs and contraindications **are not in the
source at all**. They are in the schema, they come back as empty arrays, and
`hasTechnique` / `hasSafety` say so outright. Nothing fills them with something plausible,
because a person follows what a contraindication field says. The same rule the audit
applied to the invented "готовность 78" applies here, and it is pinned by a test.

`difficulty` is `null` in the starter set for the same reason: the source has a level
column, it has not been converted yet, and `null` is not "beginner". The `difficulty`
dictionary still ships all three levels, so the client can build the filter today and it
starts returning rows the moment the real content lands.

### Loading content

```bash
go run ./cmd/api seed-exercises                        # the embedded starter set
go run ./cmd/api seed-exercises --file content/export.json
```

The command prints `added=… updated=… skipped=… absent=… codes=…` and records the same
counts, plus the file's SHA-256, in `exercise_import` — so "which content is live" has an
answer that does not depend on somebody remembering.

* **Idempotent.** Each record carries a `content_hash` — the SHA-256 of the record exactly
  as the file stated it. A re-import compares equal, writes nothing, and does not move
  `updated_at` or `revision`. Running it twice is a no-op; running it after one record
  changed updates that one record.
* **Atomic.** One transaction, under an advisory lock. A refused file leaves the catalogue
  exactly as it was.
* **Coded.** Every machine code an exercise uses must exist in a dictionary — the file's
  own or one already stored — or the import is refused. In PostgreSQL the foreign keys
  enforce it a second time.

### The import contract

The file is JSON. Field names are the master template's own machine names, and they are
matched **case-insensitively with separators ignored**, so `EXERCISE_ID`, `exercise_id`
and `exerciseId` are one field — the content pipeline is being written in parallel and
must not be able to miss by a convention. Re-spelling a file does not change its content
hashes, so a cosmetic diff does not rewrite every row.

```json
{
  "SCHEMA_VERSION": 1,
  "CONTENT_VERSION": 7,
  "CONTENT_LOCALE": "ru-RU",
  "DICTIONARIES": {
    "sport":     [{ "CODE": "strength", "NAME_RU": "Силовая тренировка", "SORT_ORDER": 1 }],
    "section":   [{ "CODE": "legs", "NAME_RU": "Ноги" }],
    "equipment": [{ "CODE": "barbell", "NAME_RU": "Штанга" }],
    "muscle":    [{ "CODE": "quadriceps", "NAME_RU": "Квадрицепс" }]
  },
  "EXERCISES": [
    {
      "EXERCISE_ID": "back-squat",
      "SLUG": "back-squat",
      "LEGACY_NUMBER": 1,
      "NAME_RU": "Приседания со штангой",
      "SPORT": "strength",
      "SECTION": "legs",
      "EQUIPMENT": ["barbell"],
      "PRIMARY_MUSCLES": ["quadriceps"],
      "PUBLICATION_STATUS": "published",
      "REVIEW_STATUS": "approved",
      "MEDIA_STATUS": "approved"
    }
  ]
}
```

Blocks C–G of the template (`SETUP`, `EXECUTION_STEPS`, `COMMON_ERRORS`,
`CONTRAINDICATIONS`, `MAIN_ASSET_ID`, `SOURCES`, `REVIEWERS` …) are all accepted and all
optional; see `internal/exercises/seed.go` for the complete field list. Choices the brief
left open, made here and written down so `content/` can match them:

| Question | Decision |
| --- | --- |
| Where do dictionaries live? | In the same file, under `DICTIONARIES`, keyed by kind. One file is one atomic import. |
| Is `SLUG` required? | No — it defaults to `EXERCISE_ID`. It is still a stable identity the rename guard watches. |
| What if a record is omitted? | Left alone and reported as `absent`. Never deleted, never archived automatically. |
| What identifies "unchanged"? | `content_hash`, the SHA-256 of the normalized record. Not a column-by-column diff. |
| Where does `CONTENT_VERSION` come from? | The file, unless a record states its own. |
| What is `sort_key`? | The folded Russian name plus the identifier, computed **in Go**. PostgreSQL's `lower()` under the `C` collation does not fold Cyrillic, so letting SQL do it would make the two store implementations disagree about where a page ends. |

Blocks C–G are stored as `jsonb` documents rather than fifty columns: they are free text,
and the contract forbids filtering on free text, so columns would buy nothing. Everything
the catalogue filters or sorts on is a real column with a real foreign key.

### The starter set

`seed/exercises.starter.json` is embedded in the binary and holds **the twenty
identifiers the app already ships**, taken verbatim from
`apps/mobile/src/features/workout/exercise-catalog.ts`. Two tests keep it that way: one
checks the identifiers against that list, the other checks that no methodology has crept
in. Its records are published with `REVIEW_NOTES` stating exactly what was approved —
the identifier, the Russian name and the classification, and nothing else, because
nothing else is there.

### Searching and filtering

`q` is matched as a **literal, case-folded substring** of a precomputed field
(`position($1 IN search_text)`), never as `LIKE` and never as a regular expression. `%`,
`_`, `\` and a quote are ordinary characters; an empty or whitespace-only query is not a
filter but the search box mid-deletion; an absurdly long one is truncated at a rune
boundary rather than refused. Every other filter takes machine codes only — a code no
dictionary defines matches nothing rather than answering `400`, because that is a client
built against an older dictionary and an empty page is kinder than an error it cannot act
on.

Pagination is the same keyset scheme as `GET /workouts`, ordered by `(sort_key, id)` with
both columns declared `COLLATE "C"` so PostgreSQL compares the bytes Go compares.

```bash
BASE=http://localhost:8080/api/v1
curl -s "$BASE/exercises?section=back&equipment=cable" -H "authorization: Bearer $TOKEN" | jq '[.items[].id]'
curl -s -G "$BASE/exercises" --data-urlencode 'q=присед' -H "authorization: Bearer $TOKEN" | jq '[.items[].nameRu]'
curl -s "$BASE/exercises/back-squat" -H "authorization: Bearer $TOKEN" | jq '{id, nameRu, hasTechnique}'
curl -s "$BASE/exercise-dictionaries" -H "authorization: Bearer $TOKEN" | jq '[.dictionaries[] | {kind, n: (.items|length)}]'
```

## Metrics

Requests are still logged structurally; they are now also counted. The exposition is
Prometheus text on a **separate listener** — the public port has no `/metrics` route and
answers it with the ordinary `404`, so a misrouted proxy cannot expose it.

```bash
curl -s localhost:9091/metrics | head          # loopback by default
```

| Series | What it answers |
| --- | --- |
| `athletica_http_requests_total{route,method,status}` | Traffic and error rate per endpoint |
| `athletica_http_request_duration_seconds{route,method}` | Latency, as a cumulative histogram |
| `athletica_rate_limited_total{scope,reason}` | How often the auth throttle fires, by IP/account and rate/backoff |
| `athletica_db_pool_connections{state}`, `_max_connections`, `_acquires_total`, `_empty_acquires_total` | Pool saturation — `empty_acquires_total` is the number that turns "the pool is fine" into a fact |
| `athletica_migrations_pending`, `athletica_migrations_applied_total` | The migration queue seen at start-up |
| `athletica_build_info{version}` | Which build is answering |

**Nothing about a person is in there.** No e-mail, no user ID, no workout or set ID, no token,
no IP. The `route` label is the registered *template* — `/api/v1/workouts/{workoutId}/sets` —
never `r.URL.Path`, which carries UUIDs; every other label comes from a fixed compile-time
vocabulary, so the series count is bounded and no request can invent a label. The gauges for a
store that is not there (the in-memory driver has no pool and no schema) are simply absent
rather than a misleading zero.

`ATHLETICA_METRICS_ADDR` defaults to `127.0.0.1:9091`; set it empty to disable the listener.
Binding it anywhere reachable from outside the host **without** `ATHLETICA_METRICS_TOKEN` is a
start-up failure, and so is pointing it at the public listener's address. With a token set,
`/metrics` demands `Authorization: Bearer …` and compares it in constant time.

## The workout lifecycle

| from | to |
| --- | --- |
| `active` | `paused`, `completed`, `cancelled` |
| `paused` | `active`, `completed`, `cancelled` |
| `completed` | — terminal |
| `cancelled` | — terminal |

Cancelling is reachable from every unfinished status — audit blocker **QA-004** was that a
started session could not be abandoned at all. Asking for the status a workout already holds is
an accepted no-op (`200`), so a client retrying a request whose answer it never saw is not
punished for work it already did; anything else the table forbids is `409 invalid_transition`.

The check and the write are one statement (`UPDATE … WHERE id = $1 AND user_id = $2 AND
status = ANY($n) RETURNING …`), so two concurrent transitions cannot both succeed, and
`workouts_ended_at_matches_status` makes a half-applied transition unrepresentable: `ended_at`
exists exactly for the two terminal statuses.

## What `GET /progress` counts

* **Strength** — per exercise: the heaviest set, and the set with the highest Epley estimate
  `weightKg × (1 + repetitions / 30)`. RIR is deliberately not folded in: the number describes
  the set that was actually performed.
* **Volume** — sets, repetitions, `Σ weight × reps` and distinct workouts per ISO week.
* **Adherence** — how the workouts *started* in each ISO week ended, plus a completion rate.

Sets of `cancelled` workouts count towards none of it: a session the athlete threw away must not
become a personal record. Every figure is computed by `GROUP BY`/`DISTINCT ON` in PostgreSQL —
no set row is ever streamed into the service — and the requested window is snapped to whole ISO
weeks and clamped to 104 of them, so no query can ask for an unbounded scan.

## Run it

### docker compose (Postgres + API)

```bash
cd infra/compose
docker compose up --build          # postgres 16 + api on http://localhost:8080
curl -s localhost:8080/health
```

The API waits for the `postgres` health check, applies migrations on start and exposes port
8080 on the host — which is what `http://10.0.2.2:8080/api/v1` resolves to from an Android
emulator. Its own container health check calls `/api healthcheck` (the image is `scratch`
based and has no shell, curl or wget).

### Locally against a database

```bash
cd infra/compose && docker compose up -d postgres

cd services/api
export ATHLETICA_DATABASE_URL="postgres://athletica:local-development-only@localhost:5432/athletica?sslmode=disable"
go run ./cmd/api                   # migrations run at start-up
```

### Locally with no database at all

```bash
ATHLETICA_STORE_DRIVER=memory go run ./cmd/api
```

The in-memory store keeps everything in the process and is refused outright when
`ATHLETICA_ENV=production`.

### Migrations as a separate step

```bash
go run ./cmd/api migrate up        # apply everything pending
go run ./cmd/api migrate down      # roll the newest migration back
go run ./cmd/api migrate down 2    # roll back two
```

Migrations live in `migrations/NNNN_name.{up,down}.sql`, are embedded into the binary and are
applied under a PostgreSQL advisory lock, so several replicas may start at once. Set
`ATHLETICA_MIGRATE_ON_START=false` to keep start-up and schema changes separate.

## Logout and token housekeeping

`POST /auth/logout` needs no access token (a client logging out may hold an expired one) and
answers `204` whatever it is given, so it cannot be used to probe which refresh handles exist.
`allSessions: true`, or `POST /auth/logout-all` with a valid access token, revokes every session
of the account.

Revocation records *why*: a token spent by rotation and then presented again is a stolen-token
signal and still revokes the whole family, while one the athlete logged out is simply refused.
Without that distinction — which is what migration `0003` adds — a background refresh racing a
logout on the same phone would sign them out of every other device. A live run against
PostgreSQL is what exposed it; `TestRefreshingALoggedOutTokenDoesNotKillOtherDevices` keeps it
fixed.

**The access token already issued stays valid until it expires** (15 minutes by default): there
is no access-token denylist, and adding one is a deliberate later decision. A client that logs
out must discard both tokens locally.

Expired and revoked refresh rows are deleted by an in-process sweeper
(`ATHLETICA_REFRESH_TOKEN_SWEEP_INTERVAL`, hourly by default, `0` disables it), or on demand:

```bash
go run ./cmd/api prune-tokens
```

## Smoke test

```bash
BASE=http://localhost:8080/api/v1
TOKEN=$(curl -s -X POST $BASE/auth/register -H 'content-type: application/json' \
  -d '{"email":"athlete@example.com","password":"correct-horse-battery"}' | jq -r .accessToken)
WORKOUT=$(curl -s -X POST $BASE/workouts -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' -d '{"title":"Pull day"}' | jq -r .id)

BODY='{"exerciseId":"lat-pulldown","setNumber":2,"weightKg":62.5,"repetitions":10,"rir":2,"clientMutationId":"outbox-1"}'
curl -s -o /dev/null -w '%{http_code}\n' -X POST $BASE/workouts/$WORKOUT/sets \
  -H "authorization: Bearer $TOKEN" -H 'content-type: application/json' -d "$BODY"   # 201
curl -s -o /dev/null -w '%{http_code}\n' -X POST $BASE/workouts/$WORKOUT/sets \
  -H "authorization: Bearer $TOKEN" -H 'content-type: application/json' -d "$BODY"   # 409, still one row

# The lifecycle, the history and the progress screen
curl -s -X POST $BASE/workouts/$WORKOUT/status -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' -d '{"status":"completed"}' | jq -r .status   # completed
curl -s -X POST $BASE/workouts/$WORKOUT/status -H "authorization: Bearer $TOKEN" \
  -H 'content-type: application/json' -d '{"status":"active"}' | jq -r .error.code  # invalid_transition (409)
curl -s "$BASE/workouts?status=completed&limit=20" -H "authorization: Bearer $TOKEN" | jq '.items|length'
curl -s $BASE/workouts/$WORKOUT -H "authorization: Bearer $TOKEN" | jq '.totals'
curl -s $BASE/progress -H "authorization: Bearer $TOKEN" | jq '.strength[0].bestEstimated1Rm'
```

## Tests

```bash
cd services/api
go build ./...
go vet ./...
go test ./...
go test -race ./...
```

Everything runs against the in-memory store and needs no database. The **same** store
conformance suite (`internal/store/storetest`) also runs against real PostgreSQL when you point
it at one, which is how the SQL adapter and the unique index are covered:

```bash
ATHLETICA_TEST_DATABASE_URL="postgres://athletica:local-development-only@localhost:5432/athletica?sslmode=disable" \
  go test -count=1 ./internal/store/postgres/
```

Covered invariants: idempotent replay (including a concurrent one), per-user isolation of reads
and writes, domain bounds at their edges, refusal to start with a development JWT secret in
production, per-IP and per-account auth throttling with growing backoff, identical answers for an
unknown address and a wrong password — and, since Phase 1: every one of the sixteen
`(from, to)` status pairs, cancelling from each unfinished status, the keyset cursor at an exact
page boundary, a cursor copied from another account, revocation after logout and after
logout-all, the refresh-token sweep, and the progress aggregates including their two exclusions
(another athlete's sets, and the sets of a cancelled workout).

Since the corrections landed the same suite also pins: a replayed edit that is *not* applied
twice (including sixteen concurrent retries of one queued edit), a repeated deletion that stays
safe, a deleted set vanishing from the detail **and** from the progress aggregates, a replayed
creation that cannot resurrect it, set numbers keeping their gaps, another athlete's set being
neither editable nor deletable behind a `404` byte-identical to a missing one, a correction
being unable to leave the domain bounds, a `clientMutationId` that cannot be recycled for a
second change — and, for the observability half, that `/metrics` is a plain `404` on the public
port, that the metrics listener refuses an anonymous scrape when a token is configured, and that
no workout ID, set ID, user ID, e-mail, token or IP appears anywhere on the page.

The reference book added six more groups to the same suite, and they are the ones worth
naming: a repeated import that writes nothing at all, an import that would rename an
identifier being refused **whole** (the innocent record beside the rename does not land
either), a record short of any one of the three statuses being invisible to both the list
and the card, the catalogue paged one row at a time producing exactly the unpaged answer
under every filter, a search that treats `%`, `_`, `\`, a quote and a whole `DROP TABLE`
as ordinary letters, every code in an answer existing in the dictionaries, and an import
that omits a record leaving it alone rather than deleting it. Running that live against
PostgreSQL is what caught two things the in-memory store had happily accepted: a `sort_key`
containing a NUL byte, which `text` cannot store at all, and a nil alias list arriving as
`NULL` in a `NOT NULL` column.

## Configuration

Everything comes from the environment. Unknown or unsafe values fail the start, they never warn.

| Variable | Default | Purpose |
| --- | --- | --- |
| `ATHLETICA_ENV` | `development` | `development`, `staging` or `production` |
| `ATHLETICA_HTTP_ADDR` | `:8080` | Listen address |
| `ATHLETICA_BASE_PATH` | `/api/v1` | Path prefix for every route except `/health` |
| `ATHLETICA_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `ATHLETICA_STORE_DRIVER` | `postgres` | `postgres` or `memory` (`memory` is refused in production) |
| `ATHLETICA_DATABASE_URL` | — | Required for the `postgres` driver (`DATABASE_URL` also accepted) |
| `ATHLETICA_MIGRATE_ON_START` | `true` | Apply pending migrations during start-up |
| `ATHLETICA_JWT_SECRET` | dev placeholder | HMAC key; `AUTH_TOKEN_SECRET` is accepted as an alias |
| `ATHLETICA_JWT_ISSUER` | `athletica-api` | `iss` claim |
| `ATHLETICA_ACCESS_TOKEN_TTL` | `15m` | Must stay ≤ 1h |
| `ATHLETICA_REFRESH_TOKEN_TTL` | `720h` | Refresh token lifetime |
| `ATHLETICA_BCRYPT_COST` | `12` | Lower it only in tests |
| `ATHLETICA_AUTH_RATE_LIMIT` | `10` | Auth requests per window, per key |
| `ATHLETICA_AUTH_RATE_WINDOW` | `1m` | Length of that window |
| `ATHLETICA_AUTH_FAILURE_LIMIT` | `5` | Consecutive failures before the lockout starts |
| `ATHLETICA_AUTH_BACKOFF_BASE` | `2s` | First lockout, doubling each further failure |
| `ATHLETICA_AUTH_BACKOFF_MAX` | `15m` | Lockout ceiling |
| `ATHLETICA_REFRESH_TOKEN_SWEEP_INTERVAL` | `1h` | Background sweep of dead refresh rows; `0` disables it |
| `ATHLETICA_REFRESH_TOKEN_RETENTION` | `24h` | How long an expired or revoked row is kept before the sweep may delete it |
| `ATHLETICA_TRUST_PROXY_HEADERS` | `false` | Honour `X-Forwarded-For` — only turn on behind a proxy you control |
| `ATHLETICA_SHUTDOWN_TIMEOUT` | `15s` | Grace period for in-flight requests |
| `ATHLETICA_METRICS_ADDR` | `127.0.0.1:9091` | Separate Prometheus listener; empty disables it |
| `ATHLETICA_METRICS_TOKEN` | — | Bearer token `/metrics` demands; required for any non-loopback bind |

**Production refuses to start** when the signing secret is empty, is a known placeholder such as
`dev-only-change-me`, or is shorter than 32 characters. The message names the variable and how to
generate a real value. Staging is held to the same standard.

## Layout

```
services/api
├── api/openapi.yaml            contract (source of truth)
├── cmd/api                     main: serve | migrate | prune-tokens | seed-exercises | healthcheck | version
├── internal/config             environment parsing + the start-up safety rules
├── internal/auth               bcrypt hashing, HS256 tokens, register/login/refresh
├── internal/workouts           domain bounds (mirrors packages/domain) + use cases
├── internal/exercises          the reference book: filters, cursor, import parsing
├── internal/ratelimit          per-IP and per-account throttling with backoff
├── internal/metrics            Prometheus text registry (no dependencies, no user data)
├── internal/store              Store interface + models
│   ├── memory                  in-process implementation (tests, local runs)
│   ├── postgres                pgx implementation + migration runner
│   └── storetest               conformance suite both implementations must pass
├── internal/httpapi            router, middleware, handlers, the metrics listener
├── internal/ids                UUID and opaque-token generation
├── seed                        the embedded starter catalogue (the app's own 20 IDs)
├── migrations                  NNNN_name.{up,down}.sql, embedded into the binary
└── Dockerfile                  multi-stage, scratch, non-root (uid 10001)
```

Only the standard library plus `pgx` and `golang.org/x/crypto` — no HTTP framework, no ORM,
no JWT library (HS256 signing and verification is ~80 lines and lets us reject `alg: none`
ourselves).

## Deliberately deferred

- Nutrition, recovery, the AI coach, wearables and media uploads — out of scope for the beta loop.
- **The exercise content itself.** The schema, the importer and the endpoints are here;
  the 918 records are produced in `content/` and arrive through `api seed-exercises`.
  Until they do, the catalogue holds the twenty starter records and every technique and
  safety field on them is empty.
- Editing the catalogue over HTTP. It is imported, not authored: there is no write
  endpoint, and the only way content changes is a reviewed file going through
  `seed-exercises`.
- Media itself. `mediaStatus` and the asset IDs exist; nothing serves or stores an image.
- Access-token revocation. Logout revokes the refresh token immediately, but the access token in
  the client's hands stays valid for its remaining lifetime (≤ 15 minutes); a denylist or short
  server-side session check is the next step if that window turns out to matter.
- A training *plan*: `adherence` therefore measures how many started sessions were finished,
  not conformance to a prescribed schedule. It gains the second meaning once plans exist.
- Password reset, e-mail verification, account deletion.
- Rate-limit state is per process, which is right for one container and needs Redis or an
  equivalent once the API is replicated.
- Tracing. Requests are logged structurally and counted (see [Metrics](#metrics)), but there
  are no spans and no propagated trace context yet.
- Hard-deleting a set. Removal is soft, which is what keeps the idempotency slot and the safe
  repeat; a retention job that eventually purges rows deleted long ago is a later decision.
- Undoing a deletion. The row is still there, so restoring it would be a small change, but it
  needs its own mutation kind and its own place in the client's outbox, and nothing asks for
  it yet.
- The PostgreSQL conformance suite is opt-in through an environment variable; wiring a disposable
  database into CI is the remaining half of audit finding H3.
