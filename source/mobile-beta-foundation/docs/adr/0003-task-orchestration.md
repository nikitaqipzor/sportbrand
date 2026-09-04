# ADR 0003 — Task orchestration: pnpm recursive, not Turborepo

## Status

Accepted (Sprint 1).

## Context

The root manifest declared `turbo` as a devDependency and shipped a `turbo.json`,
but every root script actually ran `pnpm --recursive`. Turborepo was dead weight:
declared, downloaded, never executed. The declared pipeline was also wrong —
`test` depended on `^test`, which would have forced upstream packages to re-run
their tests before a downstream package could run its own.

The workspace is three packages (`@athletica/domain`, `@athletica/api-client`,
`@athletica/mobile`). None of them has a build step: TypeScript is consumed as
source by Metro and executed directly by Node's type stripping in tests. There
are therefore no build artifacts to cache, which is where Turborepo pays off.

## Decision

Remove `turbo` and `turbo.json`. Root `typecheck` and `test` keep using
`pnpm --recursive run`, which already walks the workspace in topological order.

## Consequences

- One less toolchain and one less platform binary per CI runner.
- No remote/local task cache. Acceptable while the full `typecheck` + `test`
  sweep takes seconds.
- Reintroduce Turborepo (or Nx) when a real pipeline appears — bundling,
  lint, codegen from the OpenAPI contract, or Android artifacts — i.e. when
  there is something worth caching. That reversal is a two-file change.
