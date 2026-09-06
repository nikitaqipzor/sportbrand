import type { WorkoutSetInput } from "@athletica/domain";

/**
 * Виды мутаций тренировки, которые едут в очереди.
 *
 * Очередь несёт не «подходы», а изменения: запись, правку и удаление. Порядок
 * сохраняется по seq, поэтому правка физически не может уехать раньше своей
 * записи — она поставлена в очередь позже.
 */
export type WorkoutMutation =
  | { kind: "log-set"; workoutId: string; input: WorkoutSetInput }
  | {
      kind: "edit-set";
      workoutId: string;
      /** Идентификатор подхода НА СЕРВЕРЕ. Известен только после подтверждения. */
      setId: string;
      patch: { weightKg: number; repetitions: number; rir: number };
    }
  | { kind: "delete-set"; workoutId: string; setId: string };

export const workoutIdOf = (mutation: WorkoutMutation): string => mutation.workoutId;

/**
 * Тренировку нужно создать на сервере только перед записью подхода. Правка и
 * удаление ссылаются на подход, который там уже есть, — а значит и тренировка
 * тоже.
 */
export const needsWorkout = (mutation: WorkoutMutation): boolean => mutation.kind === "log-set";

/**
 * Правка подхода, который ещё не уехал на сервер, — это не мутация, а
 * исправление того, что лежит в очереди. Слать серверу правку записи, которой
 * он не видел, невозможно: идентификатора подхода ещё не существует.
 */
export function patchPendingSet(
  mutation: WorkoutMutation,
  patch: { weightKg: number; repetitions: number; rir: number }
): WorkoutMutation {
  if (mutation.kind !== "log-set") return mutation;
  return { ...mutation, input: { ...mutation.input, ...patch } };
}

/** Детерминированный идентификатор правки: тот же после перезапуска. */
export const editMutationId = (setId: string, revision: number): string => `edit:${setId}:${revision}`;

/** Удаление одного подхода — одно и то же намерение, сколько бы раз ни повторили. */
export const deleteMutationId = (setId: string): string => `delete:${setId}`;
