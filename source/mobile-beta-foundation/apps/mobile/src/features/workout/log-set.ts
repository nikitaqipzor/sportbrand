import { validateSet, type WorkoutSetInput } from "@athletica/domain";

import { enqueue, type OutboxItem } from "../../platform/offline/outbox.ts";

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
