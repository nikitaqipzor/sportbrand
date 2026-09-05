export type WorkoutStatus = "active" | "paused" | "cancelled" | "completed";

export type WorkoutSetInput = {
  workoutId: string;
  exerciseId: string;
  setNumber: number;
  weightKg: number;
  repetitions: number;
  rir: number;
  clientMutationId: string;
};

export function validateSet(input: WorkoutSetInput): string[] {
  const issues: string[] = [];
  if (!input.workoutId || !input.exerciseId || !input.clientMutationId) issues.push("required identifier missing");
  if (!Number.isInteger(input.setNumber) || input.setNumber < 1) issues.push("set number must be positive");
  if (!Number.isFinite(input.weightKg) || input.weightKg < 0 || input.weightKg > 1000) issues.push("weight must be between 0 and 1000 kg");
  if (!Number.isInteger(input.repetitions) || input.repetitions < 1 || input.repetitions > 100) issues.push("repetitions must be between 1 and 100");
  if (!Number.isInteger(input.rir) || input.rir < 0 || input.rir > 10) issues.push("RIR must be between 0 and 10");
  return issues;
}

/**
 * Действия над активной тренировкой. cancel — отдельное действие, а не
 * разновидность завершения: отменённая тренировка не попадает в статистику
 * (блокер QA-004: отменить тренировку было нельзя вообще).
 */
export type WorkoutAction = "pause" | "resume" | "cancel" | "complete";

/** Завершённая и отменённая тренировки неизменны: из них выхода нет. */
export function isTerminalStatus(status: WorkoutStatus): boolean {
  return status === "completed" || status === "cancelled";
}

/**
 * cancel и complete разрушительны и необратимы — экран обязан спросить
 * подтверждение перед вызовом (QA-009, QA-010).
 */
export function isDestructiveAction(action: WorkoutAction): boolean {
  return action === "cancel" || action === "complete";
}

export type WorkoutTransition =
  | { ok: true; status: WorkoutStatus }
  | { ok: false; reason: string };

export function applyWorkoutAction(status: WorkoutStatus, action: WorkoutAction): WorkoutTransition {
  if (isTerminalStatus(status)) return { ok: false, reason: "workout already finished" };
  switch (action) {
    case "pause":
      return status === "active" ? { ok: true, status: "paused" } : { ok: false, reason: "workout is not active" };
    case "resume":
      return status === "paused" ? { ok: true, status: "active" } : { ok: false, reason: "workout is not paused" };
    case "cancel":
      return { ok: true, status: "cancelled" };
    case "complete":
      return { ok: true, status: "completed" };
  }
}
