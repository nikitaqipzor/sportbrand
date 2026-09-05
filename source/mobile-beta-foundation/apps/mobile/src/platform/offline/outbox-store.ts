import { bySeq, toRecord, type OutboxItem, type OutboxRecord } from "./outbox.ts";

/**
 * Узкий интерфейс персистентной очереди. На устройстве за ним стоит SQLite
 * (см. sqlite.ts), в тестах — реализация в памяти: сеть и настоящий SQLite
 * для проверки правил синхронизации не нужны.
 *
 * Все методы пользовательские: id владельца обязателен везде, где элемент
 * читается или удаляется, — очередь одного пользователя физически не может
 * попасть в отправку под чужой сессией (ADR 0002, находка H1).
 */
export type OutboxStore<T> = {
  /** Все элементы пользователя в порядке записи (pending и dead вперемешку). */
  listForUser: (userId: string) => Promise<OutboxRecord<T>[]>;
  /**
   * Идемпотентная вставка: повторный append с тем же clientMutationId того же
   * пользователя не создаёт вторую строку и не сбрасывает счётчик попыток —
   * clientMutationId неизменяем и навсегда обозначает одну мутацию.
   */
  append: (item: OutboxItem<T>) => Promise<OutboxRecord<T>>;
  /** Сохраняет изменённое состояние элемента (attempts/backoff/dead). */
  update: (record: OutboxRecord<T>) => Promise<void>;
  /** Снимает элемент с очереди: сервер его принял. */
  remove: (userId: string, id: string) => Promise<void>;
  /** Полное удаление очереди пользователя — вызывается при выходе. */
  purgeUser: (userId: string) => Promise<void>;
};

/**
 * «Диск» для реализации в памяти. Тест пересоздаёт store поверх того же db —
 * это и есть перезапуск процесса без потери данных.
 */
export type OutboxMemoryDb<T> = { rows: OutboxRecord<T>[]; nextSeq: number };

export const createOutboxMemoryDb = <T>(): OutboxMemoryDb<T> => ({ rows: [], nextSeq: 1 });

const clone = <T>(value: T): T => structuredClone(value);

export function createMemoryOutboxStore<T>(db: OutboxMemoryDb<T> = createOutboxMemoryDb<T>()): OutboxStore<T> {
  const find = (userId: string, id: string): OutboxRecord<T> | undefined =>
    db.rows.find((row) => row.userId === userId && row.id === id);

  return {
    listForUser: async (userId) => bySeq(db.rows.filter((row) => row.userId === userId)).map(clone),

    append: async (item) => {
      const existing = find(item.userId, item.id);
      if (existing) return clone(existing);
      const record = toRecord(clone(item), db.nextSeq);
      db.nextSeq += 1;
      db.rows.push(record);
      return clone(record);
    },

    update: async (record) => {
      const index = db.rows.findIndex((row) => row.userId === record.userId && row.id === record.id);
      if (index >= 0) db.rows[index] = clone(record);
    },

    remove: async (userId, id) => {
      db.rows = db.rows.filter((row) => !(row.userId === userId && row.id === id));
    },

    purgeUser: async (userId) => {
      db.rows = db.rows.filter((row) => row.userId !== userId);
    }
  };
}
