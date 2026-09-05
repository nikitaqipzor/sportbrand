import type { WorkoutAction, WorkoutSetInput } from "@athletica/domain";

import type { OutboxRecord } from "../../platform/offline/outbox.ts";
import type { SnapshotStore } from "../../platform/offline/snapshot-store.ts";
import type { FlushSummary, OutboxSync, OutboxSyncStatus } from "../../platform/offline/sync.ts";
import { registerSessionCleanup, type SessionCleanupEvent } from "../auth/session-cleanup.ts";
import {
  actOnWorkout,
  buildSetInput,
  nextSetNumber,
  startActiveWorkout,
  withRecordedSet,
  type ActiveWorkout,
  type StartWorkoutInput
} from "./active-workout.ts";
import { submitSet } from "./log-set.ts";

export type WorkoutOfflineDeps = {
  sync: OutboxSync;
  snapshots: SnapshotStore<ActiveWorkout>;
  now?: () => Date;
};

export type RecordSetResult =
  | { ok: true; workout: ActiveWorkout; record: OutboxRecord<WorkoutSetInput> }
  | { ok: false; issues: string[] };

export type FinishResult = { ok: true; workout: ActiveWorkout; discarded: number } | { ok: false; reason: string };

export type WorkoutOffline = {
  sync: OutboxSync;
  start: (userId: string, input: StartWorkoutInput) => Promise<ActiveWorkout>;
  load: (userId: string) => Promise<ActiveWorkout | null>;
  recordSet: (
    userId: string,
    workout: ActiveWorkout,
    measures: { weightKg: number; repetitions: number; rir: number }
  ) => Promise<RecordSetResult>;
  /** cancel и complete — разрушительные действия, экран спрашивает confirm. */
  finish: (userId: string, workout: ActiveWorkout, action: WorkoutAction) => Promise<FinishResult>;
  flush: (userId: string | null) => Promise<FlushSummary>;
  status: (userId: string | null) => Promise<OutboxSyncStatus>;
  list: (userId: string | null) => Promise<OutboxRecord<WorkoutSetInput>[]>;
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
      const existing = await deps.snapshots.load(userId);
      if (existing && existing.workoutId === input.workoutId && existing.status !== "cancelled") return existing;
      return persist(userId, startActiveWorkout(input, now()));
    },

    load: (userId) => deps.snapshots.load(userId),

    recordSet: async (userId, workout, measures) => {
      const input = buildSetInput(workout, measures, nextSetNumber(workout));
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
      await deps.snapshots.clear(userId);
      return { ok: true, workout: transition.workout, discarded };
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
    },

    attach: () => registerSessionCleanup((event) => offline.onSessionEnded(event))
  };

  return offline;
}
