import type { ApiError, Progress, Workout, WorkoutDetail } from "@athletica/api-client";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { useAuth } from "../auth/auth-context.tsx";
import {
  emptyFeed,
  hasMore,
  HISTORY_FILTERS,
  isEmptyHistory,
  loadNextPage,
  type HistoryFeed
} from "./workout-feed.ts";

/**
 * Загрузка «только для чтения»: сервер — источник истины для итогов и
 * прогресса. Локальная очередь их не подменяет, иначе экран показывал бы
 * подходы, которых на сервере ещё нет, и цифры разъезжались бы после синка.
 */
export type RemoteState<T> = {
  data: T | null;
  error: ApiError | null;
  loading: boolean;
  reload: () => Promise<void>;
};

function useRemote<T>(load: (() => Promise<{ ok: true; value: T } | { ok: false; error: ApiError }>) | null): RemoteState<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [loading, setLoading] = useState(true);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const reload = useCallback(async (): Promise<void> => {
    if (!load) {
      setData(null);
      setError(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    const result = await load();
    if (!alive.current) return;
    if (result.ok) {
      setData(result.value);
      setError(null);
    } else {
      setError(result.error);
    }
    setLoading(false);
  }, [load]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { data, error, loading, reload };
}

/** Итоги одной тренировки: она сама, её подходы и суммы. */
export function useWorkoutSummary(workoutId: string | undefined): RemoteState<WorkoutDetail> {
  const { client, session } = useAuth();
  const ready = Boolean(session && workoutId);
  const load = useCallback(
    () => client.getWorkout(workoutId as string),
    [client, workoutId]
  );
  return useRemote<WorkoutDetail>(ready ? load : null);
}

/** Прогресс за окно: рекорды, недельный объём и соблюдение. */
export function useProgress(): RemoteState<Progress> {
  const { client, session } = useAuth();
  const load = useCallback(() => client.progress(), [client]);
  return useRemote<Progress>(session ? load : null);
}

/**
 * История тренировок с курсорной пагинацией.
 *
 * Вся арифметика страниц живёт в workout-feed.ts и проверена тестами; здесь
 * только React: одна загрузка за раз (иначе две подгрузки взяли бы один и тот
 * же курсор и строки удвоились бы) и сброс ленты при смене фильтра.
 */
export type WorkoutHistoryView = {
  items: Workout[];
  /** Первая страница ещё не пришла. */
  loading: boolean;
  /** Идёт подгрузка следующей страницы поверх уже показанных строк. */
  loadingMore: boolean;
  error: ApiError | null;
  hasMore: boolean;
  /** Пустая история нового аккаунта — не ошибка. */
  empty: boolean;
  filterId: string;
  setFilterId: (id: string) => void;
  loadMore: () => Promise<void>;
  reload: () => Promise<void>;
};

export const HISTORY_PAGE_SIZE = 20;

export function useWorkoutHistory(): WorkoutHistoryView {
  const { client, session } = useAuth();
  const [filterId, setFilterId] = useState<string>("all");
  const [feed, setFeed] = useState<HistoryFeed>(emptyFeed);
  const [busy, setBusy] = useState(false);
  const alive = useRef(true);
  // Курсор берётся из последнего состояния, а не из замыкания: иначе повторный
  // вызов во время загрузки ушёл бы со старым курсором и продублировал строки.
  const inflight = useRef(false);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const statuses = useMemo(
    () => HISTORY_FILTERS.find((entry) => entry.id === filterId)?.statuses ?? null,
    [filterId]
  );

  const advance = useCallback(
    async (from: HistoryFeed): Promise<void> => {
      if (!session || inflight.current) return;
      if (from.loaded && !hasMore(from)) return;
      inflight.current = true;
      setBusy(true);
      const next = await loadNextPage(
        (query) => client.listWorkouts(query),
        from,
        { filter: statuses, limit: HISTORY_PAGE_SIZE }
      );
      inflight.current = false;
      if (!alive.current) return;
      setFeed(next);
      setBusy(false);
    },
    [client, session, statuses]
  );

  const reload = useCallback(async (): Promise<void> => {
    const fresh = emptyFeed();
    setFeed(fresh);
    await advance(fresh);
  }, [advance]);

  // Смена фильтра начинает ленту заново: строки чужого статуса в ней остаться
  // не могут, а курсор прежнего фильтра к новому запросу неприменим.
  useEffect(() => {
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [statuses, session?.user.id]);

  return {
    items: feed.items,
    loading: busy && !feed.loaded,
    loadingMore: busy && feed.loaded,
    error: feed.error,
    hasMore: hasMore(feed),
    empty: isEmptyHistory(feed),
    filterId,
    setFilterId,
    loadMore: () => advance(feed),
    reload
  };
}
