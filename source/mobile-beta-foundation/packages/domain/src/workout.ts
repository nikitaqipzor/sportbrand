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
