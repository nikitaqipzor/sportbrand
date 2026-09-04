import { existsSync, readFileSync } from "node:fs";

const required = [
  "apps/mobile/app/_layout.tsx", "apps/mobile/app/(app)/index.tsx", "apps/mobile/app/(app)/workout/[workoutId].tsx",
  "packages/domain/src/workout.ts", "packages/api-client/src/config.ts", "services/api/api/openapi.yaml",
  "infra/compose/docker-compose.yml", "docs/adr/0001-monorepo-and-mobile-stack.md", "docs/adr/0002-offline-write-model.md"
];
for (const file of required) if (!existsSync(file)) throw new Error(`Missing ${file}`);
const contract = readFileSync("services/api/api/openapi.yaml", "utf8");
if (!contract.includes("clientMutationId")) throw new Error("OpenAPI must require clientMutationId");
if (!contract.includes("/workouts/{workoutId}/sets")) throw new Error("OpenAPI must expose set logging");
console.log(`Foundation verified: ${required.length} required files and idempotent workout contract.`);
