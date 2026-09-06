import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const path = (...parts) => join(root, ...parts);
const read = (...parts) => readFileSync(path(...parts), "utf8");
const readJson = (...parts) => JSON.parse(read(...parts));

const failures = [];
const check = (condition, message) => {
  if (!condition) failures.push(message);
};

// 1. Обязательные файлы каркаса.
const requiredFiles = [
  ".nvmrc",
  ".env.example",
  "package.json",
  "pnpm-workspace.yaml",
  "tsconfig.base.json",
  "apps/mobile/package.json",
  "apps/mobile/app.json",
  "apps/mobile/app.config.ts",
  "apps/mobile/babel.config.js",
  "apps/mobile/metro.config.js",
  "apps/mobile/tsconfig.json",
  "apps/mobile/app/_layout.tsx",
  "apps/mobile/app/(app)/index.tsx",
  "apps/mobile/app/(app)/workout/[workoutId].tsx",
  "apps/mobile/src/config/env.ts",
  "apps/mobile/src/features/workout/log-set.ts",
  "apps/mobile/src/platform/offline/outbox.ts",
  "packages/domain/package.json",
  "packages/domain/tsconfig.json",
  "packages/domain/src/workout.ts",
  "packages/api-client/package.json",
  "packages/api-client/tsconfig.json",
  "packages/api-client/src/config.ts",
  "services/api/api/openapi.yaml",
  "infra/compose/docker-compose.yml",
  "docs/test-plan.md",
  "docs/adr/0001-monorepo-and-mobile-stack.md",
  "docs/adr/0002-offline-write-model.md"
];
for (const file of requiredFiles) check(existsSync(path(file)), `Missing ${file}`);

// 2. Все три воркспейс-пакета — полноценные пакеты с typecheck и test.
const workspacePackages = [
  ["packages/domain", "@athletica/domain"],
  ["packages/api-client", "@athletica/api-client"],
  ["apps/mobile", "@athletica/mobile"]
];
for (const [dir, name] of workspacePackages) {
  if (!existsSync(path(dir, "package.json"))) {
    check(false, `${dir} is not a package: package.json is missing`);
    continue;
  }
  const manifest = readJson(dir, "package.json");
  check(manifest.name === name, `${dir}/package.json must be named ${name}`);
  check(manifest.private === true, `${dir}/package.json must be private`);
  check(Boolean(manifest.scripts?.typecheck), `${dir} must declare a typecheck script`);
  check(Boolean(manifest.scripts?.test), `${dir} must declare a test script`);
  check(existsSync(path(dir, "tsconfig.json")), `${dir}/tsconfig.json is missing`);

  const testsDir = path(dir, "tests");
  const testFiles = existsSync(testsDir)
    ? readdirSync(testsDir).filter((file) => file.endsWith(".test.mjs"))
    : [];
  check(testFiles.length > 0, `${dir}/tests must contain at least one *.test.mjs file`);
}

// 3. Мобильное приложение реально подключает воркспейс-пакеты.
const mobile = readJson("apps/mobile", "package.json");
for (const dependency of ["@athletica/domain", "@athletica/api-client"]) {
  check(
    String(mobile.dependencies?.[dependency] ?? "").startsWith("workspace:"),
    `apps/mobile must depend on ${dependency} via workspace:*`
  );
}
const mobileSources = ["apps/mobile/src/features/workout/log-set.ts", "apps/mobile/src/config/env.ts"]
  .filter((file) => existsSync(path(file)))
  .map((file) => read(file))
  .join("\n");
check(mobileSources.includes("@athletica/domain"), "apps/mobile must import @athletica/domain in source");
check(mobileSources.includes("@athletica/api-client"), "apps/mobile must import @athletica/api-client in source");

// 4. Обвязка Expo Router и проброс окружения.
if (existsSync(path("apps/mobile/app.json"))) {
  const appJson = readJson("apps/mobile", "app.json");
  check(
    Array.isArray(appJson.expo?.plugins) && appJson.expo.plugins.includes("expo-router"),
    "apps/mobile/app.json must enable the expo-router plugin"
  );
  check(Boolean(appJson.expo?.scheme), "apps/mobile/app.json must define a scheme for expo-router");
}
if (existsSync(path("apps/mobile/app.config.ts"))) {
  const appConfig = read("apps/mobile/app.config.ts");
  for (const variable of ["ATHLETICA_API_URL", "ATHLETICA_ENV"]) {
    check(appConfig.includes(variable), `apps/mobile/app.config.ts must forward ${variable}`);
  }
}
for (const variable of ["ATHLETICA_API_URL", "ATHLETICA_ENV"]) {
  check(read(".env.example").includes(variable), `.env.example must document ${variable}`);
}

// 5. Воспроизводимость окружения.
const rootManifest = readJson("package.json");
check(Boolean(rootManifest.engines?.node), "root package.json must declare engines.node");
check(existsSync(path("pnpm-lock.yaml")), "pnpm-lock.yaml must be committed");
check(
  !rootManifest.devDependencies?.turbo === !existsSync(path("turbo.json")),
  "turbo dependency and turbo.json must be in sync (both present or both absent)"
);

// 6. Контракт API остаётся идемпотентным.
if (existsSync(path("services/api/api/openapi.yaml"))) {
  const contract = read("services/api/api/openapi.yaml");
  check(contract.includes("clientMutationId"), "OpenAPI must require clientMutationId");
  check(contract.includes("/workouts/{workoutId}/sets"), "OpenAPI must expose set logging");
}

// 7. Экран не показывает выдуманное число.
//
// «Готовность 78» и «сон снизил интенсивность на 5%» жили на экране «Сегодня»,
// хотя домена готовности не существует. В фитнес-приложении правдоподобная
// подделка опаснее пустого места: по такому числу человек планирует нагрузку.
// Проверка держит правило, пока домен не появится по-настоящему.
const fabricated = [
  { file: "apps/mobile/app/(app)/index.tsx", pattern: /<Text style={styles.score}>\s*\d/, what: "готовность подставным числом" },
  { file: "apps/mobile/app/(app)/index.tsx", pattern: /Сон снизил/, what: "выдуманное объяснение готовности" }
];
for (const { file, pattern, what } of fabricated) {
  if (!existsSync(path(file))) continue;
  check(!pattern.test(read(file)), `${file} must not render ${what}`);
}

if (failures.length > 0) {
  for (const failure of failures) console.error(`FAIL ${failure}`);
  throw new Error(`Foundation verification failed: ${failures.length} problem(s).`);
}

console.log(
  `Foundation verified: ${requiredFiles.length} required files, ${workspacePackages.length} workspace packages with tests, expo-router wiring, env plumbing and idempotent workout contract.`
);
