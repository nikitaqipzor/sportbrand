import type { ApiError, Progress, WorkoutDetail } from "@athletica/api-client";
import { useCallback, useEffect, useRef, useState } from "react";

import { useAuth } from "../auth/auth-context.tsx";

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
