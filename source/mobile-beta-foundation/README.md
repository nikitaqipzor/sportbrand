# Athletica AI — Android beta foundation

Android-first monorepo for the first beta loop:

`Today → active strength workout → log set → offline sync → summary → progress`

## Status

Foundation only. The published web prototype is not replaced by this folder.
Real authentication, database migrations and production deployment are deliberately pending recovery of the original React Native + Go repository.

## Requirements

- Node >= 22 (see `.nvmrc`) — tests run TypeScript directly via Node's type stripping, no build step.
- pnpm 10 (`corepack enable`).
- Android SDK + JDK 17 only for `expo run:android`.

## Workspace

| Package | Path | Purpose |
| --- | --- | --- |
| `@athletica/mobile` | `apps/mobile` | Expo Router app (Android-first) |
| `@athletica/domain` | `packages/domain` | Workout rules, set validation |
| `@athletica/api-client` | `packages/api-client` | API environment/config resolution |

## Local checks

```bash
pnpm install
pnpm typecheck            # tsc --noEmit in all three packages
pnpm test                 # node --test in all three packages
pnpm verify:foundation    # structural checks of the foundation
```

Android debug build (requires a local Android SDK):

```bash
cp .env.example .env
pnpm --filter @athletica/mobile android
```

## Configuration

`ATHLETICA_API_URL` and `ATHLETICA_ENV` flow `.env` → `apps/mobile/app.config.ts` →
manifest `extra` → `expo-constants` → `resolveApiConfig()` in `@athletica/api-client`.
Verify the resolved manifest with:

```bash
pnpm --filter @athletica/mobile exec expo config --type public --json
```

## Sprint 1 exit criteria

- Android debug build starts.
- Workout writes are idempotent and isolated by user.
- Logout purges the previous user's local session and outbox.
- API contract, typecheck, unit tests and Android smoke run in CI.
