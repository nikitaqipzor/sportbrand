import test from "node:test";
import assert from "node:assert/strict";

import {
  createMemoryOutboxStore,
  createMemorySnapshotStore,
  createMemoryWorkoutRegistry,
  createOutboxSync
} from "../src/platform/offline/index.ts";
import { createWorkoutOffline } from "../src/features/workout/workout-offline.ts";
import { deleteMutationId, editMutationId } from "../src/platform/offline/mutations.ts";

const ALICE = "user-alice";
const at = (iso) => () => new Date(iso);
const NOW = at("2026-09-06T10:00:00.000Z");

function build() {
  const calls = [];
  const sync = createOutboxSync({
    store: createMemoryOutboxStore(),
    send: async (workoutId, input) => {
      calls.push({ call: "log", n: input.setNumber });
      return { ok: true, value: { outcome: "created", set: { id: `srv-${input.setNumber}` } } };
    },
    editSet: async (workoutId, setId, patch, mutationId) => {
      calls.push({ call: "edit", setId, patch, mutationId });
      return { ok: true, value: { outcome: "updated" } };
    },
    deleteSet: async (workoutId, setId, mutationId) => {
      calls.push({ call: "delete", setId, mutationId });
      return { ok: true, value: { outcome: "deleted" } };
    },
    createWorkout: async () => ({ ok: true, value: {} }),
    registry: createMemoryWorkoutRegistry(),
    now: NOW
  });
  const offline = createWorkoutOffline({
    sync,
    snapshots: createMemorySnapshotStore(),
    registry: createMemoryWorkoutRegistry(),
    now: NOW
  });
  return { offline, sync, calls };
}

const good = { weightKg: 102.5, repetitions: 6, rir: 1 };

test("правка проходит доменные границы, а не обходит их", async () => {
  const { offline, sync } = build();

  for (const bad of [
    { weightKg: 1001, repetitions: 6, rir: 1 },
    { weightKg: -1, repetitions: 6, rir: 1 },
    { weightKg: 100, repetitions: 0, rir: 1 },
    { weightKg: 100, repetitions: 101, rir: 1 },
    { weightKg: 100, repetitions: 6, rir: 11 }
  ]) {
    const result = await offline.editSet(ALICE, "w-1", "srv-1", bad);
    assert.equal(result.ok, false, `${JSON.stringify(bad)} обязано быть отвергнуто`);
  }

  assert.equal((await sync.list(ALICE)).length, 0, "мусор не должен попадать в очередь");
});

test("правка встаёт в очередь и уходит правкой", async () => {
  const { offline, calls } = build();

  const result = await offline.editSet(ALICE, "w-1", "srv-1", good);
  await offline.flush(ALICE);

  assert.equal(result.ok, true);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].call, "edit");
  assert.deepEqual(calls[0].patch, good);
});

test("две правки одного подхода не схлопываются в одну", async () => {
  const { offline, sync } = build();

  await offline.editSet(ALICE, "w-1", "srv-1", good, 1);
  await offline.editSet(ALICE, "w-1", "srv-1", { weightKg: 105, repetitions: 5, rir: 0 }, 2);

  const queued = await sync.list(ALICE);
  assert.equal(queued.length, 2, "без revision вторая правка получила бы id первой и была бы отброшена сервером");
  assert.deepEqual(queued.map((r) => r.id), [editMutationId("srv-1", 1), editMutationId("srv-1", 2)]);
});

test("удаление — одно намерение, сколько раз ни повтори", async () => {
  const { offline, sync } = build();

  await offline.deleteSet(ALICE, "w-1", "srv-1");
  await offline.deleteSet(ALICE, "w-1", "srv-1");

  const queued = await sync.list(ALICE);
  assert.equal(queued.length, 1);
  assert.equal(queued[0].id, deleteMutationId("srv-1"));
});

test("правка ещё не отправленного подхода не порождает второй мутации", async () => {
  const { offline, sync, calls } = build();
  const workout = await offline.start(ALICE, { workoutId: "w-1", title: "Тяга", exerciseId: "back-squat" });
  const recorded = await offline.recordSet(ALICE, workout, { weightKg: 100, repetitions: 5, rir: 2 });
  assert.equal(recorded.ok, true);

  // Правим по идентификатору мутации записи, а не по серверному id: его ещё нет.
  const amended = await sync.amendPending(ALICE, recorded.record.id, good);
  const queued = await sync.list(ALICE);

  assert.equal(amended, true);
  assert.equal(queued.length, 1, "правка неотправленного подхода не должна добавлять элемент");
  assert.equal(queued[0].payload.input.weightKg, 102.5);

  await offline.flush(ALICE);
  assert.deepEqual(calls.map((c) => c.call), ["log"], "серверу уходит одна запись с исправленным весом");
});

test("правка работает без сети: элемент ждёт в очереди, а не теряется", async () => {
  const sync = createOutboxSync({
    store: createMemoryOutboxStore(),
    send: async () => ({ ok: true, value: { outcome: "created", set: {} } }),
    editSet: async () => ({ ok: false, error: { kind: "network", message: "нет сети", attempts: 3 } }),
    deleteSet: async () => ({ ok: true, value: { outcome: "deleted" } }),
    createWorkout: async () => ({ ok: true, value: {} }),
    registry: createMemoryWorkoutRegistry(),
    now: NOW
  });
  const offline = createWorkoutOffline({ sync, snapshots: createMemorySnapshotStore(), now: NOW });

  await offline.editSet(ALICE, "w-1", "srv-1", good);
  const summary = await offline.flush(ALICE);

  assert.equal(summary.reason, "retry");
  assert.equal((await sync.list(ALICE)).length, 1, "правка обязана дождаться связи");
});
