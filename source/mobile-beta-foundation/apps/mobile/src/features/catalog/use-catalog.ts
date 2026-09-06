import type { ApiError, ExerciseCard, ExerciseDictionary, ExerciseSummary } from "@athletica/api-client";
import { useCallback, useEffect, useRef, useState } from "react";

import { useAuth } from "../auth/auth-context.tsx";

export const CATALOG_PAGE_SIZE = 30;

export type CatalogFilters = { q?: string; section?: string; equipment?: string; muscle?: string };

export type CatalogFeed = {
  items: ExerciseSummary[];
  cursor: string | null;
  exhausted: boolean;
  loading: boolean;
  error: ApiError | null;
  loadMore: () => Promise<void>;
  setFilters: (filters: CatalogFilters) => void;
  filters: CatalogFilters;
};

/**
 * Каталог упражнений постранично.
 *
 * Курсор непрозрачен и возвращается серверу нетронутым. Смена фильтра
 * начинает ленту заново: курсор принадлежит конкретному запросу, и переносить
 * его на другой набор фильтров нельзя.
 */
export function useExerciseCatalog(initial: CatalogFilters = {}): CatalogFeed {
  const { client, session } = useAuth();
  const [filters, setFiltersState] = useState<CatalogFilters>(initial);
  const [items, setItems] = useState<ExerciseSummary[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [exhausted, setExhausted] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const inflight = useRef(false);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const load = useCallback(
    async (from: string | null, replace: boolean, active: CatalogFilters): Promise<void> => {
      if (!session || inflight.current) return;
      inflight.current = true;
      setLoading(true);
      const result = await client.listExercises({ ...active, limit: CATALOG_PAGE_SIZE, cursor: from ?? undefined });
      inflight.current = false;
      if (!alive.current) return;
      setLoading(false);
      if (!result.ok) {
        // Сбой не стирает уже показанное и не двигает курсор: повтор продолжит
        // с того же места, а не с начала.
        setError(result.error);
        return;
      }
      setError(null);
      setItems((prev) => {
        const next = replace ? result.value.items : [...prev, ...result.value.items];
        const seen = new Set<string>();
        return next.filter((item) => (seen.has(item.id) ? false : (seen.add(item.id), true)));
      });
      setCursor(result.value.nextCursor);
      setExhausted(result.value.nextCursor === null);
    },
    [client, session]
  );

  useEffect(() => {
    void load(null, true, filters);
  }, [load, filters]);

  return {
    items,
    cursor,
    exhausted,
    loading,
    error,
    filters,
    loadMore: async () => {
      if (exhausted || !cursor) return;
      await load(cursor, false, filters);
    },
    setFilters: (next) => {
      setItems([]);
      setCursor(null);
      setExhausted(false);
      setFiltersState(next);
    }
  };
}

export type ExerciseCardState = {
  card: ExerciseCard | null;
  error: ApiError | null;
  loading: boolean;
  reload: () => Promise<void>;
};

export function useExerciseCard(exerciseId: string | undefined): ExerciseCardState {
  const { client, session } = useAuth();
  const [card, setCard] = useState<ExerciseCard | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [loading, setLoading] = useState(true);

  const reload = useCallback(async (): Promise<void> => {
    if (!session || !exerciseId) {
      setLoading(false);
      return;
    }
    setLoading(true);
    const result = await client.getExercise(exerciseId);
    setLoading(false);
    if (result.ok) {
      setCard(result.value);
      setError(null);
    } else {
      setError(result.error);
    }
  }, [client, exerciseId, session]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { card, error, loading, reload };
}

export type DictionaryState = { dictionaries: ExerciseDictionary[]; loading: boolean };

/** Фильтры строятся из справочников сервера, а не из зашитого списка. */
export function useExerciseDictionaries(): DictionaryState {
  const { client, session } = useAuth();
  const [dictionaries, setDictionaries] = useState<ExerciseDictionary[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    if (!session) {
      setLoading(false);
      return;
    }
    void client.exerciseDictionaries().then((result) => {
      if (!alive) return;
      setLoading(false);
      if (result.ok) setDictionaries(result.value);
    });
    return () => {
      alive = false;
    };
  }, [client, session]);

  return { dictionaries, loading };
}
