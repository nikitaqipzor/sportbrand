# Athletica AI — Android beta foundation

Android-first monorepo for the first beta loop:

`Today → active strength workout → log set → offline sync → summary → progress`

## Status

Foundation only. The published web prototype is not replaced by this folder.
Real authentication, database migrations and production deployment are deliberately pending recovery of the original React Native + Go repository.

## Local checks available now

```bash
npm run verify:foundation
```

After dependencies are installed in a full mobile environment:

```bash
pnpm install
pnpm typecheck
pnpm --filter @athletica/mobile android
```

## Sprint 1 exit criteria

- Android debug build starts.
- Workout writes are idempotent and isolated by user.
- Logout purges the previous user's local session and outbox.
- API contract, typecheck, unit tests and Android smoke run in CI.
