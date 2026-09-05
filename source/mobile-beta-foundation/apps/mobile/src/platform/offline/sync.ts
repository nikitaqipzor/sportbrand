import { describeApiError, type ApiResult, type LogSetOutcome, type WorkoutSetInput as ApiSetInput } from "@athletica/api-client";
import type { WorkoutSetInput } from "@athletica/domain";

import {
  bySeq,
  deadRecords,
  isDue,
  outboxBackoffMs,
  pendingRecords,
  withDeath,
  withFailure,
  type OutboxItem,
  type OutboxRecord
} from "./outbox.ts";
import type { OutboxStore } from "./outbox-store.ts";

/** Отправитель мутации. В приложении — AuthClient.logSet, в тестах — мок. */
export type LogSetSender = (workoutId: string, input: ApiSetInput) => Promise<ApiResult<LogSetOutcome>>;

export type OutboxSyncDeps = {
  store: OutboxStore<WorkoutSetInput>;
  send: LogSetSender;
  now?: () => Date;
  backoff?: (attempts: number) => number;
};

/**
 * Чем закончился проход синхронизации.
 * - done    — очередь пуста или всё, что было готово, ушло;
 * - retry   — упёрлись во временный сбой, повторим после backoff;
 * - paused  — сессия истекла, отправка остановлена до следующего входа;
 * - busy    — проход уже идёт, второй не запускается (см. single-flight);
 * - no-user — отправлять не под кем.
 */
export type FlushReason = "done" | "retry" | "paused" | "busy" | "no-user";

export type FlushSummary = {
  reason: FlushReason;
  /** Приняты сервером: created. */
  sent: number;
  /** Приняты сервером ранее: 409 duplicate — тоже успех, элемент снимается. */
  duplicates: number;
  /** Уведены в «мёртвые» на этом проходе (4xx кроме 409). */
  dead: number;
  /** Оставлены в очереди с отложенным повтором. */
  retried: number;
};

export type OutboxSyncStatus = {
  pending: number;
  dead: number;
  syncing: boolean;
  paused: boolean;
  /** Короткое описание последней причины, по которой отправка не прошла. */
  lastFailure: string | null;
};

export type OutboxSync = {
  enqueue: (userId: string, input: WorkoutSetInput, now?: Date) => Promise<OutboxRecord<WorkoutSetInput>>;
  flush: (userId: string | null) => Promise<FlushSummary>;
  status: (userId: string | null) => Promise<OutboxSyncStatus>;
  /** Очередь и «мёртвые» элементы пользователя в порядке записи. */
  list: (userId: string | null) => Promise<OutboxRecord<WorkoutSetInput>[]>;
  isPaused: () => boolean;
  pause: () => void;
  /** Снимает паузу после успешного входа. */
  resume: () => void;
  /**
   * Убирает неотправленные подходы отменённой тренировки: отменённая
   * тренировка не должна доехать до сервера (QA-004).
   */
  discardWorkout: (userId: string, workoutId: string) => Promise<number>;
  purgeUser: (userId: string) => Promise<void>;
};

const empty = (reason: FlushReason): FlushSummary => ({ reason, sent: 0, duplicates: 0, dead: 0, retried: 0 });

/** Тело запроса контракта: workoutId живёт в пути, а не в теле. */
export function toApiSetInput(input: WorkoutSetInput): ApiSetInput {
  return {
    exerciseId: input.exerciseId,
    setNumber: input.setNumber,
    weightKg: input.weightKg,
    repetitions: input.repetitions,
    rir: input.rir,
    clientMutationId: input.clientMutationId
  };
}

/**
 * Синхронизация «ровно один раз».
 *
 * Гарантии:
 *  1. clientMutationId неизменяем и уникален в паре с userId — сервер
 *     дедуплицирует повтор и отвечает 409 (LogSetOutcome duplicate);
 *  2. duplicate обрабатывается как успех: элемент снимается с очереди, а не
 *     отправляется снова;
 *  3. проход строго один — параллельный flush() получает тот же промис, так
 *     что два потока физически не могут взять один элемент;
 *  4. элемент удаляется из очереди ТОЛЬКО после подтверждения сервера, а
 *     значит потеря соединения приводит к повтору, но не к пропаже подхода.
 *
 * Порядок: элементы уходят строго по seq. Временный сбой прерывает проход
 * (связи всё равно нет), навсегда невалидная мутация уходит в «мёртвые» и
 * проход продолжается — очередь не встаёт намертво ни в одном из случаев.
 */
export function createOutboxSync(deps: OutboxSyncDeps): OutboxSync {
  const now = deps.now ?? (() => new Date());
  const backoff = deps.backoff ?? outboxBackoffMs;
  let inflight: Promise<FlushSummary> | null = null;
  let paused = false;
  let lastFailure: string | null = null;

  async function run(userId: string): Promise<FlushSummary> {
    const summary = empty("done");
    const queue = bySeq(pendingRecords(await deps.store.listForUser(userId)));

    for (const record of queue) {
      // Ещё не подошёл срок повтора: следующие элементы обязаны ждать своей
      // очереди, иначе подходы уедут на сервер в неправильном порядке.
      if (!isDue(record, now())) {
        summary.reason = "retry";
        break;
      }

      const result = await deps.send(record.payload.workoutId, toApiSetInput(record.payload));

      if (result.ok) {
        await deps.store.remove(userId, record.id);
        if (result.value.outcome === "duplicate") summary.duplicates += 1;
        else summary.sent += 1;
        continue;
      }

      const error = result.error;
      const description = describeApiError(error);

      if (error.kind === "session_expired") {
        paused = true;
        lastFailure = description;
        summary.reason = "paused";
        break;
      }

      // 4xx (кроме 409, который клиент уже разобрал как duplicate) —
      // мутация невалидна навсегда. Повторять её вечно нельзя.
      if (error.kind === "client" && error.status !== 409) {
        await deps.store.update(withDeath(record, description));
        lastFailure = description;
        summary.dead += 1;
        continue;
      }

      await deps.store.update(withFailure(record, description, now(), backoff));
      lastFailure = description;
      summary.retried += 1;
      summary.reason = "retry";
      break;
    }

    if (summary.reason === "done" && summary.dead === 0) lastFailure = null;
    return summary;
  }

  return {
    enqueue: (userId, input, at = now()) =>
      deps.store.append({ id: input.clientMutationId, userId, createdAt: at.toISOString(), payload: input } satisfies OutboxItem<WorkoutSetInput>),

    flush: (userId) => {
      if (!userId) return Promise.resolve(empty("no-user"));
      if (paused) return Promise.resolve(empty("paused"));
      // Single-flight: второй вызов не запускает второй проход, а ждёт первый.
      if (inflight) return inflight.then((summary) => ({ ...summary, reason: "busy" as FlushReason }));
      const started = run(userId).finally(() => {
        if (inflight === started) inflight = null;
      });
      inflight = started;
      return started;
    },

    status: async (userId) => {
      if (!userId) return { pending: 0, dead: 0, syncing: inflight !== null, paused, lastFailure };
      const records = await deps.store.listForUser(userId);
      return {
        pending: pendingRecords(records).length,
        dead: deadRecords(records).length,
        syncing: inflight !== null,
        paused,
        lastFailure
      };
    },

    list: async (userId) => (userId ? bySeq(await deps.store.listForUser(userId)) : []),

    isPaused: () => paused,
    pause: () => {
      paused = true;
    },
    resume: () => {
      paused = false;
      lastFailure = null;
    },
    discardWorkout: async (userId, workoutId) => {
      const records = await deps.store.listForUser(userId);
      const doomed = records.filter((record) => record.payload.workoutId === workoutId);
      for (const record of doomed) await deps.store.remove(userId, record.id);
      return doomed.length;
    },

    purgeUser: async (userId) => {
      await deps.store.purgeUser(userId);
    }
  };
}
