/**
 * Доменное ядро офлайн-очереди: чистые функции над массивом элементов.
 *
 * Здесь нет ни ввода-вывода, ни времени «изнутри» — всё передаётся явно,
 * поэтому переходы очереди проверяются тестами без хранилища и без сети.
 * Персистентность построена ВОКРУГ этих функций (см. outbox-store.ts,
 * sqlite.ts), а не вместо них.
 */

export type OutboxItem<T> = { id: string; userId: string; createdAt: string; payload: T };

export function itemsForUser<T>(items: OutboxItem<T>[], userId: string): OutboxItem<T>[] {
  return items.filter((item) => item.userId === userId);
}

export function purgeForLogout<T>(items: OutboxItem<T>[], userId: string): OutboxItem<T>[] {
  return items.filter((item) => item.userId !== userId);
}

/**
 * Идемпотентная постановка в очередь: повторная запись с тем же id того же
 * пользователя заменяет предыдущую, а не создаёт дубль (см. ADR 0002).
 */
export function enqueue<T>(items: OutboxItem<T>[], item: OutboxItem<T>): OutboxItem<T>[] {
  const withoutDuplicate = items.filter((existing) => !(existing.id === item.id && existing.userId === item.userId));
  return [...withoutDuplicate, item];
}

/**
 * pending — ждёт отправки; dead — сервер отверг мутацию навсегда (4xx кроме
 * 409). «Мёртвый» элемент не перепосылается никогда, но и не удаляется молча:
 * он виден пользователю с причиной и не блокирует остальную очередь.
 */
export type OutboxItemState = "pending" | "dead";

export type OutboxRecord<T> = OutboxItem<T> & {
  /** Монотонный номер вставки: очередь отправляется строго по возрастанию. */
  seq: number;
  state: OutboxItemState;
  attempts: number;
  /** Раньше этого момента повторять бессмысленно (backoff). */
  nextAttemptAt: string;
  /** Причина последнего сбоя; для «мёртвых» — причина, по которой сняли. */
  failure: string | null;
};

export const OUTBOX_BASE_BACKOFF_MS = 2_000;
export const OUTBOX_MAX_BACKOFF_MS = 5 * 60_000;

/** Экспоненциальный backoff с потолком: 2с, 4с, 8с … но не больше 5 минут. */
export function outboxBackoffMs(
  attempts: number,
  base: number = OUTBOX_BASE_BACKOFF_MS,
  max: number = OUTBOX_MAX_BACKOFF_MS
): number {
  if (attempts < 1) return 0;
  const exponential = base * 2 ** (attempts - 1);
  return Math.min(max, exponential);
}

export function toRecord<T>(item: OutboxItem<T>, seq: number): OutboxRecord<T> {
  return { ...item, seq, state: "pending", attempts: 0, nextAttemptAt: item.createdAt, failure: null };
}

export function isDue<T>(record: OutboxRecord<T>, now: Date): boolean {
  return record.state === "pending" && Date.parse(record.nextAttemptAt) <= now.getTime();
}

/** Временный сбой: элемент остаётся в очереди, но отодвигается по времени. */
export function withFailure<T>(
  record: OutboxRecord<T>,
  reason: string,
  now: Date,
  backoff: (attempts: number) => number = outboxBackoffMs
): OutboxRecord<T> {
  const attempts = record.attempts + 1;
  return {
    ...record,
    attempts,
    failure: reason,
    nextAttemptAt: new Date(now.getTime() + backoff(attempts)).toISOString()
  };
}

/**
 * Навсегда невалидная мутация. Перепосылать её вечно нельзя: она встанет
 * первой в очереди и заблокирует все последующие подходы.
 */
export function withDeath<T>(record: OutboxRecord<T>, reason: string): OutboxRecord<T> {
  return { ...record, state: "dead", failure: reason };
}

export function pendingRecords<T>(records: OutboxRecord<T>[]): OutboxRecord<T>[] {
  return records.filter((record) => record.state === "pending");
}

export function deadRecords<T>(records: OutboxRecord<T>[]): OutboxRecord<T>[] {
  return records.filter((record) => record.state === "dead");
}

/** Порядок очереди — это порядок записи подходов, а не порядок ответа сервера. */
export function bySeq<T>(records: OutboxRecord<T>[]): OutboxRecord<T>[] {
  return [...records].sort((left, right) => left.seq - right.seq);
}
