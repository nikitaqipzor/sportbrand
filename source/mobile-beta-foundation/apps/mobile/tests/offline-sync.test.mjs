import test from "node:test";
import assert from "node:assert/strict";

import {
  createMemoryOutboxStore,
  createMemorySnapshotStore,
  createOutboxMemoryDb,
  createSnapshotMemoryDb,
  createOutboxSync,
  deadRecords,
  pendingRecords
} from "../src/platform/offline/index.ts";
import { createWorkoutOffline } from "../src/features/workout/workout-offline.ts";
import { startActiveWorkout, buildSetInput } from "../src/features/workout/active-workout.ts";

const ALICE = "user-alice";
const BOB = "user-bob";

const setInput = (n, workoutId = "w-1") => ({
  workoutId,
  exerciseId: "lat-pulldown",
  setNumber: n,
  weightKg: 60 + n,
  repetitions: 10,
  rir: 2,
  clientMutationId: `${workoutId}:lat-pulldown:${n}`
});

const created = (input) => ({ ok: true, value: { outcome: "created", set: { id: `s-${input.setNumber}` } } });
const duplicate = (input) => ({ ok: true, value: { outcome: "duplicate", set: { id: `s-${input.setNumber}` } } });
const offline = () => ({ ok: false, error: { kind: "network", message: "нет сети", attempts: 3 } });
const rejected = (status, code) => ({ ok: false, error: { kind: "client", status, code, message: code, attempts: 1 } });
const expired = () => ({ ok: false, error: { kind: "session_expired", message: "сессия истекла" } });

/** Отправитель-шпион: запоминает КАЖДЫЙ вызов, чтобы ловить двойную отправку. */
function spySender(reply = created) {
  const calls = [];
  const send = async (workoutId, input) => {
    calls.push({ workoutId, mutationId: input.clientMutationId });
    return typeof reply === "function" ? reply(input) : reply;
  };
  return { send, calls };
}

const at = (iso) => () => new Date(iso);

test("подход переживает перезапуск процесса: очередь лежит на диске, а не в памяти", async () => {
  const db = createOutboxMemoryDb();
  const first = createOutboxSync({ store: createMemoryOutboxStore(db), send: spySender().send, now: at("2026-09-05T10:00:00.000Z") });
  await first.enqueue(ALICE, setInput(1));
  await first.enqueue(ALICE, setInput(2));

  // Процесс умер: новый sync поверх того же «диска».
  const revived = createOutboxSync({ store: createMemoryOutboxStore(db), send: spySender().send, now: at("2026-09-05T10:00:05.000Z") });
  const survived = await revived.list(ALICE);

  assert.equal(survived.length, 2, "оба подхода обязаны пережить перезапуск");
  assert.deepEqual(survived.map((r) => r.payload.setNumber), [1, 2]);
});

test("повторная постановка того же clientMutationId не создаёт второй строки", async () => {
  const sync = createOutboxSync({ store: createMemoryOutboxStore(), send: spySender().send, now: at("2026-09-05T10:00:00.000Z") });
  await sync.enqueue(ALICE, setInput(1));
  await sync.enqueue(ALICE, setInput(1));
  assert.equal((await sync.list(ALICE)).length, 1);
});

test("created и duplicate одинаково снимают элемент с очереди", async () => {
  const sync = createOutboxSync({ store: createMemoryOutboxStore(), send: spySender(duplicate).send, now: at("2026-09-05T10:00:00.000Z") });
  await sync.enqueue(ALICE, setInput(1));

  const summary = await sync.flush(ALICE);

  assert.equal(summary.duplicates, 1, "409 — это успех: сервер уже принял мутацию");
  assert.equal(summary.sent, 0);
  assert.equal((await sync.list(ALICE)).length, 0, "принятый подход не должен остаться в очереди");
});

test("сеть пропала — подход остаётся в очереди и не теряется", async () => {
  const sync = createOutboxSync({ store: createMemoryOutboxStore(), send: spySender(offline).send, now: at("2026-09-05T10:00:00.000Z") });
  await sync.enqueue(ALICE, setInput(1));

  const summary = await sync.flush(ALICE);
  const queue = await sync.list(ALICE);

  assert.equal(summary.reason, "retry");
  assert.equal(queue.length, 1, "элемент удаляется только после подтверждения сервера");
  assert.equal(queue[0].attempts, 1);
  assert.ok(Date.parse(queue[0].nextAttemptAt) > Date.parse("2026-09-05T10:00:00.000Z"), "повтор обязан быть отложен");
});

test("навсегда невалидная мутация уходит в «мёртвые» и не блокирует очередь", async () => {
  const store = createMemoryOutboxStore();
  const replies = { 1: rejected(422, "validation_failed"), 2: created(setInput(2)) };
  const seen = [];
  const sync = createOutboxSync({
    store,
    send: async (_workoutId, input) => {
      seen.push(input.setNumber);
      return replies[input.setNumber];
    },
    now: at("2026-09-05T10:00:00.000Z")
  });
  await sync.enqueue(ALICE, setInput(1));
  await sync.enqueue(ALICE, setInput(2));

  const summary = await sync.flush(ALICE);
  const records = await sync.list(ALICE);

  assert.equal(summary.dead, 1);
  assert.equal(summary.sent, 1, "второй подход обязан уйти, несмотря на мёртвый первый");
  assert.deepEqual(seen, [1, 2], "мёртвый элемент не встаёт пробкой перед остальными");
  assert.equal(deadRecords(records).length, 1);
  assert.equal(pendingRecords(records).length, 0);
});

test("истёкшая сессия ставит отправку на паузу до следующего входа", async () => {
  const spy = spySender(expired);
  const sync = createOutboxSync({ store: createMemoryOutboxStore(), send: spy.send, now: at("2026-09-05T10:00:00.000Z") });
  await sync.enqueue(ALICE, setInput(1));

  assert.equal((await sync.flush(ALICE)).reason, "paused");
  assert.equal(sync.isPaused(), true);

  assert.equal((await sync.flush(ALICE)).reason, "paused", "на паузе новых попыток быть не должно");
  assert.equal(spy.calls.length, 1, "второй flush не обязан дёргать сеть");

  sync.resume();
  assert.equal(sync.isPaused(), false);
});

test("два параллельных прохода не отправляют один подход дважды", async () => {
  const spy = spySender();
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  const sync = createOutboxSync({
    store: createMemoryOutboxStore(),
    send: async (workoutId, input) => {
      await gate;
      return spy.send(workoutId, input);
    },
    now: at("2026-09-05T10:00:00.000Z")
  });
  await sync.enqueue(ALICE, setInput(1));

  const both = Promise.all([sync.flush(ALICE), sync.flush(ALICE)]);
  release();
  const [first, second] = await both;

  assert.equal(spy.calls.length, 1, "элемент обязан уйти на сервер ровно один раз");
  assert.ok([first.reason, second.reason].includes("busy"), "второй проход обязан примкнуть к первому, а не начать свой");
});

test("подходы уходят в порядке записи, а не в произвольном", async () => {
  const spy = spySender();
  const sync = createOutboxSync({ store: createMemoryOutboxStore(), send: spy.send, now: at("2026-09-05T10:00:00.000Z") });
  for (const n of [1, 2, 3]) await sync.enqueue(ALICE, setInput(n));

  await sync.flush(ALICE);

  assert.deepEqual(spy.calls.map((c) => c.mutationId), [
    "w-1:lat-pulldown:1",
    "w-1:lat-pulldown:2",
    "w-1:lat-pulldown:3"
  ]);
});

test("отправка невозможна, когда некому отправлять", async () => {
  const spy = spySender();
  const sync = createOutboxSync({ store: createMemoryOutboxStore(), send: spy.send, now: at("2026-09-05T10:00:00.000Z") });
  assert.equal((await sync.flush(null)).reason, "no-user");
  assert.equal(spy.calls.length, 0);
});

test("РЕГРЕССИЯ H1: подходы вышедшего пользователя не уходят под токеном следующего", async () => {
  const outboxDb = createOutboxMemoryDb();
  const snapshotDb = createSnapshotMemoryDb();
  const spy = spySender();
  const offlineQueue = createWorkoutOffline({
    sync: createOutboxSync({
      store: createMemoryOutboxStore(outboxDb),
      send: spy.send,
      now: at("2026-09-05T10:00:00.000Z")
    }),
    snapshots: createMemorySnapshotStore(snapshotDb),
    now: at("2026-09-05T10:00:00.000Z")
  });

  // Алиса тренируется в самолётном режиме: три подхода легли на диск.
  const workout = await offlineQueue.start(ALICE, { workoutId: "w-1", title: "Тяга верхнего блока", exerciseId: "lat-pulldown" });
  let current = workout;
  for (const measures of [{ weightKg: 60, repetitions: 10, rir: 2 }, { weightKg: 62.5, repetitions: 9, rir: 2 }]) {
    const result = await offlineQueue.recordSet(ALICE, current, measures);
    assert.equal(result.ok, true);
    current = result.workout;
  }
  assert.equal((await offlineQueue.list(ALICE)).length, 2);
  assert.notEqual(await offlineQueue.load(ALICE), null, "снимок активной тренировки сохранён");

  // Алиса выходит из аккаунта.
  await offlineQueue.onSessionEnded({ userId: ALICE, reason: "user" });

  assert.equal((await offlineQueue.list(ALICE)).length, 0, "очередь ушедшего пользователя стёрта");
  assert.equal(await offlineQueue.load(ALICE), null, "снимок ушедшего пользователя стёрт");

  // Входит Боб и синхронизируется.
  offlineQueue.signedIn(BOB);
  const summary = await offlineQueue.flush(BOB);

  assert.equal((await offlineQueue.list(BOB)).length, 0, "у Боба не должно быть чужих подходов");
  assert.equal(summary.sent, 0);
  assert.equal(spy.calls.length, 0, "ни один подход Алисы не уехал под сессией Боба");
});

test("отменённая тренировка не доезжает до сервера, завершённая — доезжает", async () => {
  const build = () => {
    const spy = spySender();
    const queue = createWorkoutOffline({
      sync: createOutboxSync({ store: createMemoryOutboxStore(), send: spy.send, now: at("2026-09-05T10:00:00.000Z") }),
      snapshots: createMemorySnapshotStore(),
      now: at("2026-09-05T10:00:00.000Z")
    });
    return { spy, queue };
  };

  const cancelled = build();
  let workout = await cancelled.queue.start(ALICE, { workoutId: "w-1", title: "Тяга", exerciseId: "lat-pulldown" });
  workout = (await cancelled.queue.recordSet(ALICE, workout, { weightKg: 60, repetitions: 10, rir: 2 })).workout;
  const cancelResult = await cancelled.queue.finish(ALICE, workout, "cancel");
  assert.equal(cancelResult.ok, true);
  assert.equal(cancelResult.discarded, 1, "неотправленные подходы отменённой тренировки снимаются");
  await cancelled.queue.flush(ALICE);
  assert.equal(cancelled.spy.calls.length, 0, "отменённая тренировка не должна попасть на сервер");

  const completed = build();
  let done = await completed.queue.start(ALICE, { workoutId: "w-2", title: "Тяга", exerciseId: "lat-pulldown" });
  done = (await completed.queue.recordSet(ALICE, done, { weightKg: 60, repetitions: 10, rir: 2 })).workout;
  const finishResult = await completed.queue.finish(ALICE, done, "complete");
  assert.equal(finishResult.ok, true);
  assert.equal(finishResult.discarded, 0);
  await completed.queue.flush(ALICE);
  assert.equal(completed.spy.calls.length, 1, "завершённая тренировка обязана досинхронизироваться");
});

test("невалидный подход не доезжает до хранилища", async () => {
  const spy = spySender();
  const queue = createWorkoutOffline({
    sync: createOutboxSync({ store: createMemoryOutboxStore(), send: spy.send, now: at("2026-09-05T10:00:00.000Z") }),
    snapshots: createMemorySnapshotStore(),
    now: at("2026-09-05T10:00:00.000Z")
  });
  const workout = await queue.start(ALICE, { workoutId: "w-1", title: "Тяга", exerciseId: "lat-pulldown" });

  const result = await queue.recordSet(ALICE, workout, { weightKg: 2000, repetitions: 10, rir: 2 });

  assert.equal(result.ok, false);
  assert.ok(result.issues.length > 0);
  assert.equal((await queue.list(ALICE)).length, 0, "мусор не должен попадать в очередь");
});

test("детерминированный clientMutationId: тот же подход после перезапуска узнаётся сервером", () => {
  const workout = startActiveWorkout({ workoutId: "w-1", title: "Тяга", exerciseId: "lat-pulldown" }, new Date("2026-09-05T10:00:00.000Z"));
  const first = buildSetInput(workout, { weightKg: 60, repetitions: 10, rir: 2 }, 1);
  const again = buildSetInput(workout, { weightKg: 60, repetitions: 10, rir: 2 }, 1);
  assert.equal(first.clientMutationId, again.clientMutationId);
});

// --- Тренировка на сервере раньше своих подходов ---
//
// До этого приложение писало подходы в тренировку, которой на сервере не
// существовало: POST /workouts/{id}/sets отвечал 404, а 404 — это 4xx, то есть
// очередь уводила КАЖДЫЙ подход в «мёртвые». Петля была разорвана целиком.

import { createMemoryWorkoutRegistry, createWorkoutRegistryMemoryDb } from "../src/platform/offline/index.ts";

/** Пишет порядок вызовов, чтобы поймать создание после подхода. */
function tracedDeps({ createReply = () => ({ ok: true, value: { id: "w-1" } }), sendReply = created } = {}) {
  const order = [];
  const registryDb = createWorkoutRegistryMemoryDb();
  const registry = createMemoryWorkoutRegistry(registryDb);
  const deps = {
    store: createMemoryOutboxStore(),
    registry,
    createWorkout: async (workoutId, title) => {
      order.push({ call: "create", workoutId, title });
      return createReply(workoutId, title);
    },
    send: async (workoutId, input) => {
      order.push({ call: "send", workoutId, mutationId: input.clientMutationId });
      return typeof sendReply === "function" ? sendReply(input) : sendReply;
    },
    now: at("2026-09-05T10:00:00.000Z")
  };
  return { deps, order, registry };
}

test("тренировка создаётся на сервере раньше первого подхода", async () => {
  const { deps, order, registry } = tracedDeps();
  await registry.remember(ALICE, "w-1", "Тяга верхнего блока");
  const sync = createOutboxSync(deps);
  await sync.enqueue(ALICE, setInput(1));

  await sync.flush(ALICE);

  assert.equal(order[0].call, "create", "подход в несуществующую тренировку вернул бы 404");
  assert.equal(order[0].title, "Тяга верхнего блока", "название обязано доехать вместе с тренировкой");
  assert.equal(order[1].call, "send");
});

test("создание не повторяется перед каждым подходом", async () => {
  const { deps, order, registry } = tracedDeps();
  await registry.remember(ALICE, "w-1", "Тяга");
  const sync = createOutboxSync(deps);
  for (const n of [1, 2, 3]) await sync.enqueue(ALICE, setInput(n));

  await sync.flush(ALICE);

  assert.equal(order.filter((entry) => entry.call === "create").length, 1);
  assert.equal(order.filter((entry) => entry.call === "send").length, 3);
});

test("не удалось создать тренировку — подходы ждут, а не гибнут", async () => {
  const { deps, order, registry } = tracedDeps({ createReply: offline });
  await registry.remember(ALICE, "w-1", "Тяга");
  const sync = createOutboxSync(deps);
  await sync.enqueue(ALICE, setInput(1));

  const summary = await sync.flush(ALICE);
  const queue = await sync.list(ALICE);

  assert.equal(summary.reason, "retry");
  assert.equal(order.filter((entry) => entry.call === "send").length, 0, "нельзя писать подход в несозданную тренировку");
  assert.equal(pendingRecords(queue).length, 1, "подход обязан дождаться, а не уйти в «мёртвые»");
  assert.equal(deadRecords(queue).length, 0);
});

test("подтверждённая тренировка больше не создаётся при следующем проходе", async () => {
  const { deps, order, registry } = tracedDeps();
  await registry.remember(ALICE, "w-1", "Тяга");
  const sync = createOutboxSync(deps);

  await sync.enqueue(ALICE, setInput(1));
  await sync.flush(ALICE);
  await sync.enqueue(ALICE, setInput(2));
  await sync.flush(ALICE);

  assert.equal(order.filter((entry) => entry.call === "create").length, 1);
});

test("название переживает завершение тренировки: снимок стёрт, реестр помнит", async () => {
  const registryDb = createWorkoutRegistryMemoryDb();
  const registry = createMemoryWorkoutRegistry(registryDb);
  const spy = spySender();
  const queue = createWorkoutOffline({
    sync: createOutboxSync({ store: createMemoryOutboxStore(), send: spy.send, now: at("2026-09-05T10:00:00.000Z") }),
    snapshots: createMemorySnapshotStore(),
    registry,
    now: at("2026-09-05T10:00:00.000Z")
  });

  let workout = await queue.start(ALICE, { workoutId: "w-1", title: "Тяга верхнего блока", exerciseId: "lat-pulldown" });
  workout = (await queue.recordSet(ALICE, workout, { weightKg: 60, repetitions: 10, rir: 2 })).workout;
  await queue.finish(ALICE, workout, "complete");

  assert.equal(await queue.load(ALICE), null, "снимок завершённой тренировки стёрт");
  const remembered = await registry.get(ALICE, "w-1");
  assert.equal(remembered?.title, "Тяга верхнего блока", "иначе тренировка доехала бы на сервер безымянной");
});

test("выход из аккаунта стирает и реестр тренировок ушедшего", async () => {
  const registry = createMemoryWorkoutRegistry();
  const queue = createWorkoutOffline({
    sync: createOutboxSync({ store: createMemoryOutboxStore(), send: spySender().send, now: at("2026-09-05T10:00:00.000Z") }),
    snapshots: createMemorySnapshotStore(),
    registry,
    now: at("2026-09-05T10:00:00.000Z")
  });
  await queue.start(ALICE, { workoutId: "w-1", title: "Тяга", exerciseId: "lat-pulldown" });

  await queue.onSessionEnded({ userId: ALICE, reason: "user" });

  assert.equal(await registry.get(ALICE, "w-1"), null);
});

test("отменённая тренировка забывается: её незачем создавать на сервере", async () => {
  const registry = createMemoryWorkoutRegistry();
  const queue = createWorkoutOffline({
    sync: createOutboxSync({ store: createMemoryOutboxStore(), send: spySender().send, now: at("2026-09-05T10:00:00.000Z") }),
    snapshots: createMemorySnapshotStore(),
    registry,
    now: at("2026-09-05T10:00:00.000Z")
  });
  let workout = await queue.start(ALICE, { workoutId: "w-1", title: "Тяга", exerciseId: "lat-pulldown" });
  workout = (await queue.recordSet(ALICE, workout, { weightKg: 60, repetitions: 10, rir: 2 })).workout;

  await queue.finish(ALICE, workout, "cancel");

  assert.equal(await registry.get(ALICE, "w-1"), null);
});
