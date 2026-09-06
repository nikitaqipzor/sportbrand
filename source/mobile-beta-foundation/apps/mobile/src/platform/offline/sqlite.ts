import * as SQLite from "expo-sqlite";

import { bySeq, type OutboxItem, type OutboxItemState, type OutboxRecord } from "./outbox.ts";
import type { OutboxStore } from "./outbox-store.ts";
import type { SnapshotStore } from "./snapshot-store.ts";
import type { CrashRecord, CrashStore } from "../diagnostics/crash-log.ts";
import { CRASH_LOG_LIMIT } from "../diagnostics/crash-log.ts";
import type { WorkoutRegistry, WorkoutRegistryEntry } from "./workout-registry.ts";

/**
 * Локальная база устройства.
 *
 * Почему SQLite, а не AsyncStorage: очередь обязана пережить убийство
 * процесса посреди тренировки, а запись должна быть атомарной и
 * упорядоченной. AsyncStorage — это перезапись целого JSON-документа: гонка
 * двух записей теряет подход, а порядок приходится хранить самому. SQLite
 * (expo-sqlite, уже часть SDK, без нативной настройки) даёт транзакцию,
 * автоинкрементный seq для порядка и UNIQUE(user_id, id) — тот самый
 * неизменяемый clientMutationId из ADR 0002, защищающий от дубля на клиенте
 * так же, как уникальный индекс защищает сервер.
 *
 * Секретов здесь нет: токены живут только в Keystore (expo-secure-store).
 */
export const LOCAL_DATABASE_NAME = "athletica-offline.db";

const SCHEMA = `
PRAGMA journal_mode = WAL;
CREATE TABLE IF NOT EXISTS outbox (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id TEXT NOT NULL,
  id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  state TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  failure TEXT,
  payload TEXT NOT NULL,
  UNIQUE (user_id, id)
);
CREATE INDEX IF NOT EXISTS outbox_user_seq ON outbox (user_id, seq);
CREATE TABLE IF NOT EXISTS active_workout (
  user_id TEXT PRIMARY KEY,
  snapshot TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS crash_log (
  id TEXT PRIMARY KEY,
  at TEXT NOT NULL,
  scope TEXT NOT NULL,
  message TEXT NOT NULL,
  stack TEXT
);
CREATE TABLE IF NOT EXISTS workout_registry (
  user_id TEXT NOT NULL,
  workout_id TEXT NOT NULL,
  title TEXT NOT NULL,
  created INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (user_id, workout_id)
);
`;

let database: Promise<SQLite.SQLiteDatabase> | null = null;

export function openLocalDatabase(name: string = LOCAL_DATABASE_NAME): Promise<SQLite.SQLiteDatabase> {
  database ??= SQLite.openDatabaseAsync(name).then(async (db) => {
    await db.execAsync(SCHEMA);
    return db;
  });
  return database;
}

type OutboxRow = {
  seq: number;
  user_id: string;
  id: string;
  created_at: string;
  state: string;
  attempts: number;
  next_attempt_at: string;
  failure: string | null;
  payload: string;
};

const toRecordFromRow = <T>(row: OutboxRow): OutboxRecord<T> => ({
  seq: row.seq,
  userId: row.user_id,
  id: row.id,
  createdAt: row.created_at,
  state: row.state === "dead" ? "dead" : ("pending" satisfies OutboxItemState),
  attempts: row.attempts,
  nextAttemptAt: row.next_attempt_at,
  failure: row.failure,
  payload: JSON.parse(row.payload) as T
});

export function createSqliteOutboxStore<T>(db: SQLite.SQLiteDatabase): OutboxStore<T> {
  const read = async (userId: string, id: string): Promise<OutboxRecord<T> | null> => {
    const row = await db.getFirstAsync<OutboxRow>("SELECT * FROM outbox WHERE user_id = ? AND id = ?", userId, id);
    return row ? toRecordFromRow<T>(row) : null;
  };

  return {
    listForUser: async (userId) => {
      const rows = await db.getAllAsync<OutboxRow>("SELECT * FROM outbox WHERE user_id = ? ORDER BY seq ASC", userId);
      return bySeq(rows.map((row) => toRecordFromRow<T>(row)));
    },

    append: async (item: OutboxItem<T>) => {
      // DO NOTHING, а не REPLACE: clientMutationId неизменяем и обозначает
      // ровно одну мутацию — повтор не должен сбрасывать счётчик попыток.
      await db.runAsync(
        `INSERT INTO outbox (user_id, id, created_at, state, attempts, next_attempt_at, failure, payload)
         VALUES (?, ?, ?, 'pending', 0, ?, NULL, ?)
         ON CONFLICT (user_id, id) DO NOTHING`,
        item.userId,
        item.id,
        item.createdAt,
        item.createdAt,
        JSON.stringify(item.payload)
      );
      const stored = await read(item.userId, item.id);
      if (!stored) throw new Error("не удалось сохранить элемент очереди");
      return stored;
    },

    update: async (record) => {
      await db.runAsync(
        `UPDATE outbox SET state = ?, attempts = ?, next_attempt_at = ?, failure = ?
         WHERE user_id = ? AND id = ?`,
        record.state,
        record.attempts,
        record.nextAttemptAt,
        record.failure,
        record.userId,
        record.id
      );
    },

    remove: async (userId, id) => {
      await db.runAsync("DELETE FROM outbox WHERE user_id = ? AND id = ?", userId, id);
    },

    purgeUser: async (userId) => {
      await db.runAsync("DELETE FROM outbox WHERE user_id = ?", userId);
    }
  };
}

export function createSqliteSnapshotStore<T>(db: SQLite.SQLiteDatabase): SnapshotStore<T> {
  return {
    load: async (userId) => {
      const row = await db.getFirstAsync<{ snapshot: string }>(
        "SELECT snapshot FROM active_workout WHERE user_id = ?",
        userId
      );
      return row ? (JSON.parse(row.snapshot) as T) : null;
    },
    save: async (userId, value) => {
      await db.runAsync(
        `INSERT INTO active_workout (user_id, snapshot) VALUES (?, ?)
         ON CONFLICT (user_id) DO UPDATE SET snapshot = excluded.snapshot`,
        userId,
        JSON.stringify(value)
      );
    },
    clear: async (userId) => {
      await db.runAsync("DELETE FROM active_workout WHERE user_id = ?", userId);
    }
  };
}


type RegistryRow = { workout_id: string; title: string; created: number };

/**
 * Реестр начатых тренировок на диске. Переживает перезапуск ровно так же, как
 * очередь: тренировка, проведённая целиком офлайн, обязана доехать до сервера
 * со своим названием, а не безымянной.
 */
export function createSqliteWorkoutRegistry(db: SQLite.SQLiteDatabase): WorkoutRegistry {
  return {
    remember: async (userId, workoutId, title) => {
      // Название принадлежит серверу с момента создания, поэтому повторный
      // старт не переписывает уже сохранённую строку.
      await db.runAsync(
        "INSERT OR IGNORE INTO workout_registry (user_id, workout_id, title, created) VALUES (?, ?, ?, 0)",
        userId,
        workoutId,
        title
      );
    },

    get: async (userId, workoutId): Promise<WorkoutRegistryEntry | null> => {
      const row = await db.getFirstAsync<RegistryRow>(
        "SELECT workout_id, title, created FROM workout_registry WHERE user_id = ? AND workout_id = ?",
        userId,
        workoutId
      );
      return row ? { workoutId: row.workout_id, title: row.title, created: row.created !== 0 } : null;
    },

    markCreated: async (userId, workoutId) => {
      await db.runAsync(
        "UPDATE workout_registry SET created = 1 WHERE user_id = ? AND workout_id = ?",
        userId,
        workoutId
      );
    },

    forget: async (userId, workoutId) => {
      await db.runAsync("DELETE FROM workout_registry WHERE user_id = ? AND workout_id = ?", userId, workoutId);
    },

    purgeUser: async (userId) => {
      await db.runAsync("DELETE FROM workout_registry WHERE user_id = ?", userId);
    }
  };
}


type CrashRow = { id: string; at: string; scope: string; message: string; stack: string | null };

/**
 * Журнал падений на диске: он нужен именно после перезапуска, потому что
 * приложение к этому моменту уже упало. Журнал не привязан к пользователю —
 * падение может случиться и до входа, и после выхода.
 */
export function createSqliteCrashStore(db: SQLite.SQLiteDatabase, limit = CRASH_LOG_LIMIT): CrashStore {
  return {
    list: async (): Promise<CrashRecord[]> => {
      const rows = await db.getAllAsync<CrashRow>("SELECT id, at, scope, message, stack FROM crash_log ORDER BY at DESC LIMIT ?", limit);
      return rows.map((row) => ({ ...row, stack: row.stack ?? null }));
    },
    append: async (record) => {
      await db.runAsync(
        "INSERT OR REPLACE INTO crash_log (id, at, scope, message, stack) VALUES (?, ?, ?, ?, ?)",
        record.id,
        record.at,
        record.scope,
        record.message,
        record.stack
      );
      // Держим журнал в границах: телефон не место для бесконечного лога.
      await db.runAsync(
        "DELETE FROM crash_log WHERE id NOT IN (SELECT id FROM crash_log ORDER BY at DESC LIMIT ?)",
        limit
      );
    },
    clear: async () => {
      await db.runAsync("DELETE FROM crash_log");
    }
  };
}
