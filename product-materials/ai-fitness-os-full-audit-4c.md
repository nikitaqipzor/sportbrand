# AI Fitness OS — Full Studio Audit (Sprint 4C in progress)

Дата аудита: 2026-09-02

## Executive verdict

Текущая кодовая база функционально сильная и проходит основной regression gate, но **Sprint 4C пока не должен считаться production-ready**. Главный риск находится не в Workout/Recovery формулах, а в границах аккаунта для нового passive Health Sync и в отсутствии реального device/build/PostgreSQL CI-gate.

Статус главного инженера: **CONDITIONAL / FIX BEFORE RELEASE**.

## Что реально прогнано

- `./scripts/verify.sh` — PASS.
- `go test ./...` — PASS.
- `go vet ./...` — PASS.
- `go test -race` для healthdata/recovery/workouts/programs/technique/httpapi — PASS.
- 20 последовательных прогонов healthdata/recovery/workouts/programs/technique — PASS.
- OpenAPI/Docker YAML — PASS.
- 58 TS/TSX файлов проходят syntax-transpile — PASS.
- Android native verifier — 118 checks PASS.
- Mobile auth refresh regression — PASS.
- Mobile session scope regression — PASS.
- Mobile technique mapping — PASS.
- Mobile passive health sync regression — PASS.
- 18 up/down migrations присутствуют попарно.
- Скан репозитория не обнаружил реального API key/private key.

## Покрытие backend

Общее statement coverage: **47.5%**.

Сильные зоны:
- healthdata: 83.2%
- bodyscan: 79.8%
- technique: 78.1%
- recovery: 71.2%
- programs: 68.4%
- httpapi: 59.2%

Слабые зоны:
- workouts package: 16.7% (engine покрыт лучше, service слой фактически прикрыт HTTP integration tests, но package coverage низкий)
- nutrition: 5.5%
- auth: 28.3%
- AI service/provider: 28.0%
- PostgreSQL store: 0% автоматического покрытия
- profile/progress/media/catalog: 0% package-level coverage

## Найденные проблемы

### BLOCKER / HIGH

#### H1. Passive Health Sync не привязан к app-user

`HealthPassiveSyncStore` хранит encrypted snapshots на уровне Android-приложения и ключует их только по дате. `flushPassiveHealthSnapshots(accessToken)` затем загружает все pending snapshots в аккаунт, который в этот момент авторизован.

Сценарий риска:
1. пользователь A включает passive sync;
2. WorkManager складывает health data в encrypted cache;
3. A выходит до upload;
4. входит пользователь B;
5. pending Xiaomi snapshot может быть отправлен под access token пользователя B.

Также logout сейчас не выключает WorkManager и не очищает native health cache.

Решение до release:
- bind passive sync к `user_id`;
- хранить owner id внутри native encrypted envelope;
- flush только для совпадающего owner;
- на logout отменять periodic work и очищать/отвязывать pending health cache;
- добавить cross-account native regression test.

#### H2. Android build всё ещё не доказан end-to-end

Static native verifier сильный, но в текущем окружении отсутствуют Android SDK и установленные npm dependencies. `npm install`, Gradle assemble/install и запуск на физическом устройстве здесь не пройдены.

Дополнительно отсутствует `package-lock.json`/другой lockfile, поэтому JavaScript/native dependency graph не воспроизводим побайтно.

Release gate:
- lockfile;
- clean install;
- `tsc --noEmit`;
- `assembleDebug`;
- install on Android;
- smoke на Xiaomi/Health Connect/CameraX.

#### H3. PostgreSQL adapter не проверяется реальной БД в CI

Все store SQL adapters имеют 0% coverage. Большая часть integration tests проходит через MemoryStore. Это значит, что schema/query/scan regressions могут пережить unit gate.

Нужен disposable PostgreSQL test container/CI job:
- migrations up;
- critical auth/workout/nutrition/recovery/health flows;
- unique constraints;
- cross-user isolation;
- migrations down/up smoke.

#### H4. Нет rate limiting для auth endpoints

`register/login/refresh` доступны без rate limiter/brute-force protection. Для локального MVP допустимо, для внешнего API — нет.

Нужно:
- IP + account-based login throttling;
- cooldown/backoff;
- metrics;
- одинаковый ответ для неверного email/password.

#### H5. Production может стартовать с development JWT secret

`AUTH_TOKEN_SECRET=dev-only-change-me` сейчас выдаёт warning, а не hard fail. В production это должно приводить к отказу запуска.

### HIGH / MEDIUM — продукт и архитектура mobile

#### U1. Самописный `Step` router вырос до 28 экранов

`App.tsx` вручную управляет back stack через union `Step` и `BackHandler`. Сейчас работает, но плохо масштабируется для deep links, notification navigation, restore state, modal routes и вложенных flows.

Рекомендация: React Navigation/native stack + 5 root tabs + nested stacks.

#### U2. Accessibility и testability покрыты частично

На уровне screens:
- 28 экранов;
- 101 `Pressable`;
- только 41 `testID`;
- 48 accessibility attrs (role/label суммарно);
- много экранов имеют нулевой testID/accessibility coverage: AI Coach, Auth, Food Search, Goal, History, Recipes, Program Setup и др.

По всему mobile src:
- 252 упоминания Pressable;
- 43 testID;
- 24 accessibilityLabel;
- 7 hitSlop.

До полноценного Maestro/Detox E2E ключевые действия должны иметь стабильные selectors.

#### U3. UI visual system пока functional prototype

Плюсы: tokens и `AppButton` уже появились.

Но:
- 35 отдельных `StyleSheet.create`;
- заметное число hardcoded hex вне theme;
- разные border radii/цвета/типографика;
- glyph-символы `● ◆ ◐ ↗ ✦` вместо иконографической системы;
- часть экранов написана сверхплотным single-line JSX, что усложняет ревью и дизайн-рефакторинг;
- Home содержит много равнозначных карточек и конкурирующих CTA.

Вывод дизайнера: **структуру функций сохранять, визуальный язык можно переработать глубоко**.

#### U4. Profile entry пока placeholder

Home profile вызывает Alert: «Настройки аккаунта будут расширены». Нужен реальный Settings/Profile screen: профиль, цели, equipment, health permissions, privacy/data deletion, logout.

#### U5. AI Coach UX ниже уровня остальных систем

- send button не имеет accessibility metadata;
- нет retry action на failed message;
- нет stop/cancel generation;
- нет explicit tool/source chips;
- нет автоскролла/streaming UX;
- visual hierarchy минимальная.

### MEDIUM — логика/механики

#### M1. Nutrition service почти без unit coverage

5.5% package coverage. Критические сценарии стоит закрепить таблицами тестов: targets, quantity bounds, custom foods, recipe scaling, repeat, correlation.

#### M2. Recipe logging не транзакционен на уровне всей порции

Рецепт логирует ингредиенты отдельными entries. При частичной DB-ошибке возможен частично записанный приём пищи. Нужна Store batch/transaction API.

#### M3. Offline-first покрывает тренировку, но не остальные главные действия

Workout set/finish/cancel имеют offline queue. Nutrition entry, recovery check-in, body measurement и health import — нет. Это не баг MVP, но поведение offline сейчас непоследовательно.

#### M4. Нет mobile crash/observability слоя

В source нет Sentry/Crashlytics/mobile telemetry. Для CameraX/Health Connect/Codegen/native bridges это станет важным на beta.

#### M5. iOS script существует, iOS проекта нет

`npm run ios` объявлен, но `apps/mobile/ios` отсутствует. Для Android-first это не blocker, но команда должна либо убрать misleading script, либо завести отдельный iOS milestone.

## Сильные стороны продукта

- LLM не является source of truth.
- Workout/Nutrition/Program/Recovery/Technique/Health engines разделены.
- Health provenance и freshness уже встроены в модель.
- Xiaomi предпочтителен явно, данные разных health sources больше не должны логически суммироваться без resolver.
- Body Scan media защищены auth-доступом.
- Technique CV privacy-first: landmarks вместо обязательной загрузки live video.
- Workout offline queue привязана к user.
- Access-token refresh single-flight протестирован.
- Recovery реально изменяет generated workout, а не только UI score.

## Аудит фич / R&D

### Что сейчас является ядром продукта
1. Today / Readiness.
2. Adaptive Workout.
3. Nutrition.
4. Progress: body + strength + technique.
5. AI Coach как интерфейс к фактам.
6. Wearable context.

### Что НЕ стоит добавлять прямо сейчас
- социальную ленту;
- challenges/кланы;
- большое количество новых AI agents внутри пользовательского UI;
- marketplace тренеров;
- десятки wearable direct APIs.

Причина: приложение уже feature-rich. Главный риск сейчас — потерять ясный daily loop.

### Следующие продуктовые идеи после QA/design stabilization
- Morning Brief: один экран «что делать сегодня и почему»;
- Workout Adaptation Diff: что конкретно изменилось против плана;
- Recovery timeline: сон/нагрузка/soreness → readiness → workout effect;
- confidence badge для CV technique;
- personal weekly experiment: одна гипотеза в неделю (сон, белок, объём) с измеримым outcome;
- privacy center с источниками и временем последней синхронизации.

## Рекомендуемый порядок исправлений

### Audit Fix Pack A — до passive sync release
1. User-bound native health cache + logout cleanup.
2. Auth rate limiting.
3. Production JWT secret hard fail.
4. Lockfile + semantic TypeScript typecheck.

### Audit Fix Pack B — before beta
5. PostgreSQL CI/E2E.
6. React Navigation migration.
7. testID/accessibility pass.
8. Mobile E2E smoke suite.
9. Crash/telemetry.

### Design Sprint
10. Зафиксировать visual direction.
11. Собрать design tokens v2.
12. Перерисовать Today, Active Workout, Nutrition, Recovery, Progress, Devices, AI.
13. После утверждения — только тогда рефакторить остальные 20+ экранов.

