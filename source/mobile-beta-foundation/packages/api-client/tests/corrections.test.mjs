import test from "node:test";
import assert from "node:assert/strict";

import { createApiClient } from "../src/index.ts";

const config = { environment: "development", baseUrl: "http://api.test/api/v1" };

const json = (status, body) =>
  new Response(status === 204 || body === undefined ? null : JSON.stringify(body), {
    status,
    headers: status === 204 ? {} : { "content-type": "application/json" }
  });

function stubFetch(responses) {
  const calls = [];
  const fetchMock = async (url, init) => {
    calls.push({ url, init });
    const next = responses[Math.min(calls.length - 1, responses.length - 1)];
    return typeof next === "function" ? next(url, init) : next;
  };
  return { fetchMock, calls };
}

const client = (responses) => {
  const { fetchMock, calls } = stubFetch(responses);
  return { api: createApiClient({ config, fetch: fetchMock, sleep: async () => {} }), calls };
};

const storedSet = (over = {}) => ({
  id: "set-1",
  workoutId: "w-1",
  exerciseId: "back-squat",
  setNumber: 2,
  weightKg: 102.5,
  repetitions: 6,
  rir: 1,
  clientMutationId: "w-1:back-squat:2",
  createdAt: "2026-09-05T10:00:00Z",
  updatedAt: "2026-09-05T10:30:00Z",
  deletedAt: null,
  ...over
});

const patch = { weightKg: 102.5, repetitions: 6, rir: 1, clientMutationId: "w-1:back-squat:2:edit:1" };

test("правка возвращает обновлённый подход", async () => {
  const { api, calls } = client([json(200, storedSet())]);

  const result = await api.editSet("token", "w-1", "set-1", patch);

  assert.equal(result.ok, true);
  assert.equal(result.value.outcome, "updated");
  assert.equal(result.value.set.weightKg, 102.5);
  assert.equal(calls[0].init.method, "PATCH");
  assert.deepEqual(JSON.parse(calls[0].init.body), patch);
});

test("повтор правки — успех, а не ошибка: очередь обязана снять элемент", async () => {
  const { api } = client([json(409, { error: { code: "duplicate_client_mutation", message: "already applied" }, set: storedSet() })]);

  const result = await api.editSet("token", "w-1", "set-1", { ...patch, weightKg: 999 });

  assert.equal(result.ok, true, "иначе очередь будет перепосылать применённую правку вечно");
  assert.equal(result.value.outcome, "duplicate");
  assert.equal(result.value.set.weightKg, 102.5, "сервер возвращает сохранённое, а не присланное");
});

test("правка удалённого подхода — состояние сошлось, а не поломка", async () => {
  const { api } = client([json(409, { error: { code: "set_deleted", message: "set is deleted" } })]);

  const result = await api.editSet("token", "w-1", "set-1", patch);

  assert.equal(result.ok, true);
  assert.equal(result.value.outcome, "gone", "пользователь ничего не сломал — править нечего");
});

test("правка в отменённой тренировке — постоянная ошибка", async () => {
  const { api } = client([json(409, { error: { code: "workout_not_editable", message: "cancelled" } })]);

  const result = await api.editSet("token", "w-1", "set-1", patch);

  assert.equal(result.ok, false);
  assert.equal(result.error.kind, "client");
  assert.equal(result.error.code, "workout_not_editable");
});

test("409 без сохранённого подхода — сломанный сервер, а не успех", async () => {
  const { api } = client([json(409, { error: { code: "duplicate_client_mutation", message: "oops" } })]);

  const result = await api.editSet("token", "w-1", "set-1", patch);

  assert.equal(result.ok, false);
  assert.equal(result.error.kind, "server");
});

test("удаление возвращает мягко удалённый подход", async () => {
  const { api, calls } = client([json(200, storedSet({ deletedAt: "2026-09-05T11:00:00Z" }))]);

  const result = await api.deleteSet("token", "w-1", "set-1", "w-1:back-squat:2:delete");

  assert.equal(result.ok, true);
  assert.equal(result.value.outcome, "deleted");
  assert.notEqual(result.value.set.deletedAt, null);
  assert.equal(calls[0].init.method, "DELETE");
  assert.deepEqual(JSON.parse(calls[0].init.body), { clientMutationId: "w-1:back-squat:2:delete" });
});

test("повтор удаления неотличим от первого: 200, тот же исход", async () => {
  const deleted = storedSet({ deletedAt: "2026-09-05T11:00:00Z" });
  const { api } = client([json(200, deleted), json(200, deleted)]);

  const first = await api.deleteSet("token", "w-1", "set-1", "m-del");
  const second = await api.deleteSet("token", "w-1", "set-1", "m-del");

  assert.equal(first.ok && second.ok, true);
  assert.equal(second.value.outcome, "deleted");
  assert.equal(second.value.set.deletedAt, first.value.set.deletedAt);
});

test("чужой подход не отличить от несуществующего", async () => {
  const { api } = client([json(404, { error: { code: "not_found", message: "workout not found" } })]);

  const result = await api.editSet("token", "w-1", "set-1", patch);

  assert.equal(result.ok, false);
  assert.equal(result.error.status, 404);
  assert.equal(result.error.code, "not_found");
});

test("выход за доменные границы отвергается сервером как 422", async () => {
  const { api } = client([
    json(422, { error: { code: "validation_failed", message: "weightKg", details: [{ field: "weightKg", issue: "out of range" }] } })
  ]);

  const result = await api.editSet("token", "w-1", "set-1", { ...patch, weightKg: 1001 });

  assert.equal(result.ok, false);
  assert.equal(result.error.code, "validation_failed");
});
