import { validateSet, type WorkoutAction, type WorkoutSetInput } from "@athletica/domain";

import type { WorkoutMutation } from "../../platform/offline/mutations.ts";

import type { OutboxRecord } from "../../platform/offline/outbox.ts";
import type { SnapshotStore } from "../../platform/offline/snapshot-store.ts";
import type { WorkoutRegistry } from "../../platform/offline/workout-registry.ts";
import type { FlushSummary, OutboxSync, OutboxSyncStatus } from "../../platform/offline/sync.ts";
import { deleteMutationId, editMutationId } from "../../platform/offline/mutations.ts";
import { registerSessionCleanup, type SessionCleanupEvent } from "../auth/session-cleanup.ts";
import {
  actOnWorkout,
  buildSetInput,
  isResumable,
  nextSetNumber,
  reviveActiveWorkout,
  startActiveWorkout,
  withCurrentExercise,
  withExercise,
  withRecordedSet,
  type ActiveWorkout,
  type ExerciseSeed,
  type SetMeasures,
  type StartWorkoutInput
} from "./active-workout.ts";
import { submitSet } from "./log-set.ts";

export type WorkoutOfflineDeps = {
  sync: OutboxSync;
  snapshots: SnapshotStore<ActiveWorkout>;
  /** Помнит название тренировки дольше, чем живёт её снимок. */
  registry?: WorkoutRegistry;
  now?: () => Date;
};

export type RecordSetResult =
  | { ok: true; workout: ActiveWorkout; record: OutboxRecord<WorkoutMutation> }
  | { ok: false; issues: string[] };

export type EditResult = { ok: true } | { ok: false; issues: string[] };

export type FinishResult = { ok: true; workout: ActiveWorkout; discarded: number } | { ok: false; reason: string };

export type WorkoutOffline = {
  sync: OutboxSync;
  start: (userId: string, input: StartWorkoutInput) => Promise<ActiveWorkout>;
  load: (userId: string) => Promise<ActiveWorkout | null>;
  recordSet: (
    userId: string,
    workout: ActiveWorkout,
    measures: SetMeasures,
    /** По умолчанию — открытое упражнение снимка. */
    exerciseId?: string
  ) => Promise<RecordSetResult>;
  /** Переключение между упражнениями тренировки; снимок сохраняется. */
  selectExercise: (userId: string, workout: ActiveWorkout, exerciseId: string) => Promise<ActiveWorkout>;
  /** Добавление упражнения в идущую тренировку; оно сразу становится текущим. */
  addExercise: (userId: string, workout: ActiveWorkout, seed: ExerciseSeed) => Promise<ActiveWorkout>;
  /** Незавершённая тренировка предыдущего запуска, если она есть (П3). */
  resumable: (userId: string | null) => Promise<ActiveWorkout | null>;
  /**
   * Правка записанного подхода. Уходит через ту же очередь, что и запись,
   * поэтому работает без сети.
   *
   * revision различает две правки одного подхода: без него вторая получила бы
   * идентификатор первой, сервер счёл бы её уже применённой и молча отбросил.
   */
  editSet: (
    userId: string,
    workoutId: string,
    setId: string,
    patch: SetMeasures,
    revision?: number
  ) => Promise<EditResult>;
  /** Удаление записанного подхода. Одно намерение — один идентификатор. */
  deleteSet: (userId: string, workoutId: string, setId: string) => Promise<void>;
  /** cancel и complete — разрушительные действия, экран спрашивает confirm. */
  finish: (userId: string, workout: ActiveWorkout, action: WorkoutAction) => Promise<FinishResult>;
  flush: (userId: string | null) => Promise<FlushSummary>;
  status: (userId: string | null) => Promise<OutboxSyncStatus>;
  list: (userId: string | null) => Promise<OutboxRecord<WorkoutMutation>[]>;
  /** Новая сессия: отправка снова разрешена. */
  signedIn: (userId: string) => void;
  /**
   * H1: выход из сессии стирает снимок и очередь ушедшего пользователя,
   * чтобы его подходы физически не могли уйти под токеном следующего.
   */
  onSessionEnded: (event: SessionCleanupEvent) => Promise<void>;
  /** Подписывает onSessionEnded на выход из аккаунта; вернёт отписку. */
  attach: () => () => void;
};

export function createWorkoutOffline(deps: WorkoutOfflineDeps): WorkoutOffline {
  const now = deps.now ?? (() => new Date());

  const persist = async (userId: string, workout: ActiveWorkout): Promise<ActiveWorkout> => {
    await deps.snapshots.save(userId, workout);
    return workout;
  };

  const offline: WorkoutOffline = {
    sync: deps.sync,

    start: async (userId, input) => {
      await deps.registry?.remember(userId, input.workoutId, input.title);
      const existing = reviveActiveWorkout(await deps.snapshots.load(userId));
      // Возврат в ту же тренировку поднимает её снимок целиком: все
      // упражнения со своими счётчиками, а не пустую сессию заново.
      if (existing && existing.workoutId === input.workoutId && existing.status !== "cancelled") {
        return persist(userId, existing);
      }
      return persist(userId, startActiveWorkout(input, now()));
    },

    load: async (userId) => reviveActiveWorkout(await deps.snapshots.load(userId)),

    resumable: async (userId) => {
      if (!userId) return null;
      const snapshot = reviveActiveWorkout(await deps.snapshots.load(userId));
      return isResumable(snapshot) ? snapshot : null;
    },

    selectExercise: (userId, workout, exerciseId) => persist(userId, withCurrentExercise(workout, exerciseId)),

    addExercise: (userId, workout, seed) => persist(userId, withExercise(workout, seed)),

    recordSet: async (userId, workout, measures, exerciseId = workout.currentExerciseId) => {
      const input = buildSetInput(workout, measures, nextSetNumber(workout, exerciseId), exerciseId);
      const result = await submitSet(deps.sync, userId, input, now());
      if (!result.ok) return result;
      const updated = await persist(userId, withRecordedSet(workout, input, now()));
      return { ok: true, workout: updated, record: result.record };
    },

    finish: async (userId, workout, action) => {
      const transition = actOnWorkout(workout, action);
      if (!transition.ok) return { ok: false, reason: transition.reason };
      // Отменённая тренировка не должна доехать до сервера: снимаем её
      // неотправленные подходы. Завершённая, наоборот, обязана досинхронизироваться.
      const discarded =
        action === "cancel" ? await deps.sync.discardWorkout(userId, workout.workoutId) : 0;
      // Отменённая тренировка не поедет на сервер — её незачем помнить.
      if (action === "cancel") await deps.registry?.forget(userId, workout.workoutId);
      await deps.snapshots.clear(userId);
      return { ok: true, workout: transition.workout, discarded };
    },

    editSet: async (userId, workoutId, setId, patch, revision = 1) => {
      // Границы те же, что у записи: правка не должна быть обходным путём
      // мимо домена. setNumber и exerciseId не меняются — они и есть
      // идентичность записи, поэтому для проверки берём безопасные значения.
      const issues = validateSet({
        workoutId,
        exerciseId: "check",
        setNumber: 1,
        clientMutationId: "check",
        ...patch
      });
      if (issues.length > 0) return { ok: false, issues };

      const id = editMutationId(setId, revision);
      const amended = await deps.sync.amendPending(userId, id, patch);
      if (!amended) {
        await deps.sync.enqueueMutation(userId, id, { kind: "edit-set", workoutId, setId, patch });
      }
      return { ok: true };
    },

    deleteSet: async (userId, workoutId, setId) => {
      await deps.sync.enqueueMutation(userId, deleteMutationId(setId), {
        kind: "delete-set",
        workoutId,
        setId
      });
    },

    flush: (userId) => deps.sync.flush(userId),
    status: (userId) => deps.sync.status(userId),
    list: (userId) => deps.sync.list(userId),

    signedIn: (_userId) => {
      deps.sync.resume();
    },

    onSessionEnded: async (event) => {
      deps.sync.pause();
      if (!event.userId) return;
      await deps.sync.purgeUser(event.userId);
      await deps.snapshots.clear(event.userId);
      await deps.registry?.purgeUser(event.userId);
    },

    attach: () => registerSessionCleanup((event) => offline.onSessionEnded(event))
  };

  return offline;
}
