# Athletica AI — API service

Go service for the Sprint 1 beta loop: **health, authentication, idempotent set logging**.
Nothing else is implemented yet, on purpose (see [Deliberately deferred](#deliberately-deferred)).

`api/openapi.yaml` is the source of truth at the boundary (ADR 0001); this README explains
how to run what the contract describes.

## What it does

| Endpoint | Notes |
| --- | --- |
| `GET /api/v1/health`, `GET /health` | Liveness plus a real database ping; `503` when the store is unreachable |
| `POST /api/v1/auth/register` | bcrypt-hashed account, returns an access + refresh token pair |
| `POST /api/v1/auth/login` | Identical answer for unknown address and wrong password |
| `POST /api/v1/auth/refresh` | Rotating refresh tokens; replaying a spent token revokes the whole family |
| `GET /api/v1/auth/me` | The account behind the current access token |
| `POST /api/v1/workouts` | Starts a workout owned by the caller (needed to have something to log into) |
| `POST /api/v1/workouts/{workoutId}/sets` | **The core write**: idempotent, user-scoped, domain-validated |
| `GET /api/v1/workouts/{workoutId}/sets` | The caller's sets of the caller's workout |

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
   cross-user write impossible even if the application check were skipped.

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
production, per-IP and per-account auth throttling with growing backoff, and identical answers
for an unknown address and a wrong password.

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
| `ATHLETICA_TRUST_PROXY_HEADERS` | `false` | Honour `X-Forwarded-For` — only turn on behind a proxy you control |
| `ATHLETICA_SHUTDOWN_TIMEOUT` | `15s` | Grace period for in-flight requests |

**Production refuses to start** when the signing secret is empty, is a known placeholder such as
`dev-only-change-me`, or is shorter than 32 characters. The message names the variable and how to
generate a real value. Staging is held to the same standard.

## Layout

```
services/api
├── api/openapi.yaml            contract (source of truth)
├── cmd/api                     main: serve | migrate | healthcheck | version
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
- Logout / token revocation endpoints, and a background sweep of expired refresh tokens.
- Listing and completing workouts (`GET /workouts`, status transitions); Sprint 1 only needs to
  create one and log into it.
- Password reset, e-mail verification, account deletion.
- Rate-limit state is per process, which is right for one container and needs Redis or an
  equivalent once the API is replicated.
- Metrics and tracing: requests are logged structurally, but nothing is exported yet.
- The PostgreSQL conformance suite is opt-in through an environment variable; wiring a disposable
  database into CI is the remaining half of audit finding H3.
