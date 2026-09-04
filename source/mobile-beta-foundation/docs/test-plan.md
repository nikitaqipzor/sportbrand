# Sprint 1 test plan

| # | Case | Automated in |
| --- | --- | --- |
| 1 | API config rejects cleartext production URLs, falls back in dev, trims trailing slash, fails on empty baseUrl for staging/production. | `packages/api-client/tests/config.test.mjs` |
| 2 | Set validation rejects invalid weight, reps, RIR, set number and empty identifiers; accepts valid input. | `packages/domain/tests/workout.test.mjs` |
| 3 | Outbox returns only current-user items, logout purges only that user's items, enqueue is idempotent per `clientMutationId`. | `apps/mobile/tests/outbox.test.mjs` |
| 4 | Contract contains idempotent set-write endpoint. | `scripts/verify-foundation.mjs` |
| 5 | Android smoke: Today opens active workout and the primary action is reachable. | Manual — needs Android SDK (pending) |

Run everything with:

```bash
pnpm test && pnpm verify:foundation
```
