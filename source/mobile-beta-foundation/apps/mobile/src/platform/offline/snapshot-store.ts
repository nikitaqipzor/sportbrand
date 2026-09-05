/**
 * Хранилище одного снимка на пользователя (активная тренировка). Тот же
 * контракт, что у очереди: чужой снимок недостижим без явного userId, а выход
 * из сессии стирает снимок ушедшего пользователя.
 */
export type SnapshotStore<T> = {
  load: (userId: string) => Promise<T | null>;
  save: (userId: string, value: T) => Promise<void>;
  clear: (userId: string) => Promise<void>;
};

export type SnapshotMemoryDb = { values: Record<string, string> };

export const createSnapshotMemoryDb = (): SnapshotMemoryDb => ({ values: {} });

export function createMemorySnapshotStore<T>(db: SnapshotMemoryDb = createSnapshotMemoryDb()): SnapshotStore<T> {
  return {
    load: async (userId) => {
      const raw = db.values[userId];
      return raw === undefined ? null : (JSON.parse(raw) as T);
    },
    save: async (userId, value) => {
      db.values[userId] = JSON.stringify(value);
    },
    clear: async (userId) => {
      delete db.values[userId];
    }
  };
}
