import test from "node:test";
import assert from "node:assert/strict";

import { createApiClient } from "../src/index.ts";

const config = { environment: "development", baseUrl: "http://api.test/api/v1" };

/** 204 не имеет тела — Response это и не позволит, как и настоящий сервер. */
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

test("фильтры истории уезжают в строку запроса, повторяемый статус — несколькими параметрами", async () => {
  const { api, calls } = client([json(200, { items: [], nextCursor: null })]);

  await api.listWorkouts("token", {
    status: ["completed", "cancelled"],
    from: "2026-08-01",
    to: "2026-09-01",
    limit: 50
  });

  const url = new URL(calls[0].url);
  assert.equal(url.pathname, "/api/v1/workouts");
  assert.deepEqual(url.searchParams.getAll("status"), ["completed", "cancelled"]);
  assert.equal(url.searchParams.get("from"), "2026-08-01");
  assert.equal(url.searchParams.get("limit"), "50");
  assert.equal(calls[0].init.headers.authorization, "Bearer token");
});

test("курсор проходит насквозь нетронутым: клиент его не разбирает и не собирает", async () => {
  const opaque = "eyJjcmVhdGVkQXQiOiIyMDI2LTA5LTAxIn0=";
  const { api, calls } = client([json(200, { items: [], nextCursor: null })]);

  await api.listWorkouts("token", { cursor: opaque });

  assert.equal(new URL(calls[0].url).searchParams.get("cursor"), opaque);
});

test("страница без nextCursor читается как последняя, а не как сбой", async () => {
  const { api } = client([json(200, { items: [{ id: "w-1", title: "Тяга", status: "completed", createdAt: "2026-09-01T10:00:00Z" }] })]);

  const result = await api.listWorkouts("token");

  assert.equal(result.ok, true);
  assert.equal(result.value.nextCursor, null);
  assert.equal(result.value.items.length, 1);
});

test("детали тренировки отдают подходы и итоги", async () => {
  const { api } = client([
    json(200, {
      id: "w-1",
      title: "Тяга верхнего блока",
      status: "completed",
      createdAt: "2026-09-01T10:00:00Z",
      endedAt: "2026-09-01T11:00:00Z",
      sets: [{ id: "s-1", workoutId: "w-1", exerciseId: "lat-pulldown", setNumber: 1, weightKg: 60, repetitions: 10, rir: 2, clientMutationId: "m-1", createdAt: "2026-09-01T10:05:00Z" }],
      totals: { sets: 1, repetitions: 10, volumeKg: 600 }
    })
  ]);

  const result = await api.getWorkout("token", "w-1");

  assert.equal(result.ok, true);
  assert.equal(result.value.totals.volumeKg, 600);
  assert.equal(result.value.sets[0].exerciseId, "lat-pulldown");
  assert.equal(result.value.endedAt, "2026-09-01T11:00:00Z");
});

test("тренировка без подходов не ломает экран «Итоги»", async () => {
  const { api } = client([json(200, { id: "w-1", title: "Пустая", status: "cancelled", createdAt: "2026-09-01T10:00:00Z" })]);

  const result = await api.getWorkout("token", "w-1");

  assert.equal(result.ok, true);
  assert.deepEqual(result.value.sets, []);
  assert.deepEqual(result.value.totals, { sets: 0, repetitions: 0, volumeKg: 0 });
});

test("недопустимый переход статуса — это 409 с причиной, а не сбой связи", async () => {
  const { api, calls } = client([json(409, { error: { code: "invalid_transition", message: "completed is terminal" } })]);

  const result = await api.setWorkoutStatus("token", "w-1", "active");

  assert.equal(result.ok, false);
  assert.equal(result.error.kind, "client");
  assert.equal(result.error.status, 409);
  assert.equal(result.error.code, "invalid_transition");
  assert.equal(calls.length, 1, "конфликт цикла не ретраится: повтор ничего не изменит");
});

test("смена статуса отправляет ровно один статус в теле", async () => {
  const { api, calls } = client([json(200, { id: "w-1", title: "Тяга", status: "cancelled", createdAt: "2026-09-01T10:00:00Z" })]);

  const result = await api.setWorkoutStatus("token", "w-1", "cancelled");

  assert.equal(result.ok, true);
  assert.equal(result.value.status, "cancelled");
  assert.deepEqual(JSON.parse(calls[0].init.body), { status: "cancelled" });
});

test("прогресс отдаёт все три секции", async () => {
  const { api, calls } = client([
    json(200, {
      window: { from: "2026-06-01T00:00:00Z", to: "2026-09-07T00:00:00Z" },
      strength: [
        {
          exerciseId: "squat",
          sets: 12,
          repetitions: 96,
          volumeKg: 9000,
          bestWeight: { weightKg: 105, repetitions: 9, achievedAt: "2026-09-01T10:00:00Z" },
          bestEstimated1Rm: { estimated1RmKg: 136.5, weightKg: 105, repetitions: 9, achievedAt: "2026-09-01T10:00:00Z" },
          lastPerformedAt: "2026-09-01T10:00:00Z"
        }
      ],
      weeklyVolume: [{ weekStart: "2026-08-31T00:00:00Z", sets: 4, repetitions: 38, volumeKg: 3470, workouts: 1 }],
      adherence: {
        weeks: [{ weekStart: "2026-08-31T00:00:00Z", started: 2, completed: 1, cancelled: 1, inProgress: 0, completionRate: 0.5 }],
        totals: { started: 12, completed: 2, cancelled: 4, inProgress: 6, completionRate: 0.1667, weeksInWindow: 12, weeksWithTraining: 3 }
      }
    })
  ]);

  const result = await api.progress("token", { from: "2026-06-01", exerciseLimit: 10 });

  assert.equal(result.ok, true);
  assert.equal(result.value.strength[0].bestEstimated1Rm.estimated1RmKg, 136.5);
  assert.equal(result.value.weeklyVolume[0].volumeKg, 3470);
  assert.equal(result.value.adherence.totals.weeksWithTraining, 3);
  assert.equal(new URL(calls[0].url).searchParams.get("exerciseLimit"), "10");
});

test("пустой прогресс у нового пользователя — валидные пустые секции", async () => {
  const { api } = client([json(200, { window: { from: "2026-06-01T00:00:00Z", to: "2026-09-07T00:00:00Z" } })]);

  const result = await api.progress("token");

  assert.equal(result.ok, true);
  assert.deepEqual(result.value.strength, []);
  assert.deepEqual(result.value.weeklyVolume, []);
  assert.deepEqual(result.value.adherence.weeks, []);
  assert.equal(result.value.adherence.totals.completionRate, 0);
});

test("выход не требует access-токена и не ретраится", async () => {
  const { api, calls } = client([json(204)]);

  const result = await api.logout("refresh-handle");

  assert.equal(result.ok, true);
  assert.equal(calls[0].init.headers?.authorization, undefined, "выходящий клиент вполне может держать протухший access");
  assert.deepEqual(JSON.parse(calls[0].init.body), { refreshToken: "refresh-handle", allSessions: false });
  assert.equal(calls.length, 1);
});

test("выход со всех устройств идёт под access-токеном", async () => {
  const { api, calls } = client([json(204)]);

  const result = await api.logoutAll("token");

  assert.equal(result.ok, true);
  assert.equal(calls[0].init.headers.authorization, "Bearer token");
});
