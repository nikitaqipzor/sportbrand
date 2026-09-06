import {
  describeApiError,
  type ApiError,
  type ApiResult,
  type LogSetOutcome,
  type WorkoutSetInput as ApiSetInput
} from "@athletica/api-client";
import type { WorkoutSetInput } from "@athletica/domain";

import { needsWorkout, workoutIdOf, type WorkoutMutation } from "./mutations.ts";

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
import type { WorkoutRegistry } from "./workout-registry.ts";

/** Отправитель мутации. В приложении — AuthClient.logSet, в тестах — мок. */
export type LogSetSender = (workoutId: string, input: ApiSetInput) => Promise<ApiResult<LogSetOutcome>>;

/** Правка подхода. duplicate и gone — успешные исходы, см. EditSetOutcome. */
export type EditSetSender = (
  workoutId: string,
  setId: string,
  patch: { weightKg: number; repetitions: number; rir: number },
  clientMutationId: string
) => Promise<ApiResult<{ outcome: "updated" | "duplicate" | "gone" }>>;

/** Удаление подхода. Повтор отвечает тем же исходом, различать незачем. */
export type DeleteSetSender = (
  workoutId: string,
  setId: string,
  clientMutationId: string
) => Promise<ApiResult<{ outcome: "deleted" }>>;

/**
 * Создание тренировки на сервере. Идемпотентно по клиентскому id: повтор
 * возвращает ту же сессию, поэтому вызывать его перед подходами безопасно.
 */
export type WorkoutCreator = (workoutId: string, title: string) => Promise<ApiResult<unknown>>;

export type OutboxSyncDeps = {
  store: OutboxStore<WorkoutMutation>;
  send: LogSetSender;
  /** Без них очередь несёт только записи — правки останутся лежать. */
  editSet?: EditSetSender;
  deleteSet?: DeleteSetSender;
  /**
   * Тренировка обязана существовать на сервере раньше своих подходов: иначе
   * запись подхода получает 404 и уходит в «мёртвые». Без этой пары очередь
   * молча теряла бы всё, что записано в начатой офлайн тренировке.
   */
  createWorkout?: WorkoutCreator;
  registry?: WorkoutRegistry;
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
  /** Уведены в «мёртвые» на этом проходе: клиентская ошибка, которая не пройдёт и после повтора. */
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
  /** Запись подхода — самый частый случай, поэтому у него своя дверь. */
  enqueue: (userId: string, input: WorkoutSetInput, now?: Date) => Promise<OutboxRecord<WorkoutMutation>>;
  /** Любая мутация: правка и удаление приходят сюда. */
  enqueueMutation: (
    userId: string,
    id: string,
    mutation: WorkoutMutation,
    now?: Date
  ) => Promise<OutboxRecord<WorkoutMutation>>;
  /** Меняет ещё не отправленную запись прямо в очереди. */
  amendPending: (
    userId: string,
    id: string,
    patch: { weightKg: number; repetitions: number; rir: number }
  ) => Promise<boolean>;
  flush: (userId: string | null) => Promise<FlushSummary>;
  status: (userId: string | null) => Promise<OutboxSyncStatus>;
  /** Очередь и «мёртвые» элементы пользователя в порядке записи. */
  list: (userId: string | null) => Promise<OutboxRecord<WorkoutMutation>[]>;
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

/**
 * Вид мутации, который некому отправить. Это ошибка проводки приложения, а не
 * сервера: элемент уходит в «мёртвые», чтобы не крутиться в очереди вечно.
 */
const unsupported = (kind: string): ApiResult<never> => ({
  ok: false,
  error: { kind: "client", status: 501, code: "internal_error", message: `no sender for ${kind}`, details: [] }
});

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

  /**
   * Создаёт тренировку на сервере, если она ещё не подтверждена.
   * Возвращает null, когда путь свободен, и ошибку — когда подходы этой
   * тренировки отправлять пока нельзя.
   */
  async function ensureWorkout(userId: string, workoutId: string): Promise<{ ok: false; error: ApiError } | null> {
    if (!deps.createWorkout || !deps.registry) return null;
    const entry = await deps.registry.get(userId, workoutId);
    if (entry?.created) return null;
    const result = await deps.createWorkout(workoutId, entry?.title ?? "");
    if (result.ok) {
      await deps.registry.markCreated(userId, workoutId);
      return null;
    }
    return result;
  }

  /**
   * Отправляет одну мутацию. Все успешные исходы — created, duplicate,
   * updated, gone, deleted — означают одно: сервер сошёлся с нами, элемент
   * снимается. Различать их важно только для отчёта, не для решения.
   */
  async function dispatch(
    mutation: WorkoutMutation,
    mutationId: string
  ): Promise<ApiResult<{ outcome: string }>> {
    switch (mutation.kind) {
      case "log-set":
        return deps.send(mutation.workoutId, toApiSetInput(mutation.input));
      case "edit-set":
        if (!deps.editSet) return unsupported("edit-set");
        return deps.editSet(mutation.workoutId, mutation.setId, mutation.patch, mutationId);
      case "delete-set":
        if (!deps.deleteSet) return unsupported("delete-set");
        return deps.deleteSet(mutation.workoutId, mutation.setId, mutationId);
    }
  }

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

      const mutation = record.payload;

      // Тренировку нужно создать только перед записью подхода: правка и
      // удаление ссылаются на подход, который на сервере уже есть.
      const blocked = needsWorkout(mutation) ? await ensureWorkout(userId, workoutIdOf(mutation)) : null;
      const result = blocked ?? (await dispatch(mutation, record.id));

      if (result.ok) {
        await deps.store.remove(userId, record.id);
        // duplicate и gone: сервер уже в нужном состоянии. Это успех, а не
        // повод перепосылать — иначе очередь не опустеет никогда.
        if (result.value.outcome === "duplicate" || result.value.outcome === "gone") summary.duplicates += 1;
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

      // Любая клиентская ошибка — мутация не станет валидной от повтора.
      //
      // 409 здесь исключать нельзя: успешные конфликты (запись, которую сервер
      // уже принял; правка, которая уже применена; подход, который уже удалён)
      // клиент разбирает раньше и отдаёт как успех. Значит 409, доехавший
      // сюда, — настоящий отказ вроде отменённой тренировки, и ретраить его
      // означает крутить элемент в очереди вечно.
      if (error.kind === "client") {
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
      deps.store.append({
        id: input.clientMutationId,
        userId,
        createdAt: at.toISOString(),
        payload: { kind: "log-set", workoutId: input.workoutId, input }
      } satisfies OutboxItem<WorkoutMutation>),

    enqueueMutation: (userId, id, mutation, at = now()) =>
      deps.store.append({ id, userId, createdAt: at.toISOString(), payload: mutation } satisfies OutboxItem<WorkoutMutation>),

    // Правка записи, которая ещё не уехала: серверу нечего править — он этого
    // подхода не видел, идентификатора не существует. Меняем то, что лежит.
    amendPending: async (userId, id, patch) => {
      const records = await deps.store.listForUser(userId);
      const target = records.find((record) => record.id === id && record.state === "pending");
      if (!target || target.payload.kind !== "log-set") return false;
      await deps.store.update({
        ...target,
        payload: { ...target.payload, input: { ...target.payload.input, ...patch } }
      });
      return true;
    },

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
      const doomed = records.filter((record) => workoutIdOf(record.payload) === workoutId);
      for (const record of doomed) await deps.store.remove(userId, record.id);
      return doomed.length;
    },

    purgeUser: async (userId) => {
      await deps.store.purgeUser(userId);
    }
  };
}
