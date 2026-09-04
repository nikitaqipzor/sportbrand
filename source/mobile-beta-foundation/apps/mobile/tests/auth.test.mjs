import test from "node:test";
import assert from "node:assert/strict";

import { createAuthClient } from "@athletica/api-client";

import { createMemorySecureStorage } from "../src/platform/storage/secure-storage.ts";
import { createSecureSessionStore, SESSION_STORAGE_KEY } from "../src/platform/storage/session-store.ts";
import {
  registerSessionCleanup,
  resetSessionCleanup,
  runSessionCleanup
} from "../src/features/auth/session-cleanup.ts";
import { authErrorMessage, fieldErrorsFrom } from "../src/features/auth/messages.ts";
import { validateCredentials, validateEmail, validatePassword } from "../src/features/auth/validation.ts";

const config = { environment: "development", baseUrl: "http://api.test/api/v1" };
const json = (status, body) =>
  new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });

const session = {
  user: { id: "user-1", email: "athlete@example.com", createdAt: "2026-09-04T10:00:00Z" },
  accessToken: "access-1",
  tokenType: "Bearer",
  expiresIn: 900,
  refreshToken: "refresh-1",
  refreshExpiresIn: 2592000
};

test("валидация почты и пароля повторяет границы контракта", () => {
  assert.equal(validateEmail("athlete@example.com"), null);
  assert.match(validateEmail(""), /Введите адрес/);
  assert.match(validateEmail("athlete@example"), /некорректно/);
  assert.match(validateEmail(`${"a".repeat(250)}@example.com`), /длиннее 254/);

  assert.equal(validatePassword("correct-horse-battery"), null);
  assert.match(validatePassword("short"), /короче 10/);
  // Кириллица считается байтами: bcrypt ограничен 72 байтами.
  assert.match(validatePassword("я".repeat(37)), /длиннее 72/);
  assert.equal(validatePassword("я".repeat(36)), null);

  assert.deepEqual(validateCredentials({ email: "athlete@example.com", password: "correct-horse-battery" }), {});
  const bad = validateCredentials({ email: "нет", password: "" });
  assert.ok(bad.email && bad.password);
});

test("сообщения об ошибках русские и не содержат текста сервера", () => {
  assert.match(authErrorMessage({ kind: "network", message: "ECONNREFUSED token=abc", attempts: 3 }), /Нет связи/);
  assert.match(
    authErrorMessage({ kind: "client", status: 401, code: "invalid_credentials", message: "invalid", details: [] }),
    /Неверная почта или пароль/
  );
  assert.match(
    authErrorMessage({
      kind: "client",
      status: 429,
      code: "rate_limited",
      message: "too many",
      details: [],
      retryAfterSeconds: 12
    }),
    /через 12 с/
  );
  assert.deepEqual(
    fieldErrorsFrom({
      kind: "client",
      status: 422,
      code: "validation_failed",
      message: "failed",
      details: [{ field: "password", message: "auth: password must be between 10 and 72 bytes" }]
    }),
    { password: "Пароль не соответствует требованиям" }
  );
});

test("защищённое хранилище: сессия кладётся под один ключ и читается обратно", async () => {
  const storage = createMemorySecureStorage();
  const store = createSecureSessionStore(storage);

  assert.equal(await store.load(), null);
  await store.save(session);
  assert.deepEqual(await store.load(), session);
  assert.ok((await storage.getItem(SESSION_STORAGE_KEY)).includes("access-1"));

  await store.clear();
  assert.equal(await store.load(), null);
  assert.equal(await storage.getItem(SESSION_STORAGE_KEY), null);
});

test("битая запись в хранилище не роняет старт, а приводит к экрану входа", async () => {
  const storage = createMemorySecureStorage({ [SESSION_STORAGE_KEY]: "{не json" });
  const store = createSecureSessionStore(storage);
  assert.equal(await store.load(), null);
  assert.equal(await storage.getItem(SESSION_STORAGE_KEY), null);

  const incomplete = createSecureSessionStore(
    createMemorySecureStorage({ [SESSION_STORAGE_KEY]: JSON.stringify({ accessToken: "a" }) })
  );
  assert.equal(await incomplete.load(), null);
});

test("signOut чистит защищённое хранилище и дёргает точку расширения", async () => {
  resetSessionCleanup();
  const storage = createMemorySecureStorage();
  const store = createSecureSessionStore(storage);
  await store.save(session);

  const cleaned = [];
  const unsubscribe = registerSessionCleanup((event) => cleaned.push(event));

  const auth = createAuthClient({ config, store, fetch: async () => json(200, {}), sleep: async () => {} });
  auth.subscribe((event) => {
    if (event.type === "signed_out") void runSessionCleanup({ userId: event.userId, reason: event.reason });
  });

  await auth.restore();
  await auth.signOut();

  assert.equal(auth.getSession(), null);
  assert.equal(await storage.getItem(SESSION_STORAGE_KEY), null, "токены не остаются на устройстве");
  assert.deepEqual(cleaned, [{ userId: "user-1", reason: "user" }]);

  unsubscribe();
  resetSessionCleanup();
});

test("неудачный refresh завершает сессию и запускает очистку с причиной refresh_failed", async () => {
  resetSessionCleanup();
  const storage = createMemorySecureStorage();
  const store = createSecureSessionStore(storage);
  await store.save(session);

  const cleaned = [];
  registerSessionCleanup((event) => cleaned.push(event));

  const auth = createAuthClient({
    config,
    store,
    sleep: async () => {},
    fetch: async (url) =>
      url.endsWith("/auth/refresh")
        ? json(401, { error: { code: "unauthorized", message: "refresh token spent" } })
        : json(401, { error: { code: "unauthorized", message: "access token expired" } })
  });
  auth.subscribe((event) => {
    if (event.type === "signed_out") void runSessionCleanup({ userId: event.userId, reason: event.reason });
  });

  await auth.restore();
  const result = await auth.me();

  assert.equal(result.ok, false);
  assert.equal(result.error.kind, "session_expired");
  assert.equal(await storage.getItem(SESSION_STORAGE_KEY), null);
  assert.deepEqual(cleaned, [{ userId: "user-1", reason: "refresh_failed" }]);
  resetSessionCleanup();
});

test("ошибка обработчика очистки не мешает выходу", async () => {
  resetSessionCleanup();
  const order = [];
  registerSessionCleanup(() => {
    order.push("падает");
    throw new Error("очередь недоступна");
  });
  registerSessionCleanup(() => {
    order.push("второй");
  });
  await runSessionCleanup({ userId: "user-1", reason: "user" });
  assert.deepEqual(order, ["падает", "второй"]);
  resetSessionCleanup();
});
