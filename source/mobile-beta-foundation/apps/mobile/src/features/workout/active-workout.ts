import { applyWorkoutAction, type WorkoutAction, type WorkoutSetInput, type WorkoutStatus } from "@athletica/domain";

/**
 * Снимок активной тренировки: то, что должно пережить сворачивание и
 * убийство приложения посреди подхода. Хранится по пользователю и стирается
 * при выходе вместе с очередью (находка H1).
 */
export type ActiveWorkout = {
  workoutId: string;
  title: string;
  exerciseId: string;
  status: WorkoutStatus;
  startedAt: string;
  /** Сколько подходов уже записано — источник номера следующего подхода. */
  completedSets: number;
  /** Момент последнего записанного подхода: от него идёт таймер отдыха. */
  lastSetAt: string | null;
  lastSet: { weightKg: number; repetitions: number; rir: number } | null;
};

export type StartWorkoutInput = { workoutId: string; title: string; exerciseId: string };

export function startActiveWorkout(input: StartWorkoutInput, now: Date = new Date()): ActiveWorkout {
  return {
    workoutId: input.workoutId,
    title: input.title,
    exerciseId: input.exerciseId,
    status: "active",
    startedAt: now.toISOString(),
    completedSets: 0,
    lastSetAt: null,
    lastSet: null
  };
}

export const nextSetNumber = (workout: ActiveWorkout): number => workout.completedSets + 1;

/**
 * Идентификатор мутации детерминирован: тренировка + упражнение + номер
 * подхода. Повтор того же подхода после перезапуска даст тот же id, а значит
 * сервер узнает его и ответит 409 вместо второй записи (ADR 0002).
 */
export const mutationIdFor = (workout: ActiveWorkout, setNumber: number): string =>
  `${workout.workoutId}:${workout.exerciseId}:${setNumber}`;

export function buildSetInput(
  workout: ActiveWorkout,
  measures: { weightKg: number; repetitions: number; rir: number },
  setNumber: number = nextSetNumber(workout)
): WorkoutSetInput {
  return {
    workoutId: workout.workoutId,
    exerciseId: workout.exerciseId,
    setNumber,
    weightKg: measures.weightKg,
    repetitions: measures.repetitions,
    rir: measures.rir,
    clientMutationId: mutationIdFor(workout, setNumber)
  };
}

/** Подход записан — снимок сдвигается на следующий номер. */
export function withRecordedSet(
  workout: ActiveWorkout,
  input: WorkoutSetInput,
  now: Date = new Date()
): ActiveWorkout {
  return {
    ...workout,
    completedSets: Math.max(workout.completedSets, input.setNumber),
    lastSetAt: now.toISOString(),
    lastSet: { weightKg: input.weightKg, repetitions: input.repetitions, rir: input.rir }
  };
}

/** Секунды отдыха: от последнего подхода, а до него — от начала тренировки. */
export function restSeconds(workout: ActiveWorkout, now: Date = new Date()): number {
  const since = Date.parse(workout.lastSetAt ?? workout.startedAt);
  return Math.max(0, Math.floor((now.getTime() - since) / 1000));
}

export function formatClock(totalSeconds: number): string {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
}

export type WorkoutActionResult = { ok: true; workout: ActiveWorkout } | { ok: false; reason: string };

/** Переходы статуса — из домена; экран отвечает только за подтверждение. */
export function actOnWorkout(workout: ActiveWorkout, action: WorkoutAction): WorkoutActionResult {
  const transition = applyWorkoutAction(workout.status, action);
  if (!transition.ok) return { ok: false, reason: transition.reason };
  return { ok: true, workout: { ...workout, status: transition.status } };
}
