import type { WorkoutSetInput } from "@athletica/domain";

import type { OutboxStore } from "../../platform/offline/outbox-store.ts";
import type { SnapshotStore } from "../../platform/offline/snapshot-store.ts";
import { createSqliteOutboxStore, createSqliteSnapshotStore, openLocalDatabase } from "../../platform/offline/sqlite.ts";
import { createOutboxSync } from "../../platform/offline/sync.ts";
import { getAuthClient } from "../auth/auth-client.ts";
import type { ActiveWorkout } from "./active-workout.ts";
import { createWorkoutOffline, type WorkoutOffline } from "./workout-offline.ts";

/**
 * Проводка приложения: SQLite на устройстве + AuthClient как отправитель.
 * База открывается лениво при первом обращении, поэтому импорт этого модуля
 * ничего не делает и не мешает старту.
 */
function lazyOutboxStore<T>(): OutboxStore<T> {
  const store = async (): Promise<OutboxStore<T>> => createSqliteOutboxStore<T>(await openLocalDatabase());
  return {
    listForUser: async (userId) => (await store()).listForUser(userId),
    append: async (item) => (await store()).append(item),
    update: async (record) => (await store()).update(record),
    remove: async (userId, id) => (await store()).remove(userId, id),
    purgeUser: async (userId) => (await store()).purgeUser(userId)
  };
}

function lazySnapshotStore<T>(): SnapshotStore<T> {
  const store = async (): Promise<SnapshotStore<T>> => createSqliteSnapshotStore<T>(await openLocalDatabase());
  return {
    load: async (userId) => (await store()).load(userId),
    save: async (userId, value) => (await store()).save(userId, value),
    clear: async (userId) => (await store()).clear(userId)
  };
}

let instance: WorkoutOffline | null = null;

/** Единственный экземпляр на приложение: очередь обязана быть одна. */
export function getWorkoutOffline(): WorkoutOffline {
  if (instance) return instance;
  const auth = getAuthClient();
  const offline = createWorkoutOffline({
    sync: createOutboxSync({
      store: lazyOutboxStore<WorkoutSetInput>(),
      send: (workoutId, input) => auth.logSet(workoutId, input)
    }),
    snapshots: lazySnapshotStore<ActiveWorkout>()
  });
  // Выход из аккаунта стирает очередь и снимок ушедшего пользователя (H1).
  offline.attach();
  instance = offline;
  return offline;
}
