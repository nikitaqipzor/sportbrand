import test from "node:test";
import assert from "node:assert/strict";

import {
  createExerciseCacheMemoryDb,
  createMemoryExerciseCache,
  matches,
  toCached
} from "../src/features/catalog/exercise-cache.ts";
import { EXERCISE_CATALOG } from "../src/features/workout/exercise-catalog.ts";

const summary = (id, nameRu, over = {}) => ({
  id,
  slug: id,
  legacyNumber: null,
  nameRu,
  nameEn: id,
  aliases: [],
  variantOf: null,
  sport: "strength",
  section: "legs",
  category: null,
  movementPattern: null,
  difficulty: null,
  laterality: null,
  equipment: ["barbell"],
  primaryMuscles: [],
  secondaryMuscles: [],
  joints: [],
  goalTags: [],
  contentVersion: 1,
  revision: 1,
  updatedAt: "2026-09-06T00:00:00Z",
  hasTechnique: false,
  hasSafety: false,
  ...over
});

test("каталог переживает перезапуск: выбор упражнения работает без связи", async () => {
  const db = createExerciseCacheMemoryDb();
  await createMemoryExerciseCache(db).put([summary("back-squat", "Приседания со штангой")]);

  const offline = createMemoryExerciseCache(db);
  const options = await offline.list();

  assert.equal(options.length, 1, "в зале связи может не быть, а упражнение выбрать нужно");
  assert.equal(options[0].title, "Приседания со штангой");
});

test("повторная загрузка обновляет запись, а не добавляет вторую", async () => {
  const db = createExerciseCacheMemoryDb();
  const cache = createMemoryExerciseCache(db);
  await cache.put([summary("back-squat", "Приседания")]);
  await cache.put([summary("back-squat", "Приседания со штангой")]);

  const options = await cache.list();

  assert.equal(options.length, 1, "идентификатор неизменяем — это та же запись");
  assert.equal(options[0].title, "Приседания со штангой");
});

test("поиск не зависит от регистра", async () => {
  const cache = createMemoryExerciseCache();
  await cache.put([summary("back-squat", "Приседания со штангой"), summary("bench-press", "Жим лёжа")]);

  assert.deepEqual((await cache.list("присед")).map((e) => e.id), ["back-squat"]);
  assert.deepEqual((await cache.list("ПРИСЕД")).map((e) => e.id), ["back-squat"]);
  assert.equal((await cache.list("")).length, 2, "пустой запрос — весь каталог");
});

test("пустой запрос и пробелы не отсеивают ничего", () => {
  const item = { id: "x", title: "Жим лёжа", section: "", equipment: "" };
  assert.equal(matches(item, ""), true);
  assert.equal(matches(item, "   "), true);
  assert.equal(matches(item, "приседания"), false);
});

test("в кеш попадают именно те поля, что нужны выбору", () => {
  const cached = toCached(summary("dip", "Отжимания на брусьях", { equipment: ["bars", "bodyweight"] }));
  assert.deepEqual(cached, { id: "dip", title: "Отжимания на брусьях", section: "legs", equipment: "bars, bodyweight" });
});

test("запасной список несёт валидные идентификаторы каталога", () => {
  // Подход, записанный до первой загрузки справочника, обязан ссылаться на то
  // же упражнение, что и после неё: идентификатор уезжает в clientMutationId.
  assert.ok(EXERCISE_CATALOG.length >= 20);
  for (const entry of EXERCISE_CATALOG) {
    assert.match(entry.id, /^[a-z0-9]+(-[a-z0-9]+)*$/, `${entry.id} обязан быть валидным идентификатором каталога`);
    assert.ok(entry.title.length > 0);
  }
});
