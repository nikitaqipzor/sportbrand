# Athletica AI — API service

Go service for the Phase 1 beta loop: **health, authentication with real logout, the
workout lifecycle, idempotent set logging, workout history and progress aggregates** —
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
| `POST /api/v1/workouts/{workoutId}/status` | The lifecycle: pause, resume, complete, **cancel** |
| `POST /api/v1/workouts/{workoutId}/sets` | **The core write**: idempotent, user-scoped, domain-validated |
| `GET /api/v1/workouts/{workoutId}/sets` | The caller's sets of the caller's workout |
| `GET /api/v1/progress` | Strength records, weekly volume and adherence — the "Прогресс" screen |

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

**Production refuses to start** when the signing secret is empty, is a known placeholder such as
`dev-only-change-me`, or is shorter than 32 characters. The message names the variable and how to
generate a real value. Staging is held to the same standard.

## Layout

```
services/api
├── api/openapi.yaml            contract (source of truth)
├── cmd/api                     main: serve | migrate | prune-tokens | healthcheck | version
├── internal/config             environment parsing + the start-up safety rules
├── internal/auth               bcrypt hashing, HS256 tokens, register/login/refresh
├── internal/workouts           domain bounds (mirrors packages/domain) + use cases
├── internal/ratelimit          per-IP and per-account throttling with backoff
├── internal/store              Store interface + models
│   ├── memory                  in-process implementation (tests, local runs)
│   ├── postgres                pgx implementation + migration runner
│   └── storetest               conformance suite both implementations must pass
├── internal/httpapi            router, middleware, handlers
├── internal/ids                UUID and opaque-token generation
├── migrations                  NNNN_name.{up,down}.sql, embedded into the binary
└── Dockerfile                  multi-stage, scratch, non-root (uid 10001)
```

Only the standard library plus `pgx` and `golang.org/x/crypto` — no HTTP framework, no ORM,
no JWT library (HS256 signing and verification is ~80 lines and lets us reject `alg: none`
ourselves).

## Deliberately deferred

- Nutrition, recovery, the AI coach, wearables and media uploads — out of scope for the beta loop.
- Access-token revocation. Logout revokes the refresh token immediately, but the access token in
  the client's hands stays valid for its remaining lifetime (≤ 15 minutes); a denylist or short
  server-side session check is the next step if that window turns out to matter.
- Editing or deleting a logged set, and editing a workout's title after creation.
- A training *plan*: `adherence` therefore measures how many started sessions were finished,
  not conformance to a prescribed schedule. It gains the second meaning once plans exist.
- Password reset, e-mail verification, account deletion.
- Rate-limit state is per process, which is right for one container and needs Redis or an
  equivalent once the API is replicated.
- Metrics and tracing: requests are logged structurally, but nothing is exported yet.
- The PostgreSQL conformance suite is opt-in through an environment variable; wiring a disposable
  database into CI is the remaining half of audit finding H3.
