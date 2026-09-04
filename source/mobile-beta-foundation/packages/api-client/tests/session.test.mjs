import test from "node:test";
import assert from "node:assert/strict";

import { createAuthClient } from "../src/index.ts";

const config = { environment: "development", baseUrl: "http://api.test/api/v1" };

const json = (status, body) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const user = { id: "user-1", email: "athlete@example.com", createdAt: "2026-09-04T10:00:00Z" };

const sessionWith = (accessToken, refreshToken) => ({
  user,
  accessToken,
  tokenType: "Bearer",
  expiresIn: 900,
  refreshToken,
  refreshExpiresIn: 2592000
});

/** Хранилище в памяти вместо expo-secure-store. */
function memoryStore(initial = null) {
  let value = initial;
  const calls = { save: 0, clear: 0, load: 0 };
  return {
    calls,
    peek: () => value,
    load: async () => {
      calls.load += 1;
      return value;
    },
    save: async (session) => {
      calls.save += 1;
      value = session;
    },
    clear: async () => {
      calls.clear += 1;
      value = null;
    }
  };
}

const unauthorized = () => json(401, { error: { code: "unauthorized", message: "access token expired" } });

test("несколько одновременных 401 обновляют токен ровно один раз", async () => {
  const store = memoryStore();
  let refreshCalls = 0;
  const meCalls = [];

  const fetchMock = async (url, init) => {
    if (url.endsWith("/auth/refresh")) {
      refreshCalls += 1;
      // Обновление отвечает не мгновенно — все параллельные запросы успевают
      // упереться в 401 и встать в очередь за одним и тем же промисом.
      await new Promise((resolve) => setTimeout(resolve, 10));
      return json(200, sessionWith("access-2", "refresh-2"));
    }
    const token = init.headers.authorization;
    meCalls.push(token);
    return token === "Bearer access-2" ? json(200, user) : unauthorized();
  };

  const auth = createAuthClient({ config, store, fetch: fetchMock, sleep: async () => {} });
  const events = [];
  auth.subscribe((event) => events.push(event.type));

  await store.save(sessionWith("access-1", "refresh-1"));
  await auth.restore();

  const results = await Promise.all([auth.me(), auth.me(), auth.me(), auth.me(), auth.me()]);

  assert.equal(refreshCalls, 1, "refresh вызван ровно один раз на пять параллельных 401");
  assert.equal(
    results.every((r) => r.ok === true),
    true,
    "все запросы повторены с новым токеном"
  );
  assert.equal(meCalls.filter((t) => t === "Bearer access-1").length, 5);
  assert.equal(meCalls.filter((t) => t === "Bearer access-2").length, 5);
  assert.equal(auth.getSession().accessToken, "access-2");
  assert.equal(store.peek().accessToken, "access-2");
  assert.equal(events.filter((e) => e === "refreshed").length, 1, "одно событие refreshed");
});

test("после успешного обновления следующий 401 обновляет токен снова", async () => {
  const store = memoryStore(sessionWith("access-1", "refresh-1"));
  let refreshCalls = 0;
  let generation = 2; // сервер уже выпустил новое поколение: сохранённый access-1 просрочен

  const auth = createAuthClient({
    config,
    store,
    sleep: async () => {},
    fetch: async (url, init) => {
      if (url.endsWith("/auth/refresh")) {
        refreshCalls += 1;
        generation += 1;
        return json(200, sessionWith(`access-${generation}`, `refresh-${generation}`));
      }
      return init.headers.authorization === `Bearer access-${generation}` ? json(200, user) : unauthorized();
    }
  });

  await auth.restore();
  assert.equal((await auth.me()).ok, true);
  assert.equal(refreshCalls, 1);

  generation += 1; // сервер снова считает наш токен просроченным
  assert.equal((await auth.me()).ok, true);
  assert.equal(refreshCalls, 2);
});

test("неудачный refresh завершает сессию наблюдаемым событием и чистит хранилище", async () => {
  const store = memoryStore(sessionWith("access-1", "refresh-1"));
  const events = [];

  const auth = createAuthClient({
    config,
    store,
    sleep: async () => {},
    fetch: async (url) =>
      url.endsWith("/auth/refresh")
        ? json(401, { error: { code: "unauthorized", message: "refresh token spent" } })
        : unauthorized()
  });
  auth.subscribe((event) => events.push(event));

  await auth.restore();
  const results = await Promise.all([auth.me(), auth.listSets("w1")]);

  for (const result of results) {
    assert.equal(result.ok, false);
    assert.equal(result.error.kind, "session_expired");
  }
  assert.equal(auth.getSession(), null);
  assert.equal(store.peek(), null);
  assert.equal(store.calls.clear, 1);

  const signedOut = events.filter((e) => e.type === "signed_out");
  assert.equal(signedOut.length, 1);
  assert.equal(signedOut[0].reason, "refresh_failed");
  assert.equal(signedOut[0].userId, "user-1");
  assert.equal(signedOut[0].error.kind, "client");
});

test("signOut завершает сессию и сообщает подписчикам причину user", async () => {
  const store = memoryStore(sessionWith("access-1", "refresh-1"));
  const events = [];
  const auth = createAuthClient({ config, store, sleep: async () => {}, fetch: async () => json(200, user) });
  auth.subscribe((event) => events.push(event));

  await auth.restore();
  await auth.signOut();

  assert.equal(auth.getSession(), null);
  assert.equal(store.peek(), null);
  assert.deepEqual(
    events.map((e) => e.type),
    ["signed_in", "signed_out"]
  );
  assert.equal(events[1].reason, "user");
  assert.equal(events[1].userId, "user-1");

  const afterSignOut = await auth.me();
  assert.equal(afterSignOut.ok, false);
  assert.equal(afterSignOut.error.kind, "session_expired");
});

test("вход сохраняет сессию в хранилище, ошибка входа — типизированная", async () => {
  const store = memoryStore();
  const auth = createAuthClient({
    config,
    store,
    sleep: async () => {},
    fetch: async (_url, init) => {
      const body = JSON.parse(init.body);
      return body.password === "correct-horse-battery"
        ? json(200, sessionWith("access-1", "refresh-1"))
        : json(401, { error: { code: "invalid_credentials", message: "invalid email or password" } });
    }
  });

  const bad = await auth.signIn({ email: user.email, password: "wrong-password-1" });
  assert.equal(bad.ok, false);
  assert.equal(bad.error.kind, "client");
  assert.equal(bad.error.code, "invalid_credentials");
  assert.equal(store.calls.save, 0);
  assert.equal(auth.getSession(), null);

  const good = await auth.signIn({ email: user.email, password: "correct-horse-battery" });
  assert.equal(good.ok, true);
  assert.equal(store.peek().accessToken, "access-1");
  assert.equal(auth.getSession().user.id, "user-1");
});

test("токены не утекают в текст ошибок", async () => {
  const store = memoryStore(sessionWith("access-token-super-secret", "refresh-token-super-secret"));
  const auth = createAuthClient({
    config,
    store,
    sleep: async () => {},
    retryPolicy: { maxAttempts: 1, baseDelayMs: 1, maxDelayMs: 1, factor: 2 },
    fetch: async (url, init) => {
      if (url.endsWith("/auth/refresh")) {
        // Сервер эхом вернул присланный refresh-токен в тексте ошибки.
        const body = JSON.parse(init.body);
        return json(500, { error: { code: "internal_error", message: `upstream failed for ${body.refreshToken}` } });
      }
      throw new Error(`connect ECONNREFUSED with header ${init.headers.authorization}`);
    }
  });

  await auth.restore();
  const network = await auth.me();
  assert.equal(network.ok, false);
  const networkText = JSON.stringify(network.error);
  assert.equal(networkText.includes("access-token-super-secret"), false, networkText);
  assert.equal(networkText.includes("[redacted]"), true);

  const refreshResult = await auth.refreshNow();
  assert.equal(refreshResult.ok, false);
  assert.equal(refreshResult.error.kind, "session_expired");
  const refreshText = JSON.stringify(refreshResult.error);
  assert.equal(refreshText.includes("refresh-token-super-secret"), false, refreshText);
});
