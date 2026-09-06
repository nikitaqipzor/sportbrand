import test from "node:test";
import assert from "node:assert/strict";

import {
  createMemoryOutboxStore,
  createMemoryWorkoutRegistry,
  createOutboxMemoryDb,
  createOutboxSync,
  deadRecords,
  pendingRecords
} from "../src/platform/offline/index.ts";
import { deleteMutationId, editMutationId, patchPendingSet } from "../src/platform/offline/mutations.ts";

const ALICE = "user-alice";
const at = (iso) => () => new Date(iso);
const NOW = at("2026-09-06T10:00:00.000Z");

const setInput = (n, workoutId = "w-1") => ({
  workoutId,
  exerciseId: "back-squat",
  setNumber: n,
  weightKg: 100,
  repetitions: 5,
  rir: 2,
  clientMutationId: `${workoutId}:back-squat:${n}`
});

/** Три отправителя-шпиона: важно, что каждая мутация уходит своим путём. */
function senders({ editReply = { ok: true, value: { outcome: "updated" } }, deleteReply = { ok: true, value: { outcome: "deleted" } } } = {}) {
  const calls = [];
  return {
    calls,
    send: async (workoutId, input) => {
      calls.push({ call: "log", mutationId: input.clientMutationId });
      return { ok: true, value: { outcome: "created", set: { id: `srv-${input.setNumber}` } } };
    },
    editSet: async (workoutId, setId, patch, mutationId) => {
      calls.push({ call: "edit", setId, patch, mutationId });
      return editReply;
    },
    deleteSet: async (workoutId, setId, mutationId) => {
      calls.push({ call: "delete", setId, mutationId });
      return deleteReply;
    },
    createWorkout: async () => ({ ok: true, value: {} })
  };
}

const build = (over = {}) => {
  const spy = senders(over.replies);
  const registry = createMemoryWorkoutRegistry();
  const sync = createOutboxSync({
    store: createMemoryOutboxStore(over.db ?? createOutboxMemoryDb()),
    send: spy.send,
    editSet: over.noEdit ? undefined : spy.editSet,
    deleteSet: over.noDelete ? undefined : spy.deleteSet,
    createWorkout: spy.createWorkout,
    registry,
    now: NOW
  });
  return { sync, spy, registry };
};

test("правка уходит правкой, а не второй записью подхода", async () => {
  const { sync, spy } = build();
  await sync.enqueueMutation(ALICE, editMutationId("srv-1", 1), {
    kind: "edit-set",
    workoutId: "w-1",
    setId: "srv-1",
    patch: { weightKg: 102.5, repetitions: 6, rir: 1 }
  });

  await sync.flush(ALICE);

  assert.equal(spy.calls.length, 1);
  assert.equal(spy.calls[0].call, "edit");
  assert.equal(spy.calls[0].setId, "srv-1");
  assert.equal(spy.calls[0].mutationId, "edit:srv-1:1", "идентификатор правки едет на сервер как есть");
});

test("правка не может уехать раньше своей записи", async () => {
  const { sync, spy } = build();
  await sync.enqueue(ALICE, setInput(1));
  await sync.enqueueMutation(ALICE, editMutationId("srv-1", 1), {
    kind: "edit-set",
    workoutId: "w-1",
    setId: "srv-1",
    patch: { weightKg: 102.5, repetitions: 6, rir: 1 }
  });

  await sync.flush(ALICE);

  assert.deepEqual(
    spy.calls.map((c) => c.call),
    ["log", "edit"],
    "порядок очереди — это порядок намерений человека"
  );
});

test("перед правкой тренировку создавать незачем: подход на сервере уже есть", async () => {
  const created = [];
  const registry = createMemoryWorkoutRegistry();
  const sync = createOutboxSync({
    store: createMemoryOutboxStore(),
    send: async () => ({ ok: true, value: { outcome: "created", set: {} } }),
    editSet: async () => ({ ok: true, value: { outcome: "updated" } }),
    deleteSet: async () => ({ ok: true, value: { outcome: "deleted" } }),
    createWorkout: async (id) => {
      created.push(id);
      return { ok: true, value: {} };
    },
    registry,
    now: NOW
  });
  await sync.enqueueMutation(ALICE, deleteMutationId("srv-9"), { kind: "delete-set", workoutId: "w-1", setId: "srv-9" });

  await sync.flush(ALICE);

  assert.deepEqual(created, [], "создание тренировки нужно только перед записью подхода");
});

test("«уже применено» и «уже удалено» снимают элемент, а не крутят его вечно", async () => {
  for (const outcome of ["duplicate", "gone"]) {
    const { sync } = build({ replies: { editReply: { ok: true, value: { outcome } } } });
    await sync.enqueueMutation(ALICE, editMutationId("srv-1", 1), {
      kind: "edit-set",
      workoutId: "w-1",
      setId: "srv-1",
      patch: { weightKg: 102.5, repetitions: 6, rir: 1 }
    });

    const summary = await sync.flush(ALICE);

    assert.equal(summary.duplicates, 1, `исход ${outcome} обязан считаться успехом`);
    assert.equal((await sync.list(ALICE)).length, 0, `исход ${outcome} обязан снять элемент с очереди`);
  }
});

test("отменённая тренировка — постоянный отказ, элемент уходит в «мёртвые»", async () => {
  const { sync } = build({
    replies: {
      editReply: {
        ok: false,
        error: { kind: "client", status: 409, code: "workout_not_editable", message: "cancelled", details: [] }
      }
    }
  });
  await sync.enqueueMutation(ALICE, editMutationId("srv-1", 1), {
    kind: "edit-set",
    workoutId: "w-1",
    setId: "srv-1",
    patch: { weightKg: 102.5, repetitions: 6, rir: 1 }
  });

  const summary = await sync.flush(ALICE);
  const records = await sync.list(ALICE);

  assert.equal(summary.dead, 1);
  assert.equal(deadRecords(records).length, 1, "иначе правка висела бы в очереди вечно");
  assert.equal(pendingRecords(records).length, 0);
});

test("вид мутации без отправителя не крутится в очереди, а уходит в «мёртвые»", async () => {
  const { sync } = build({ noEdit: true });
  await sync.enqueueMutation(ALICE, editMutationId("srv-1", 1), {
    kind: "edit-set",
    workoutId: "w-1",
    setId: "srv-1",
    patch: { weightKg: 102.5, repetitions: 6, rir: 1 }
  });

  const summary = await sync.flush(ALICE);

  assert.equal(summary.dead, 1, "ошибка проводки приложения не должна выглядеть как сбой связи");
});

test("правка ещё не отправленного подхода меняет очередь, а не шлёт запрос", async () => {
  const { sync, spy } = build();
  const record = await sync.enqueue(ALICE, setInput(1));

  const amended = await sync.amendPending(ALICE, record.id, { weightKg: 97.5, repetitions: 8, rir: 3 });
  const queued = (await sync.list(ALICE))[0];

  assert.equal(amended, true);
  assert.equal(queued.payload.input.weightKg, 97.5);
  assert.equal(queued.payload.input.clientMutationId, record.id, "идентичность записи не меняется от правки");
  assert.equal(spy.calls.length, 0, "серверу нечего править: этого подхода он не видел");
});

test("удаление ещё не отправленного подхода снимает его с очереди", async () => {
  const { sync, spy } = build();
  const record = await sync.enqueue(ALICE, setInput(1));

  await sync.discardWorkout(ALICE, "w-1");
  await sync.flush(ALICE);

  assert.equal((await sync.list(ALICE)).length, 0);
  assert.equal(spy.calls.length, 0, "нельзя просить сервер удалить строку, которой он не видел");
  void record;
});

test("идентификаторы правки и удаления детерминированы", () => {
  assert.equal(editMutationId("srv-1", 1), editMutationId("srv-1", 1));
  assert.notEqual(editMutationId("srv-1", 1), editMutationId("srv-1", 2), "две разные правки не должны схлопнуться");
  assert.equal(deleteMutationId("srv-1"), deleteMutationId("srv-1"), "удаление — одно намерение, сколько ни повторяй");
});

test("patchPendingSet не трогает мутации, которые не являются записью", () => {
  const del = { kind: "delete-set", workoutId: "w-1", setId: "srv-1" };
  assert.deepEqual(patchPendingSet(del, { weightKg: 1, repetitions: 1, rir: 1 }), del);
});
