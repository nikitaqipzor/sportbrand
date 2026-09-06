import test from "node:test";
import assert from "node:assert/strict";

import { createApiClient } from "../src/index.ts";

const config = { environment: "development", baseUrl: "http://api.test/api/v1" };

const json = (status, body) =>
  new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" }
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

const summary = (over = {}) => ({
  id: "back-squat",
  slug: "back-squat",
  legacyNumber: null,
  nameRu: "Приседания со штангой",
  nameEn: "Back squat",
  aliases: [],
  variantOf: null,
  sport: "strength",
  section: "legs",
  category: null,
  movementPattern: "squat",
  difficulty: null,
  laterality: "bilateral",
  equipment: ["barbell"],
  primaryMuscles: ["quadriceps"],
  secondaryMuscles: ["glutes"],
  joints: [],
  goalTags: [],
  contentVersion: 1,
  revision: 1,
  updatedAt: "2026-09-06T00:00:00Z",
  hasTechnique: false,
  hasSafety: false,
  ...over
});

test("фильтры и поиск уезжают в строку запроса", async () => {
  const { api, calls } = client([json(200, { items: [], nextCursor: null })]);

  await api.listExercises("token", { section: "back", equipment: "cable", muscle: "lats", q: "тяга", limit: 30 });

  const url = new URL(calls[0].url);
  assert.equal(url.pathname, "/api/v1/exercises");
  assert.equal(url.searchParams.get("section"), "back");
  assert.equal(url.searchParams.get("equipment"), "cable");
  assert.equal(url.searchParams.get("q"), "тяга");
  assert.equal(calls[0].init.headers.authorization, "Bearer token");
});

test("курсор каталога возвращается серверу нетронутым", async () => {
  const opaque = "AAECAwQFBgc=";
  const { api, calls } = client([json(200, { items: [], nextCursor: null })]);

  await api.listExercises("token", { cursor: opaque });

  assert.equal(new URL(calls[0].url).searchParams.get("cursor"), opaque);
});

test("страница без курсора читается как последняя", async () => {
  const { api } = client([json(200, { items: [summary()] })]);

  const result = await api.listExercises("token");

  assert.equal(result.ok, true);
  assert.equal(result.value.nextCursor, null);
  assert.equal(result.value.items[0].id, "back-squat");
});

test("незаявленный уровень остаётся null и не превращается в «новичка»", async () => {
  const { api } = client([json(200, { items: [summary({ difficulty: null })], nextCursor: null })]);

  const result = await api.listExercises("token");

  assert.equal(result.value.items[0].difficulty, null, "подстановка уровня была бы выдумкой");
});

test("карточка отдаёт пустые блоки техники и безопасности как есть", async () => {
  const { api } = client([
    json(200, {
      ...summary(),
      technique: { setup: "", startPosition: "", executionSteps: [], keyCues: [], breathing: "", tempo: "", rangeOfMotion: "", finishReturn: "" },
      programming: {},
      safety: { commonErrors: [], stopSigns: [], contraindications: [], regressions: [], progressions: [], injuryNotes: "" },
      media: {},
      publicationStatus: "published",
      reviewStatus: "approved",
      mediaStatus: "approved",
      contentLocale: "ru-RU",
      schemaVersion: 1
    })
  ]);

  const result = await api.getExercise("token", "back-squat");

  assert.equal(result.ok, true);
  assert.equal(result.value.hasTechnique, false);
  assert.deepEqual(result.value.technique.executionSteps, []);
  assert.deepEqual(result.value.safety.contraindications, []);
});

test("неопубликованное упражнение неотличимо от несуществующего", async () => {
  const { api } = client([json(404, { error: { code: "not_found", message: "exercise not found" } })]);

  const result = await api.getExercise("token", "hidden-lift");

  assert.equal(result.ok, false);
  assert.equal(result.error.status, 404);
});

test("справочники отдаются целиком, включая пустые", async () => {
  const { api } = client([
    json(200, {
      dictionaries: [
        { kind: "section", items: [{ code: "legs", nameRu: "Ноги", nameEn: "Legs", sortOrder: 1 }] },
        { kind: "joint", items: [] }
      ]
    })
  ]);

  const result = await api.exerciseDictionaries("token");

  assert.equal(result.ok, true);
  assert.equal(result.value.length, 2);
  const joints = result.value.find((d) => d.kind === "joint");
  assert.deepEqual(joints.items, [], "пустой справочник — не то же самое, что отсутствующий фильтр");
});

test("сломанный ответ справочников не роняет экран", async () => {
  const { api } = client([json(200, {})]);

  const result = await api.exerciseDictionaries("token");

  assert.equal(result.ok, true);
  assert.deepEqual(result.value, []);
});
