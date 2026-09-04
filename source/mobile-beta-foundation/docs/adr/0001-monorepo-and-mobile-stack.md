# ADR 0001: Android-first monorepo

We use pnpm workspaces and Turborepo. The Android client is Expo/React Native; the API remains Go + PostgreSQL once the original service is recovered. OpenAPI is the source of truth at the boundary. iOS, payments, camera and autonomous AI are out of scope for closed beta.
