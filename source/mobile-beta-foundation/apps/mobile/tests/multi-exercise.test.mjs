import test from "node:test";
import assert from "node:assert/strict";

import {
  createMemoryOutboxStore,
  createMemorySnapshotStore,
  createOutboxSync,
  createSnapshotMemoryDb
} from "../src/platform/offline/index.ts";
import { createWorkoutOffline } from "../src/features/workout/workout-offline.ts";
import {
  buildSetInput,
  currentExercise,
  mutationIdFor,
  nextSetNumber,
  reviveActiveWorkout,
  startActiveWorkout,
  totalCompletedSets,
  withExercise,
  withRecordedSet
} from "../src/features/workout/active-workout.ts";
import { EXERCISE_CATALOG, exerciseTitle } from "../src/features/workout/exercise-catalog.ts";

const ALICE = "user-alice";
const BENCH = "bench-press";
const SQUAT = "back-squat";

const at = (iso) => () => new Date(iso);

/** Сеть замокана: отправитель только запоминает, что у него просили. */
function spySender() {
  const calls = [];
  return {
    calls,
    send: async (workoutId, input) => {
      calls.push({ workoutId, mutationId: input.clientMutationId, exerciseId: input.exerciseId, setNumber: input.setNumber });
      return { ok: true, value: { outcome: "created", set: { id: `s-${calls.length}` } } };
    }
  };
}

/** Приложение поверх «диска»: снимок переживает пересборку объектов. */
function app(snapshotDb, { spy = spySender(), now = at("2026-09-05T10:00:00.000Z") } = {}) {
  const queue = createWorkoutOffline({
    sync: createOutboxSync({ store: createMemoryOutboxStore(), send: spy.send, now }),
    snapshots: createMemorySnapshotStore(snapshotDb),
    now
  });
  return { queue, spy };
}

const START = {
  workoutId: "w-1",
  title: "Силовая тренировка",
  exercises: [{ exerciseId: BENCH }, { exerciseId: SQUAT }]
};

test("нумерация подходов своя у каждого упражнения, а не общая на тренировку", async () => {
  const { queue, spy } = app(createSnapshotMemoryDb());
  let workout = await queue.start(ALICE, START);

  // Два подхода в жиме…
  workout = (await queue.recordSet(ALICE, workout, { weightKg: 80, repetitions: 8, rir: 2 })).workout;
  workout = (await queue.recordSet(ALICE, workout, { weightKg: 82.5, repetitions: 7, rir: 2 })).workout;
  // …и один в приседе: он обязан быть ПЕРВЫМ подходом приседа, а не третьим.
  workout = await queue.selectExercise(ALICE, workout, SQUAT);
  workout = (await queue.recordSet(ALICE, workout, { weightKg: 100, repetitions: 5, rir: 3 })).workout;

  await queue.flush(ALICE);

  assert.deepEqual(
    spy.calls.map((call) => `${call.exerciseId}#${call.setNumber}`),
    [`${BENCH}#1`, `${BENCH}#2`, `${SQUAT}#1`],
    "общий счётчик отправил бы присед третьим подходом"
  );
  assert.equal(nextSetNumber(workout, BENCH), 3);
  assert.equal(nextSetNumber(workout, SQUAT), 2);
  assert.equal(totalCompletedSets(workout), 3);
});

test("clientMutationId включает упражнение: одинаковые номера подходов не сталкиваются", async () => {
  const { queue, spy } = app(createSnapshotMemoryDb());
  let workout = await queue.start(ALICE, START);
  workout = (await queue.recordSet(ALICE, workout, { weightKg: 80, repetitions: 8, rir: 2 })).workout;
  workout = await queue.selectExercise(ALICE, workout, SQUAT);
  workout = (await queue.recordSet(ALICE, workout, { weightKg: 100, repetitions: 5, rir: 3 })).workout;

  const ids = (await queue.list(ALICE)).map((record) => record.id);

  assert.deepEqual(ids, [`w-1:${BENCH}:1`, `w-1:${SQUAT}:1`]);
  assert.equal(new Set(ids).size, 2, "иначе второй подход затёр бы первый как дубль");
  await queue.flush(ALICE);
  assert.equal(spy.calls.length, 2, "оба подхода обязаны доехать до сервера");
});

test("снимок с несколькими упражнениями переживает перезапуск целиком", async () => {
  const disk = createSnapshotMemoryDb();
  const first = app(disk);
  let workout = await first.queue.start(ALICE, START);
  workout = (await first.queue.recordSet(ALICE, workout, { weightKg: 80, repetitions: 8, rir: 2 })).workout;
  workout = (await first.queue.recordSet(ALICE, workout, { weightKg: 82.5, repetitions: 7, rir: 2 })).workout;
  workout = await first.queue.selectExercise(ALICE, workout, SQUAT);
  workout = (await first.queue.recordSet(ALICE, workout, { weightKg: 100, repetitions: 5, rir: 3 })).workout;

  // Процесс умер: новое приложение поверх того же «диска».
  const revived = await app(disk).queue.load(ALICE);

  assert.ok(revived, "снимок обязан пережить перезапуск");
  assert.deepEqual(
    revived.exercises.map((exercise) => [exercise.exerciseId, exercise.completedSets]),
    [
      [BENCH, 2],
      [SQUAT, 1]
    ],
    "после перезапуска нумерация каждого упражнения обязана продолжиться с того же места"
  );
  assert.equal(revived.currentExerciseId, SQUAT, "открытое упражнение — тоже часть снимка");
  assert.equal(currentExercise(revived).title, exerciseTitle(SQUAT));
});

test("идентификатор мутации детерминирован: после перезапуска он тот же", async () => {
  const disk = createSnapshotMemoryDb();
  const first = app(disk);
  let workout = await first.queue.start(ALICE, START);
  workout = (await first.queue.recordSet(ALICE, workout, { weightKg: 80, repetitions: 8, rir: 2 })).workout;

  const beforeRestart = buildSetInput(workout, { weightKg: 82.5, repetitions: 7, rir: 2 });
  const revived = await app(disk).queue.load(ALICE);
  const afterRestart = buildSetInput(revived, { weightKg: 82.5, repetitions: 7, rir: 2 });

  assert.equal(afterRestart.clientMutationId, beforeRestart.clientMutationId);
  assert.equal(afterRestart.clientMutationId, `w-1:${BENCH}:2`);
  assert.equal(
    mutationIdFor(revived, 1, SQUAT),
    `w-1:${SQUAT}:1`,
    "сервер обязан узнать повтор и ответить 409 вместо второй записи"
  );
});

test("возврат в ту же тренировку поднимает её упражнения, а не начинает пустую", async () => {
  const disk = createSnapshotMemoryDb();
  const first = app(disk);
  let workout = await first.queue.start(ALICE, START);
  workout = (await first.queue.recordSet(ALICE, workout, { weightKg: 80, repetitions: 8, rir: 2 })).workout;
  workout = await first.queue.addExercise(ALICE, workout, { exerciseId: "lat-pulldown" });

  // Экран открылся заново и снова просит стартовый список — снимок сильнее.
  const resumed = await app(disk).queue.start(ALICE, START);

  assert.deepEqual(resumed.exercises.map((exercise) => exercise.exerciseId), [BENCH, SQUAT, "lat-pulldown"]);
  assert.equal(nextSetNumber(resumed, BENCH), 2, "иначе первый подход был бы записан вторично");
});

test("незавершённая тренировка предлагается к возврату, завершённая — нет", async () => {
  const disk = createSnapshotMemoryDb();
  const first = app(disk);
  let workout = await first.queue.start(ALICE, START);
  workout = (await first.queue.recordSet(ALICE, workout, { weightKg: 80, repetitions: 8, rir: 2 })).workout;

  const afterCrash = await app(disk).queue.resumable(ALICE);
  assert.equal(afterCrash?.workoutId, "w-1", "закрытое посреди тренировки приложение обязано предложить вернуться");
  assert.equal(totalCompletedSets(afterCrash), 1);

  await first.queue.finish(ALICE, workout, "complete");
  assert.equal(await app(disk).queue.resumable(ALICE), null, "завершённой тренировке возвращаться некуда");
});

test("возвращать нечего, когда отправлять не под кем", async () => {
  const { queue } = app(createSnapshotMemoryDb());
  assert.equal(await queue.resumable(null), null);
});

test("снимок старого формата (одно упражнение) поднимается, а не теряется", () => {
  // Так снимок выглядел до перехода на несколько упражнений.
  const legacy = {
    workoutId: "w-1",
    title: "Тяга верхнего блока",
    exerciseId: "lat-pulldown",
    status: "active",
    startedAt: "2026-09-05T10:00:00.000Z",
    completedSets: 3,
    lastSetAt: "2026-09-05T10:12:00.000Z",
    lastSet: { weightKg: 60, repetitions: 10, rir: 2 }
  };

  const revived = reviveActiveWorkout(legacy);

  assert.equal(revived.exercises.length, 1);
  assert.equal(revived.currentExerciseId, "lat-pulldown");
  assert.equal(nextSetNumber(revived, "lat-pulldown"), 4, "иначе повтор нумерации создал бы дубли подходов");
  assert.equal(mutationIdFor(revived, 4), "w-1:lat-pulldown:4");
});

test("битый снимок не притворяется тренировкой", () => {
  assert.equal(reviveActiveWorkout(null), null);
  assert.equal(reviveActiveWorkout({ title: "без id" }), null);
});

test("добавленное упражнение становится текущим и начинает счёт с нуля", () => {
  const workout = startActiveWorkout(START, new Date("2026-09-05T10:00:00.000Z"));
  const withSet = withRecordedSet(
    workout,
    buildSetInput(workout, { weightKg: 80, repetitions: 8, rir: 2 }),
    new Date("2026-09-05T10:05:00.000Z")
  );

  const extended = withExercise(withSet, { exerciseId: "pull-up" });

  assert.equal(extended.currentExerciseId, "pull-up");
  assert.equal(nextSetNumber(extended), 1);
  assert.equal(nextSetNumber(extended, BENCH), 2, "добавление упражнения не трогает чужие счётчики");
  assert.equal(withExercise(extended, { exerciseId: "pull-up" }).exercises.length, extended.exercises.length);
});

test("встроенный список упражнений: стабильные идентификаторы без повторов", () => {
  const ids = EXERCISE_CATALOG.map((exercise) => exercise.id);
  assert.equal(new Set(ids).size, ids.length);
  for (const exercise of EXERCISE_CATALOG) {
    assert.match(exercise.id, /^[a-z0-9-]+$/, "id уходит на сервер и в clientMutationId");
    assert.ok(exercise.title.length > 0);
  }
  assert.ok(ids.includes(BENCH) && ids.includes(SQUAT));
  assert.equal(exerciseTitle("неизвестное-упражнение"), "неизвестное-упражнение", "чужой id не должен ронять экран");
});
