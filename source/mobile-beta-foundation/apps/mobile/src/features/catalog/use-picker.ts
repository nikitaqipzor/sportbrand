import { useCallback, useEffect, useMemo, useState } from "react";

import { useAuth } from "../auth/auth-context.tsx";
import { EXERCISE_CATALOG } from "../workout/exercise-catalog.ts";
import { createSqliteExerciseCache } from "../../platform/offline/sqlite.ts";
import { openLocalDatabase } from "../../platform/offline/sqlite.ts";
import type { CachedExercise, ExerciseCache } from "./exercise-cache.ts";
import { matches } from "./exercise-cache.ts";

/**
 * Встроенный список как запасной вариант первого запуска.
 *
 * Его идентификаторы побайтово совпадают со стартовым набором сервера, поэтому
 * подход, записанный до первой загрузки каталога, ссылается на то же
 * упражнение, что и после неё. Это не второй источник истины, а то, чем
 * приложение живёт до первой синхронизации.
 */
const FALLBACK: CachedExercise[] = EXERCISE_CATALOG.map((entry) => ({
  id: entry.id,
  title: entry.title,
  section: "",
  equipment: ""
}));

function lazyCache(): ExerciseCache {
  return {
    list: async (query) => createSqliteExerciseCache(await openLocalDatabase()).list(query),
    put: async (exercises) => createSqliteExerciseCache(await openLocalDatabase()).put(exercises),
    count: async () => createSqliteExerciseCache(await openLocalDatabase()).count()
  };
}

export type ExercisePicker = {
  options: CachedExercise[];
  query: string;
  setQuery: (query: string) => void;
  /** true, пока каталог ни разу не загружался и показывается запасной список. */
  fallback: boolean;
};

/**
 * Выбор упражнения для экрана тренировки.
 *
 * Читает локальный каталог, поэтому работает без связи. Если сеть есть —
 * обновляет копию в фоне, но не ждёт её: тренировка не должна упираться в
 * загрузку справочника.
 */
export function useExercisePicker(): ExercisePicker {
  const { client, session } = useAuth();
  const cache = useMemo(() => lazyCache(), []);
  const [options, setOptions] = useState<CachedExercise[]>(FALLBACK);
  const [fallback, setFallback] = useState(true);
  const [query, setQuery] = useState("");

  const readCache = useCallback(async (): Promise<boolean> => {
    const cached = await cache.list();
    if (cached.length === 0) return false;
    setOptions(cached);
    setFallback(false);
    return true;
  }, [cache]);

  useEffect(() => {
    let alive = true;
    void (async () => {
      const hadCache = await readCache();
      if (!alive || !session) return;
      // Обновление каталога в фоне: без связи просто останется прежняя копия.
      const result = await client.listExercises({ limit: 200 });
      if (!alive || !result.ok) return;
      await cache.put(result.value.items);
      await readCache();
      void hadCache;
    })();
    return () => {
      alive = false;
    };
  }, [cache, client, readCache, session]);

  return {
    options: options.filter((option) => matches(option, query)),
    query,
    setQuery,
    fallback
  };
}
