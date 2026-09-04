import test from "node:test";
import assert from "node:assert/strict";

import { enqueue, itemsForUser, purgeForLogout } from "../src/platform/offline/outbox.ts";
import { logSet } from "../src/features/workout/log-set.ts";

const item = (id, userId) => ({ id, userId, createdAt: "2026-09-04T10:00:00.000Z", payload: { id } });

const items = [item("a", "user-1"), item("b", "user-2"), item("c", "user-1"), item("d", "user-3")];

test("itemsForUser возвращает только элементы текущего пользователя", () => {
  assert.deepEqual(
    itemsForUser(items, "user-1").map((i) => i.id),
    ["a", "c"]
  );
  assert.deepEqual(itemsForUser(items, "user-2").map((i) => i.id), ["b"]);
  assert.deepEqual(itemsForUser(items, "user-404"), []);
  assert.deepEqual(itemsForUser([], "user-1"), []);
});

test("itemsForUser не мутирует исходный массив", () => {
  const source = [...items];
  itemsForUser(source, "user-1");
  assert.deepEqual(source, items);
});

test("purgeForLogout удаляет только элементы вышедшего пользователя", () => {
  assert.deepEqual(
    purgeForLogout(items, "user-1").map((i) => i.id),
    ["b", "d"]
  );
  assert.deepEqual(purgeForLogout(items, "user-404").map((i) => i.id), ["a", "b", "c", "d"]);
});

test("после выхода пользователя его очередь пуста, а чужая цела", () => {
  const remaining = purgeForLogout(items, "user-1");
  assert.deepEqual(itemsForUser(remaining, "user-1"), []);
  assert.deepEqual(itemsForUser(remaining, "user-2").map((i) => i.id), ["b"]);
});

test("enqueue идемпотентен по clientMutationId в рамках пользователя", () => {
  const first = enqueue([], item("mutation-1", "user-1"));
  const second = enqueue(first, item("mutation-1", "user-1"));
  assert.equal(second.length, 1);
  const other = enqueue(second, item("mutation-1", "user-2"));
  assert.equal(other.length, 2);
});

const validSet = {
  workoutId: "demo-strength",
  exerciseId: "lat-pulldown",
  setNumber: 2,
  weightKg: 62.5,
  repetitions: 10,
  rir: 2,
  clientMutationId: "mutation-1"
};

test("logSet кладёт валидный подход в очередь текущего пользователя", () => {
  const result = logSet([], "user-1", validSet, new Date("2026-09-04T10:00:00.000Z"));
  assert.equal(result.ok, true);
  assert.deepEqual(result.outbox, [
    { id: "mutation-1", userId: "user-1", createdAt: "2026-09-04T10:00:00.000Z", payload: validSet }
  ]);
});

test("logSet не пишет в очередь невалидный подход", () => {
  const result = logSet([], "user-1", { ...validSet, repetitions: 0 });
  assert.equal(result.ok, false);
  assert.deepEqual(result.issues, ["repetitions must be between 1 and 100"]);
});
