import { validateSet, type WorkoutSetInput } from "@athletica/domain";

import { enqueue, type OutboxItem, type OutboxRecord } from "../../platform/offline/outbox.ts";
import type { WorkoutMutation } from "../../platform/offline/mutations.ts";
import type { OutboxSync } from "../../platform/offline/sync.ts";

export type LogSetResult =
  | { ok: true; outbox: OutboxItem<WorkoutSetInput>[] }
  | { ok: false; issues: string[] };

/**
 * Единственная точка записи подхода: доменная валидация обязана пройти
 * до того, как мутация попадёт в офлайн-очередь.
 */
export function logSet(
  outbox: OutboxItem<WorkoutSetInput>[],
  userId: string,
  input: WorkoutSetInput,
  now: Date = new Date()
): LogSetResult {
  const issues = validateSet(input);
  if (issues.length > 0) return { ok: false, issues };
  return {
    ok: true,
    outbox: enqueue(outbox, {
      id: input.clientMutationId,
      userId,
      createdAt: now.toISOString(),
      payload: input
    })
  };
}

export type SubmitSetResult =
  | { ok: true; record: OutboxRecord<WorkoutMutation> }
  | { ok: false; issues: string[] };

/**
 * То же правило, но поверх персистентной очереди: невалидный подход не
 * доезжает до хранилища, валидный сначала ложится на диск и только потом
 * уходит на сервер.
 */
export async function submitSet(
  sync: OutboxSync,
  userId: string,
  input: WorkoutSetInput,
  now: Date = new Date()
): Promise<SubmitSetResult> {
  const issues = validateSet(input);
  if (issues.length > 0) return { ok: false, issues };
  return { ok: true, record: await sync.enqueue(userId, input, now) };
}
