import test from "node:test";
import assert from "node:assert/strict";

import { backoffDelayMs, createApiClient, createHttpClient, isRetryable } from "../src/index.ts";

const config = { environment: "development", baseUrl: "http://api.test/api/v1" };

const json = (status, body, headers = {}) =>
  new Response(body === undefined ? "" : JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", ...headers }
  });

/** fetch-заглушка: отдаёт заранее подготовленные ответы и считает вызовы. */
function stubFetch(responses) {
  const calls = [];
  const fetchMock = async (url, init) => {
    calls.push({ url, init });
    const next = responses[Math.min(calls.length - 1, responses.length - 1)];
    return typeof next === "function" ? next(url, init) : next;
  };
  return { fetchMock, calls };
}

const recordingSleep = (log) => async (ms) => {
  log.push(ms);
};

test("backoffDelayMs: экспонента, потолок и джиттер", () => {
  const policy = { maxAttempts: 5, baseDelayMs: 300, maxDelayMs: 1000, factor: 2 };
  assert.deepEqual(
    [1, 2, 3, 4].map((n) => backoffDelayMs(n, policy, () => 1)),
    [300, 600, 1000, 1000]
  );
  assert.equal(backoffDelayMs(3, policy, () => 0), 0);
  assert.equal(backoffDelayMs(1, policy, () => 0.5), 150);
});

test("5xx ретраится до потолка попыток, с растущей задержкой", async () => {
  const { fetchMock, calls } = stubFetch([() => json(503, { error: { code: "internal_error", message: "down" } })]);
  const delays = [];
  const http = createHttpClient({
    config,
    fetch: fetchMock,
    sleep: recordingSleep(delays),
    random: () => 1,
    retryPolicy: { maxAttempts: 3, baseDelayMs: 300, maxDelayMs: 500, factor: 2 }
  });

  const result = await http.request({ method: "GET", path: "/auth/me" });

  assert.equal(result.ok, false);
  assert.equal(result.error.kind, "server");
  assert.equal(result.error.status, 503);
  assert.equal(result.error.attempts, 3);
  assert.equal(calls.length, 3, "ровно maxAttempts запросов");
  assert.deepEqual(delays, [300, 500], "вторая задержка упёрлась в потолок");
  assert.equal(isRetryable(result.error), true);
});

test("сетевая ошибка ретраится и остаётся типизированной", async () => {
  let calls = 0;
  const http = createHttpClient({
    config,
    fetch: async () => {
      calls += 1;
      if (calls < 3) throw new TypeError("Network request failed");
      return json(200, { status: "ok", database: "up", time: "2026-09-04T10:00:00Z" });
    },
    sleep: async () => {},
    random: () => 0
  });

  const result = await http.request({ method: "GET", path: "/health" });
  assert.equal(result.ok, true);
  assert.equal(calls, 3);
});

test("4xx не ретраится никогда", async () => {
  for (const status of [400, 401, 404, 409, 422, 429]) {
    const { fetchMock, calls } = stubFetch([
      () => json(status, { error: { code: "validation_failed", message: "нет" } }, { "retry-after": "7" })
    ]);
    const http = createHttpClient({ config, fetch: fetchMock, sleep: async () => {} });
    const result = await http.request({ method: "POST", path: "/auth/login", body: {} });
    assert.equal(result.ok, false);
    assert.equal(result.error.kind, "client");
    assert.equal(result.error.status, status);
    assert.equal(calls.length, 1, `${status} не должен ретраиться`);
    assert.equal(isRetryable(result.error), false);
    assert.equal(result.error.retryAfterSeconds, 7);
  }
});

test("таймаут прерывает запрос и возвращает kind: timeout", async () => {
  const hanging = (url, init) =>
    new Promise((_resolve, reject) => {
      init.signal.addEventListener("abort", () => reject(new Error("The operation was aborted")));
    });
  const http = createHttpClient({
    config,
    fetch: hanging,
    sleep: async () => {},
    timeoutMs: 25,
    retryPolicy: { maxAttempts: 2, baseDelayMs: 1, maxDelayMs: 1, factor: 2 }
  });

  const result = await http.request({ method: "GET", path: "/auth/me" });
  assert.equal(result.ok, false);
  assert.equal(result.error.kind, "timeout");
  assert.equal(result.error.timeoutMs, 25);
  assert.equal(result.error.attempts, 2);
});

test("logSet: 201 — созданный подход", async () => {
  const set = {
    id: "11111111-1111-4111-8111-111111111111",
    workoutId: "22222222-2222-4222-8222-222222222222",
    exerciseId: "lat-pulldown",
    setNumber: 2,
    weightKg: 62.5,
    repetitions: 10,
    rir: 2,
    clientMutationId: "outbox-1",
    createdAt: "2026-09-04T10:00:00Z"
  };
  const { fetchMock, calls } = stubFetch([json(201, set)]);
  const api = createApiClient({ config, fetch: fetchMock, sleep: async () => {} });

  const result = await api.logSet("access-token", set.workoutId, {
    exerciseId: set.exerciseId,
    setNumber: 2,
    weightKg: 62.5,
    repetitions: 10,
    rir: 2,
    clientMutationId: "outbox-1"
  });

  assert.equal(result.ok, true);
  assert.equal(result.value.outcome, "created");
  assert.deepEqual(result.value.set, set);
  assert.equal(calls[0].init.headers.authorization, "Bearer access-token");
  assert.equal(calls[0].url, `${config.baseUrl}/workouts/${set.workoutId}/sets`);
});

test("logSet: 409 — успешный исход, а не ошибка (очередь снимает элемент)", async () => {
  const stored = {
    id: "11111111-1111-4111-8111-111111111111",
    workoutId: "22222222-2222-4222-8222-222222222222",
    exerciseId: "lat-pulldown",
    setNumber: 2,
    weightKg: 62.5,
    repetitions: 10,
    rir: 2,
    clientMutationId: "outbox-1",
    createdAt: "2026-09-04T10:00:00Z"
  };
  const { fetchMock, calls } = stubFetch([
    json(409, { error: { code: "duplicate_client_mutation", message: "duplicate" }, set: stored })
  ]);
  const api = createApiClient({ config, fetch: fetchMock, sleep: async () => {} });

  const result = await api.logSet("access-token", stored.workoutId, {
    exerciseId: "lat-pulldown",
    setNumber: 2,
    weightKg: 62.5,
    repetitions: 10,
    rir: 2,
    clientMutationId: "outbox-1"
  });

  assert.equal(result.ok, true);
  assert.equal(result.value.outcome, "duplicate");
  assert.deepEqual(result.value.set, stored);
  assert.equal(calls.length, 1, "409 — окончательный ответ, повторов нет");
});

test("health: 503 приходит как заполненное тело, а не как сбой", async () => {
  const body = { status: "degraded", database: "down", time: "2026-09-04T10:00:00Z" };
  const { fetchMock, calls } = stubFetch([json(503, body)]);
  const api = createApiClient({ config, fetch: fetchMock, sleep: async () => {} });

  const result = await api.health();
  assert.equal(result.ok, true);
  assert.deepEqual(result.value, body);
  assert.equal(calls.length, 1);
});

test("listSets возвращает массив, а 404 остаётся клиентской ошибкой", async () => {
  const { fetchMock } = stubFetch([json(200, { items: [{ id: "s1" }] })]);
  const api = createApiClient({ config, fetch: fetchMock, sleep: async () => {} });
  const listed = await api.listSets("token", "w1");
  assert.equal(listed.ok, true);
  assert.equal(listed.value.length, 1);

  const missing = createApiClient({
    config,
    sleep: async () => {},
    fetch: async () => json(404, { error: { code: "not_found", message: "workout not found" } })
  });
  const result = await missing.listSets("token", "w1");
  assert.equal(result.ok, false);
  assert.equal(result.error.kind, "client");
  assert.equal(result.error.code, "not_found");
});
