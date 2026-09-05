import type { ApiError, ApiResult, Workout, WorkoutListQuery, WorkoutPage, WorkoutStatus } from "@athletica/api-client";

/**
 * Лента истории тренировок: чистая, без React и без сети, чтобы правила
 * пагинации можно было проверить тестом, а не руками в приложении.
 *
 * Два правила, ради которых это отдельный модуль:
 *  1. курсор непрозрачен — он берётся из ответа и возвращается нетронутым,
 *     клиент никогда не собирает свой и не пытается его прочитать;
 *  2. страницы склеиваются без потерь и без дублей — строка, пришедшая
 *     дважды (сервер мог сдвинуть окно), не удваивается в списке.
 */

/** null — «все статусы»; пустой массив означал бы «ни одного». */
export type HistoryFilter = readonly WorkoutStatus[] | null;

export type HistoryFeed = {
  items: Workout[];
  /** Курсор следующей страницы, как его прислал сервер. */
  cursor: string | null;
  /** Сервер отдал последнюю страницу — просить дальше нечего. */
  exhausted: boolean;
  /** Загружена ли хотя бы одна страница: пустой список ≠ «ещё не грузили». */
  loaded: boolean;
  error: ApiError | null;
};

export const emptyFeed = (): HistoryFeed => ({
  items: [],
  cursor: null,
  exhausted: false,
  loaded: false,
  error: null
});

/** Есть ли смысл просить следующую страницу. */
export const hasMore = (feed: HistoryFeed): boolean => !feed.exhausted;

/**
 * Пустой список после загрузки — нормальное состояние нового аккаунта,
 * а не ошибка: в нём просто ещё не было ни одной тренировки.
 */
export const isEmptyHistory = (feed: HistoryFeed): boolean =>
  feed.loaded && feed.error === null && feed.items.length === 0;

export function mergePage(feed: HistoryFeed, page: WorkoutPage): HistoryFeed {
  const seen = new Set(feed.items.map((item) => item.id));
  const items = [...feed.items];
  for (const workout of page.items) {
    // Повтор строки на границе страниц — не повод показать её дважды.
    if (seen.has(workout.id)) continue;
    seen.add(workout.id);
    items.push(workout);
  }
  return {
    items,
    cursor: page.nextCursor,
    exhausted: page.nextCursor === null,
    loaded: true,
    error: null
  };
}

export type PageLoader = (query: WorkoutListQuery) => Promise<ApiResult<WorkoutPage>>;

export type LoadPageOptions = { filter?: HistoryFilter; limit?: number };

/**
 * Следующая страница поверх текущей ленты. Исчерпанная лента не ходит в сеть
 * повторно, а ошибка не стирает уже показанные строки — иначе одна потерянная
 * секунда связи опустошала бы экран.
 */
export async function loadNextPage(
  load: PageLoader,
  feed: HistoryFeed,
  options: LoadPageOptions = {}
): Promise<HistoryFeed> {
  if (feed.exhausted) return feed;
  const filter = options.filter;
  const result = await load({
    // Курсор уходит ровно таким, каким пришёл: разбирать и собирать его
    // клиенту нельзя, это внутреннее дело сервера.
    ...(feed.cursor === null ? {} : { cursor: feed.cursor }),
    ...(filter && filter.length > 0 ? { status: [...filter] } : {}),
    ...(options.limit === undefined ? {} : { limit: options.limit })
  });
  if (!result.ok) return { ...feed, error: result.error };
  return mergePage(feed, result.value);
}

const STATUS_LABEL: Record<WorkoutStatus, string> = {
  active: "идёт",
  paused: "на паузе",
  completed: "завершена",
  cancelled: "отменена"
};

export const statusLabel = (status: WorkoutStatus): string => STATUS_LABEL[status] ?? status;

export const HISTORY_FILTERS: readonly { id: string; title: string; statuses: HistoryFilter }[] = [
  { id: "all", title: "Все", statuses: null },
  { id: "completed", title: "Завершённые", statuses: ["completed"] },
  { id: "active", title: "Незавершённые", statuses: ["active", "paused"] },
  { id: "cancelled", title: "Отменённые", statuses: ["cancelled"] }
];
