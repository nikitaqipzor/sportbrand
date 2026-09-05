import test from "node:test";
import assert from "node:assert/strict";

import { createApiClient } from "@athletica/api-client";

import {
  emptyFeed,
  hasMore,
  HISTORY_FILTERS,
  isEmptyHistory,
  loadNextPage,
  mergePage
} from "../src/features/history/workout-feed.ts";

const config = { environment: "development", baseUrl: "http://api.test/api/v1" };

const json = (body) =>
  new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });

/**
 * Сеть замокана на уровне fetch: проверяем ровно то, что уедет на сервер,
 * включая курсор, — а не то, что мы сами себе подсунули мимо клиента.
 */
function loader(pages) {
  const calls = [];
  const fetchMock = async (url, init) => {
    calls.push({ url: new URL(url), init });
    const page = pages[Math.min(calls.length - 1, pages.length - 1)];
    return typeof page === "function" ? page() : json(page);
  };
  const api = createApiClient({ config, fetch: fetchMock, sleep: async () => {} });
  return { calls, load: (query) => api.listWorkouts("token", query) };
}

const workout = (id, createdAt, status = "completed") => ({
  id,
  title: `Тренировка ${id}`,
  status,
  createdAt
});

// Непрозрачный курсор: клиент не имеет права его читать или собирать.
const CURSOR_1 = "eyJjcmVhdGVkQXQiOiIyMDI2LTA5LTAyVDEwOjAwOjAwWiIsImlkIjoidy0yIn0=";
const CURSOR_2 = "eyJjcmVhdGVkQXQiOiIyMDI2LTA5LTAxVDEwOjAwOjAwWiIsImlkIjoidy00In0=";

test("первая страница идёт без курсора, следующая — с курсором сервера нетронутым", async () => {
  const { calls, load } = loader([
    { items: [workout("w-1", "2026-09-03T10:00:00Z"), workout("w-2", "2026-09-02T10:00:00Z")], nextCursor: CURSOR_1 },
    { items: [workout("w-3", "2026-09-01T10:00:00Z")], nextCursor: null }
  ]);

  const first = await loadNextPage(load, emptyFeed(), { limit: 2 });
  const second = await loadNextPage(load, first, { limit: 2 });

  assert.equal(calls[0].url.searchParams.get("cursor"), null, "первая страница не выдумывает курсор");
  assert.equal(calls[1].url.searchParams.get("cursor"), CURSOR_1, "курсор обязан уехать ровно таким, каким пришёл");
  assert.deepEqual(
    second.items.map((item) => item.id),
    ["w-1", "w-2", "w-3"],
    "страницы склеиваются в порядке сервера: новые сверху"
  );
  assert.equal(hasMore(second), false, "страница без nextCursor — последняя");
});

test("пагинация не теряет и не дублирует строки на границе страниц", async () => {
  const { load } = loader([
    { items: [workout("w-1", "2026-09-05T10:00:00Z"), workout("w-2", "2026-09-04T10:00:00Z")], nextCursor: CURSOR_1 },
    // Сервер сдвинул окно: w-2 приехала повторно вместе с новыми строками.
    {
      items: [workout("w-2", "2026-09-04T10:00:00Z"), workout("w-3", "2026-09-03T10:00:00Z"), workout("w-4", "2026-09-02T10:00:00Z")],
      nextCursor: CURSOR_2
    },
    { items: [workout("w-5", "2026-09-01T10:00:00Z")], nextCursor: null }
  ]);

  let feed = emptyFeed();
  for (let page = 0; page < 3; page += 1) feed = await loadNextPage(load, feed);

  assert.deepEqual(feed.items.map((item) => item.id), ["w-1", "w-2", "w-3", "w-4", "w-5"]);
  assert.equal(new Set(feed.items.map((item) => item.id)).size, feed.items.length, "дубль строки — это дубль тренировки на экране");
});

test("исчерпанная лента не ходит в сеть повторно", async () => {
  const { calls, load } = loader([{ items: [workout("w-1", "2026-09-05T10:00:00Z")], nextCursor: null }]);

  const feed = await loadNextPage(load, emptyFeed());
  const again = await loadNextPage(load, feed);

  assert.equal(calls.length, 1, "конец истории — не повод дёргать сервер бесконечно");
  assert.equal(again, feed);
});

test("фильтр по статусу уезжает в запрос повторяемым параметром", async () => {
  const { calls, load } = loader([{ items: [], nextCursor: null }]);
  const unfinished = HISTORY_FILTERS.find((filter) => filter.id === "active");

  await loadNextPage(load, emptyFeed(), { filter: unfinished.statuses, limit: 20 });

  assert.deepEqual(calls[0].url.searchParams.getAll("status"), ["active", "paused"]);
  assert.equal(calls[0].url.searchParams.get("limit"), "20");
});

test("фильтр «все» не подмешивает статусы в запрос", async () => {
  const { calls, load } = loader([{ items: [], nextCursor: null }]);
  const all = HISTORY_FILTERS.find((filter) => filter.id === "all");

  await loadNextPage(load, emptyFeed(), { filter: all.statuses });

  assert.deepEqual(calls[0].url.searchParams.getAll("status"), []);
});

test("пустая история нового аккаунта — нормальное состояние, а не ошибка", async () => {
  const { load } = loader([{ items: [], nextCursor: null }]);

  const feed = await loadNextPage(load, emptyFeed());

  assert.equal(feed.error, null);
  assert.equal(isEmptyHistory(feed), true);
  assert.equal(isEmptyHistory(emptyFeed()), false, "ещё не загруженная лента — не пустая история");
});

test("сбой связи не стирает уже показанные строки и не двигает курсор", async () => {
  const { load } = loader([
    { items: [workout("w-1", "2026-09-05T10:00:00Z")], nextCursor: CURSOR_1 },
    () => {
      throw new TypeError("network down");
    }
  ]);

  const first = await loadNextPage(load, emptyFeed());
  const failed = await loadNextPage(load, first);

  assert.ok(failed.error, "экран обязан показать причину");
  assert.deepEqual(failed.items.map((item) => item.id), ["w-1"], "потеря секунды связи не должна опустошать экран");
  assert.equal(failed.cursor, CURSOR_1, "курсор остаётся тем же: повтор продолжит с того же места");
});

test("склейка страницы поверх ленты не зависит от сети", () => {
  const feed = mergePage(emptyFeed(), { items: [workout("w-1", "2026-09-05T10:00:00Z")], nextCursor: CURSOR_1 });
  assert.equal(feed.loaded, true);
  assert.equal(feed.cursor, CURSOR_1);
  assert.equal(hasMore(feed), true);
});
