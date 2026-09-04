# AI Fitness OS — QA Audit Gate

Дата: 2026-08-14
Проверяемая сборка: Sprint 2D / OpenAPI 0.9.0
Объект аудита: `/mnt/data/ai-fitness-os-sprint2d.zip`

## Методика

Проверка выполнена четырьмя независимыми ролевыми потоками:

1. UX/UI Review — визуальная и интерфейсная структура по React Native исходникам.
2. Product Logic Review — пользовательские сценарии, состояния, переходы и edge cases.
3. Controls & Navigation Review — кнопки, Back behavior, busy/disabled/destructive actions, accessibility.
4. Functional QA Review — backend tests, API/mobile contracts, persistence/offline/auth, build readiness.

Финальный Lead Review свёл дубли и назначил приоритеты.

## Что реально прогнано

- `./scripts/verify.sh` — PASS
- `go test ./...` — PASS
- `go vet ./...` — PASS
- OpenAPI YAML parse — PASS
- Docker Compose YAML parse — PASS
- 35 TS/TSX файлов — syntax PASS
- `go test -cover ./...` — выполнен для оценки пробелов покрытия
- static scan 101 `Pressable`
- static scan accessibility/test IDs
- audit auth TTL + refresh flows
- audit offline queue + logout
- audit API URL/runtime config
- audit native mobile scaffold
- audit active workout lifecycle

---

# Executive verdict

**Вердикт: Phase 3 пока не начинать. Сначала Quality Sprint 2Q.**

Функциональная основа сильнее, чем UI-shell, но найдено несколько блокеров, которые помешают нормальному тестированию на реальном телефоне и длинной тренировке.

Оценка текущего состояния:

- Backend/domain logic: **7.5/10**
- Workout logic: **7/10**
- Nutrition/Programs logic: **7/10**
- Mobile UX: **5.5/10**
- Buttons/navigation: **5/10**
- Accessibility/testability: **2/10**
- Device build readiness: **3/10**
- Automated QA maturity: **4.5/10**

---

# BLOCKER / CRITICAL

## QA-001 — Access token истекает через 15 минут, но обычные экраны не refresh-ят его

**Severity: CRITICAL**

Backend создаёт access token на 15 минут:

- `services/api/cmd/api/main.go:26`

Refresh на mobile существует только в bootstrap/onboarding/offline sync:

- `apps/mobile/App.tsx:122-136`
- `apps/mobile/App.tsx:214-218`
- `apps/mobile/src/storage/sync.ts:28-34`

Обычные вызовы `api.logSet`, Nutrition, Programs, AI Coach и т.д. не имеют общего 401 interceptor/retry.

### Реальный сценарий

1. Пользователь входит.
2. Начинает 60–90 минутную тренировку.
3. Через ~15 минут access token протухает.
4. Следующий подход получает 401.
5. UI показывает ошибку вместо прозрачного refresh + retry.

### Fix

Создать единый `AuthenticatedApiClient`:

`request -> 401 -> single-flight refresh -> save tokens -> retry original request once`.

Refresh должен обновлять токены глобально, а не только внутри App bootstrap.

---

## QA-002 — Mobile API URL жёстко привязан к Android Emulator

**Severity: CRITICAL for device build**

`apps/mobile/src/api/client.ts:359`

```ts
const API_BASE_URL = 'http://10.0.2.2:8080/api/v1';
```

`10.0.2.2` — специальный маршрут Android emulator. Эта конфигурация не является production/device config.

### Fix

Environment configuration:

- local Android emulator
- local physical device/LAN
- staging HTTPS
- production HTTPS

Например `API_BASE_URL` через generated environment/config module.

В production запретить cleartext HTTP.

---

## QA-003 — Нет полноценного Android/iOS native scaffold

**Severity: CRITICAL for device testing**

В `apps/mobile` отсутствуют стандартные native project roots:

- нет `apps/mobile/android/`
- нет `apps/mobile/ios/`
- нет `gradlew`
- нет `settings.gradle`
- нет `Podfile`

Есть только отдельные Kotlin fragments в `apps/mobile/native/android`.

Следствие: `react-native run-android` из текущего архива нельзя считать воспроизводимой device build pipeline.

### Fix

Создать штатный RN 0.87 native scaffold и интегрировать:

- RestTimerNotificationsPackage
- FoodPhotoPickerPackage
- permissions
- network security config
- signing flavors
- debug/staging/prod environments

Добавить lockfile.

---

## QA-004 — Активную тренировку невозможно отменить

**Severity: CRITICAL UX / HIGH data integrity**

`App.tsx:116` блокирует Android Back на `active`:

```ts
if (step === 'active') return true;
```

В API нет `cancel workout` endpoint.

После перезапуска `activeWorkout()` снова открывает эту тренировку.

### Fix

Добавить:

- `POST /workouts/{id}/cancel`
- статус `cancelled`
- confirmation sheet: «Продолжить / Завершить / Отменить»
- сохранение причины optional
- очистку rest timer + local snapshot после cancel

Нельзя заставлять пользователя завершать ошибочно запущенную тренировку как completed.

---

## QA-005 — Offline queue не очищается/не scope-ится по user при logout

**Severity: CRITICAL privacy/data-integrity**

Offline queue хранится глобально:

`fitness.offline-queue.v1`

Logout очищает tokens, active workout и AI chat, но **не offline queue**:

- `apps/mobile/App.tsx:277-288`
- `apps/mobile/src/storage/session.ts:7`

### Риск

Пользователь A оставляет offline set operations -> logout -> пользователь B входит на том же телефоне -> bootstrap вызывает `flushOfflineQueue()` с токенами B.

Backend, вероятно, отклонит чужой workout ID, но очередь загрязняет новую сессию и создаёт риск data-crossing/UX lock.

### Fix

Лучший вариант: scope storage keys по immutable user ID.

Минимум перед релизом:

- `clearQueue()` на logout;
- clear active workout;
- clear per-user caches;
- тест A -> queue -> logout -> B login -> queue empty.

---

# HIGH

## QA-006 — 101 Pressable, почти нет accessibility metadata

**Severity: HIGH**

Static scan:

- `Pressable`: **101**
- `accessibilityLabel/accessibilityRole`: фактически только BodyMap hotspots
- `testID`: **0**
- `hitSlop`: **0**

### Consequences

- плохая TalkBack/VoiceOver навигация;
- сложно писать Detox/Maestro/Appium E2E;
- маленькие text-only действия имеют слабую touch ergonomics.

### Fix

Создать primitives:

- `AppButton`
- `IconButton`
- `TextButton`
- `SegmentedControl`

С обязательными:

- `accessibilityRole`
- `accessibilityLabel`
- `accessibilityState`
- `testID`
- min touch target 44–48dp
- disabled visual state

---

## QA-007 — Главный экран не соответствует целевой mobile navigation model

**Severity: HIGH UX**

Сейчас `HomeScreen` содержит сверху горизонтальный набор:

`Питание / Прогресс / История / Выйти`

При этом ранее продуктовая структура подразумевает основные разделы приложения.

### Problem

На узких Android-экранах верхняя строка перегружена; важные разделы конкурируют с logout.

### Fix

Перейти на bottom navigation:

- Сегодня
- Тренировка
- Питание
- Прогресс
- AI

Профиль/logout — через avatar/settings.

Programs — внутри Training/Home, а не второй крупной hero-карточкой рядом с AI.

---

## QA-008 — Design System формально существует, но почти не используется

**Severity: HIGH maintainability / MEDIUM visual**

Есть:

`apps/mobile/src/theme/tokens.ts`

Но static scan не нашёл его использования экранами.

Вместо этого:

- 28 отдельных `StyleSheet.create`
- 48 прямых `#111`
- множество локальных radii/paddings/font sizes

### Fix

Вынести semantic tokens:

- colors/background/surface/text/muted/border/danger/success
- spacing
- radius
- typography
- control heights
- dark/light theme

Затем заменить screen-local magic values.

---

## QA-009 — Destructive actions без confirm/undo

**Severity: HIGH UX**

Примеры:

- удалить food entry — `×`
- archive program
- finish workout

### Fix

- food delete: optimistic delete + Snackbar Undo
- archive: confirm bottom sheet
- workout finish: если план не выполнен — confirmation с прогрессом

---

## QA-010 — Finish workout допускает слишком лёгкое случайное завершение

**Severity: HIGH**

`ActiveWorkoutScreen` показывает text action `Завершить тренировку` без busy/confirm.

Backend `CompleteWorkout` не требует хотя бы одного рабочего подхода и не различает incomplete completion.

### Fix

Если `done < target`:

> Выполнено 7 из 18 подходов. Завершить тренировку досрочно?

В analytics хранить `completion_percent` / `ended_early`.

---

## QA-011 — Неполная client-side validation подхода

**Severity: HIGH/MEDIUM**

Mobile допускает:

- `reps = 0`
- RIR не проверяется на finite / 0..10
- отрицательный weight проверяется только backend

Backend также допускает `repetitions == 0` (`< 0`, а не `<= 0`).

### Fix

- reps: `1..200`
- weight: `0..1000`
- RIR: `0..10`
- RPE: `1..10`
- clear validation copy next to field
- domain validation test cases

---

## QA-012 — Нет mobile component/E2E tests

**Severity: HIGH release confidence**

Текущая mobile проверка — syntax parse.

`testID = 0` делает UI automation практически неготовой.

### Fix

Minimum quality gate:

1. Component tests основных форм.
2. E2E:
   - register/onboarding
   - generate/start/log/finish workout
   - token expires mid-workout
   - offline 3 sets -> reconnect
   - nutrition add/remove/undo
   - program create/start session
3. Native notification smoke test.

---

## QA-013 — PostgreSQL implementation не покрыта автоматическими тестами

**Severity: HIGH backend confidence**

`go test -cover ./...` показал `internal/store` coverage = 0%.

Большая часть интеграционных тестов проходит на MemoryStore.

### Fix

Testcontainers / Docker integration suite для PostgreSQL:

- migrations
- auth rotation
- workout lifecycle
- duplicate set upsert
- custom-food privacy
- program completion
- offline replay idempotency

---

# MEDIUM

## QA-014 — Busy state не унифицирован

Некоторые async buttons disabled, некоторые допускают повторный tap:

- Recipes log/save/search
- Progress save
- Nutrition remove/repeat
- Program archive/autoMove/explain
- Finish workout

Нужен shared async-button primitive / pending action registry.

---

## QA-015 — Disabled controls визуально выглядят активными

В Active Workout `Предыдущее/Следующее` используют `disabled`, но style не меняется.

Нужны `opacity`, `accessibilityState={{disabled:true}}` и inactive text color.

---

## QA-016 — BodyMap является хорошим прототипом навигации, но пока не final visual

BodyMap нарисован базовыми прямоугольниками/овалами, а мышца выбирается текстовым floating hotspot.

Для final design лучше:

- SVG/vector front/back silhouette
- actual muscle regions
- selected/loaded/recovery states
- larger invisible touch region
- labels отдельно от anatomical region

Текущий вариант подходит для functional MVP, но не для premium Fitness OS positioning.

---

## QA-017 — Safe area/theme shell не завершён

В dependency есть `react-native-safe-area-context`, но root использует `SafeAreaView` из `react-native`.

Нет общей theme provider и dark mode, хотя UI целится в системное приложение.

---

## QA-018 — Nutrition quick actions слишком тесные

`AI добавить` + `Поиск` + `Рецепты` находятся в одной горизонтальной строке.

На маленькой ширине/увеличенном системном шрифте это риск squeeze/overflow.

Лучше primary AI button + secondary 2-column row или bottom sheet «Добавить еду».

---

## QA-019 — Recipes/Progress имеют слабую форму validation UX

Recipes может отправить пустое имя/нулевые граммы и положиться на backend error.

Progress может попытаться сохранить пустой measurement.

Нужны disabled states и field-level validation.

---

## QA-020 — Manual state router становится хрупким

`App.tsx` вручную управляет 25+ значениями `Step` и BackHandler mapping.

На следующей фазе Body Scan/Technique/Recovery количество переходов резко возрастёт.

Перед Phase 3 рекомендуется перейти на typed navigator/router или хотя бы выделить navigation state machine отдельно от App.

---

# POSITIVE FINDINGS

## Architecture

- LLM не является источником бизнес-расчётов.
- Workout/Nutrition/Program engines отделены от AI.
- OpenAI provider имеет local deterministic fallback.
- Food AI использует confirmation step.
- Custom food privacy уже покрыта логикой.
- Offline workout sets имеют очередь и upsert semantics.
- Refresh token хранится не в AsyncStorage, а в Keychain.
- Program Engine имеет отдельные tests и хорошее покрытие относительно остальных модулей.

## Verification

На момент аудита:

- Go tests — PASS
- Go vet — PASS
- YAML — PASS
- TS/TSX syntax — PASS
- Program tests — PASS

То есть найденные проблемы — в основном **product/runtime/device quality**, а не «проект вообще не компилирует backend».

---

# Recommended Quality Sprint 2Q

Порядок исправления:

### Q1 — Runtime blockers

1. Central auth refresh/retry.
2. Environment-based API URL.
3. User-scoped/cleared offline queue.
4. Cancel workout lifecycle.
5. Finish confirmation + validation.

### Q2 — Device readiness

6. Generate real RN Android/iOS scaffold.
7. Integrate native packages.
8. Android emulator + physical device debug build.
9. Add staging HTTPS config.

### Q3 — UX controls

10. Bottom navigation.
11. Shared button primitives.
12. accessibilityLabel/role/state + testID.
13. destructive confirms/undo.
14. busy/disabled states.

### Q4 — Automated QA

15. Mobile E2E framework.
16. Token-expiry E2E.
17. Offline replay E2E.
18. PostgreSQL integration suite.

### Q5 — Design cleanup

19. Semantic design tokens.
20. Real SVG body map.
21. responsive typography/layout.
22. light/dark shell.

---

# Exit criteria before Phase 3 AI Fitness

Phase 3 можно начинать после того, как:

- 60+ minute workout survives access-token expiration;
- user can cancel active workout;
- logout cannot replay another user's offline queue;
- app runs on Android emulator and at least one physical Android device;
- API URL is environment based;
- critical buttons have test IDs + accessibility labels;
- core E2E passes;
- PostgreSQL core lifecycle integration tests pass.

Только после этого имеет смысл добавлять Body Scan / Pose Estimation / Technique AI, иначе Computer Vision будет строиться поверх нестабильного mobile shell.
