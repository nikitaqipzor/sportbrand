import test from "node:test";
import assert from "node:assert/strict";

import {
  deleteMutationId,
  editMutationId,
  needsWorkout,
  patchPendingSet,
  workoutIdOf
} from "../src/platform/offline/mutations.ts";

const logSet = {
  kind: "log-set",
  workoutId: "w-1",
  input: {
    workoutId: "w-1",
    exerciseId: "back-squat",
    setNumber: 2,
    weightKg: 100,
    repetitions: 5,
    rir: 2,
    clientMutationId: "w-1:back-squat:2"
  }
};

const editSet = { kind: "edit-set", workoutId: "w-1", setId: "s-9", patch: { weightKg: 102.5, repetitions: 6, rir: 1 } };
const deleteSet = { kind: "delete-set", workoutId: "w-1", setId: "s-9" };

test("тренировку нужно создать только под запись подхода", () => {
  // Правка и удаление ссылаются на подход, который на сервере уже есть,
  // а значит и тренировка тоже — лишний вызов создания был бы шумом.
  assert.equal(needsWorkout(logSet), true);
  assert.equal(needsWorkout(editSet), false);
  assert.equal(needsWorkout(deleteSet), false);
});

test("у всех видов мутаций достаётся тренировка", () => {
  for (const mutation of [logSet, editSet, deleteSet]) {
    assert.equal(workoutIdOf(mutation), "w-1");
  }
});

test("правка неотправленного подхода меняет саму запись в очереди", () => {
  // Слать серверу правку записи, которой он не видел, невозможно:
  // идентификатора подхода ещё не существует.
  const corrected = patchPendingSet(logSet, { weightKg: 102.5, repetitions: 6, rir: 1 });

  assert.equal(corrected.kind, "log-set");
  assert.equal(corrected.input.weightKg, 102.5);
  assert.equal(corrected.input.repetitions, 6);
  assert.equal(corrected.input.rir, 1);
});

test("правка в очереди сохраняет идентичность записи", () => {
  const corrected = patchPendingSet(logSet, { weightKg: 102.5, repetitions: 6, rir: 1 });

  // Идентификатор мутации, упражнение и номер подхода — это идентичность
  // записи. Смена любого из них превратила бы правку во вторую запись.
  assert.equal(corrected.input.clientMutationId, "w-1:back-squat:2");
  assert.equal(corrected.input.exerciseId, "back-squat");
  assert.equal(corrected.input.setNumber, 2);
});

test("идентификатор удаления один и тот же при любом числе повторов", () => {
  assert.equal(deleteMutationId("s-9"), deleteMutationId("s-9"));
  assert.notEqual(deleteMutationId("s-9"), deleteMutationId("s-10"));
});

test("идентификатор правки детерминирован и различает последовательные правки", () => {
  assert.equal(editMutationId("s-9", 1), editMutationId("s-9", 1));
  assert.notEqual(editMutationId("s-9", 1), editMutationId("s-9", 2));
  assert.notEqual(editMutationId("s-9", 1), deleteMutationId("s-9"));
});
