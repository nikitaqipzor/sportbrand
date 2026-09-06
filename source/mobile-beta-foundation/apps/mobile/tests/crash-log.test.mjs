import test from "node:test";
import assert from "node:assert/strict";

import {
  appendCrash,
  CRASH_LOG_LIMIT,
  createCrashReporter,
  createMemoryCrashStore,
  createCrashMemoryDb,
  redactCrashText,
  toCrashRecord
} from "../src/platform/diagnostics/crash-log.ts";

const AT = new Date("2026-09-06T12:00:00.000Z");

test("токены не попадают в журнал падений", () => {
  const jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyLTEifQ.s3cr3tsignatureVALUE1234";
  const text = `Ошибка запроса с Authorization: Bearer ${jwt}`;

  const redacted = redactCrashText(text);

  assert.ok(!redacted.includes(jwt), "отчёт о падении — самое соблазнительное место для утечки токена");
  assert.ok(!redacted.includes("s3cr3tsignature"));
});

test("адрес почты не попадает в журнал", () => {
  const redacted = redactCrashText("не удалось войти как nikita@example.com");
  assert.ok(!redacted.includes("nikita@example.com"));
  assert.ok(redacted.includes("[почта]"));
});

test("длинный refresh-токен вырезается по форме, а не по значению", () => {
  const handle = "Zm9vYmFyYmF6cXV4Y29ycmVjdGhvcnNlYmF0dGVyeXN0YXBsZTEyMzQ1Njc4OTA";
  const redacted = redactCrashText(`refresh failed for ${handle}`);
  assert.ok(!redacted.includes(handle), "в момент падения самого токена под рукой может не быть");
});

test("обычный текст ошибки не портится", () => {
  const text = "Не удалось открыть экран тренировки";
  assert.equal(redactCrashText(text), text);
});

test("запись падения несёт область, сообщение и обрезанный стек", () => {
  const error = new Error("boom");
  error.stack = ["Error: boom", ...Array.from({ length: 40 }, (_, i) => `    at frame${i}`)].join("\n");

  const record = toCrashRecord("workout", error, AT, "c-1");

  assert.equal(record.scope, "workout");
  assert.equal(record.message, "boom");
  assert.equal(record.at, AT.toISOString());
  assert.ok(record.stack.split("\n").length <= 12, "длинный хвост стека ничего не добавляет");
});

test("не-Error тоже записывается, а не теряется", () => {
  const record = toCrashRecord("sync", "просто строка", AT, "c-2");
  assert.equal(record.message, "просто строка");
});

test("журнал не растёт без границ", () => {
  let log = [];
  for (let i = 0; i < CRASH_LOG_LIMIT + 10; i += 1) {
    log = appendCrash(log, toCrashRecord("scope", new Error(`e${i}`), AT, `c-${i}`));
  }
  assert.equal(log.length, CRASH_LOG_LIMIT, "телефон не место для бесконечного лога");
  assert.equal(log[0].message, `e${CRASH_LOG_LIMIT + 9}`, "новые записи впереди");
});

test("падения переживают перезапуск: журнал на «диске», а не в памяти", async () => {
  const db = createCrashMemoryDb();
  const first = createCrashReporter({ store: createMemoryCrashStore(db), now: () => AT, id: () => "c-1" });
  await first.capture("workout", new Error("boom"));

  // Приложение упало и запустилось заново.
  const revived = createCrashReporter({ store: createMemoryCrashStore(db), now: () => AT, id: () => "c-2" });
  const records = await revived.recent();

  assert.equal(records.length, 1, "журнал нужен именно после перезапуска — приложение уже упало");
  assert.equal(records[0].message, "boom");
});

test("падение при записи падения не роняет приложение второй раз", async () => {
  const broken = {
    list: async () => [],
    append: async () => {
      throw new Error("диск недоступен");
    },
    clear: async () => {}
  };
  const reporter = createCrashReporter({ store: broken, now: () => AT });

  await reporter.capture("root", new Error("исходная ошибка"));
  // Дошли сюда — значит вторая ошибка не всплыла наружу.
  assert.ok(true);
});

test("журнал очищается по требованию", async () => {
  const reporter = createCrashReporter({ store: createMemoryCrashStore(), now: () => AT });
  await reporter.capture("root", new Error("boom"));
  assert.equal((await reporter.recent()).length, 1);

  await reporter.clear();
  assert.equal((await reporter.recent()).length, 0);
});
