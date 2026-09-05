import type { WorkoutAction, WorkoutSetInput } from "@athletica/domain";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { OutboxRecord } from "../../platform/offline/outbox.ts";
import type { OutboxSyncStatus } from "../../platform/offline/sync.ts";
import { useAuth } from "../auth/auth-context.tsx";
import {
  currentExercise,
  nextSetNumber,
  type ActiveExercise,
  type ActiveWorkout,
  type ExerciseSeed,
  type SetMeasures,
  type StartWorkoutInput
} from "./active-workout.ts";
import { getWorkoutOffline } from "./runtime.ts";

const IDLE_STATUS: OutboxSyncStatus = { pending: 0, dead: 0, syncing: false, paused: false, lastFailure: null };

/** Пауза между фоновыми попытками отправки, пока экран открыт. */
export const SYNC_INTERVAL_MS = 15_000;

export type { SetMeasures };

export type ActiveWorkoutView = {
  userId: string | null;
  workout: ActiveWorkout | null;
  /** Упражнения тренировки в порядке добавления. */
  exercises: ActiveExercise[];
  /** Открытое упражнение; его нумерация и показана на экране. */
  exercise: ActiveExercise | null;
  /** Номер следующего подхода ОТКРЫТОГО упражнения. */
  setNumber: number;
  status: OutboxSyncStatus;
  queue: OutboxRecord<WorkoutSetInput>[];
  issues: string[];
  busy: boolean;
  recordSet: (measures: SetMeasures) => Promise<void>;
  selectExercise: (exerciseId: string) => Promise<void>;
  addExercise: (seed: ExerciseSeed) => Promise<void>;
  finish: (action: WorkoutAction) => Promise<void>;
  syncNow: () => Promise<void>;
};

/**
 * Незавершённая тренировка предыдущего запуска (П3).
 *
 * Приложение могли убить посреди подхода: снимок пережил это, и «Сегодня»
 * обязана предложить вернуться, а не начать новую тренировку поверх — иначе
 * старт новой затрёт снимок и записанное потеряется.
 */
export function useResumableWorkout(): { workout: ActiveWorkout | null; refresh: () => Promise<void> } {
  const { session } = useAuth();
  const userId = session?.user.id ?? null;
  const offline = useMemo(() => getWorkoutOffline(), []);
  const [workout, setWorkout] = useState<ActiveWorkout | null>(null);
  const alive = useRef(true);

  const refresh = useCallback(async (): Promise<void> => {
    const found = await offline.resumable(userId);
    if (alive.current) setWorkout(found);
  }, [offline, userId]);

  useEffect(() => {
    alive.current = true;
    void refresh();
    return () => {
      alive.current = false;
    };
  }, [refresh]);

  return { workout, refresh };
}

/** Состояние очереди для экранов, где активной тренировки нет. */
export function useSyncStatus(): OutboxSyncStatus {
  const { session } = useAuth();
  const userId = session?.user.id ?? null;
  const offline = useMemo(() => getWorkoutOffline(), []);
  const [status, setStatus] = useState<OutboxSyncStatus>(IDLE_STATUS);

  useEffect(() => {
    let alive = true;
    const refresh = async (): Promise<void> => {
      if (userId) offline.signedIn(userId);
      await offline.flush(userId);
      const next = await offline.status(userId);
      if (alive) setStatus(next);
    };
    void refresh();
    const timer = setInterval(() => void refresh(), SYNC_INTERVAL_MS);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [offline, userId]);

  return status;
}

/**
 * Активная тренировка на экране: снимок из SQLite, запись подхода в очередь,
 * фоновая отправка и разрушительные действия (отмена/завершение).
 */
export function useActiveWorkout(input: StartWorkoutInput): ActiveWorkoutView {
  const { session } = useAuth();
  const userId = session?.user.id ?? null;
  const offline = useMemo(() => getWorkoutOffline(), []);
  const [workout, setWorkout] = useState<ActiveWorkout | null>(null);
  const [status, setStatus] = useState<OutboxSyncStatus>(IDLE_STATUS);
  const [queue, setQueue] = useState<OutboxRecord<WorkoutSetInput>[]>([]);
  const [issues, setIssues] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const alive = useRef(true);

  const refresh = useCallback(async (): Promise<void> => {
    const [next, records] = await Promise.all([offline.status(userId), offline.list(userId)]);
    if (!alive.current) return;
    setStatus(next);
    setQueue(records);
  }, [offline, userId]);

  const sync = useCallback(async (): Promise<void> => {
    await offline.flush(userId);
    await refresh();
  }, [offline, refresh, userId]);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  useEffect(() => {
    if (!userId) {
      setWorkout(null);
      setQueue([]);
      setStatus(IDLE_STATUS);
      return;
    }
    let cancelled = false;
    // Новый вход снимает паузу, оставленную истёкшей сессией.
    offline.signedIn(userId);
    void offline.start(userId, input).then(async (snapshot) => {
      if (!cancelled && alive.current) setWorkout(snapshot);
      await sync();
    });
    const timer = setInterval(() => void sync(), SYNC_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
    // input — простой литерал из параметров маршрута, следим за его полями.
  }, [offline, userId, input.workoutId, input.title, sync]);

  const recordSet = useCallback(
    async (measures: SetMeasures): Promise<void> => {
      if (!userId || !workout || busy) return;
      setBusy(true);
      // Подход записывается в ОТКРЫТОЕ упражнение: его номер и его
      // clientMutationId, независимо от остальных упражнений тренировки.
      const result = await offline.recordSet(userId, workout, measures, workout.currentExerciseId);
      if (!result.ok) {
        if (alive.current) {
          setIssues(result.issues);
          setBusy(false);
        }
        return;
      }
      if (alive.current) {
        setIssues([]);
        setWorkout(result.workout);
      }
      await sync();
      if (alive.current) setBusy(false);
    },
    [busy, offline, sync, userId, workout]
  );

  const selectExercise = useCallback(
    async (exerciseId: string): Promise<void> => {
      if (!userId || !workout) return;
      const next = await offline.selectExercise(userId, workout, exerciseId);
      if (alive.current) setWorkout(next);
    },
    [offline, userId, workout]
  );

  const addExercise = useCallback(
    async (seed: ExerciseSeed): Promise<void> => {
      if (!userId || !workout) return;
      const next = await offline.addExercise(userId, workout, seed);
      if (alive.current) setWorkout(next);
    },
    [offline, userId, workout]
  );

  const finish = useCallback(
    async (action: WorkoutAction): Promise<void> => {
      if (!userId || !workout) return;
      const result = await offline.finish(userId, workout, action);
      if (!result.ok) {
        if (alive.current) setIssues([result.reason]);
        return;
      }
      if (alive.current) setWorkout(result.workout);
      await sync();
    },
    [offline, sync, userId, workout]
  );

  const exercise = workout ? currentExercise(workout) : null;

  return {
    userId,
    workout,
    exercises: workout?.exercises ?? [],
    exercise,
    setNumber: workout ? nextSetNumber(workout, workout.currentExerciseId) : 1,
    status,
    queue,
    issues,
    busy,
    recordSet,
    selectExercise,
    addExercise,
    finish,
    syncNow: sync
  };
}
