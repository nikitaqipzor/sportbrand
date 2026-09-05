import { applyWorkoutAction, type WorkoutAction, type WorkoutSetInput, type WorkoutStatus } from "@athletica/domain";

import { DEFAULT_EXERCISE_ID, exerciseTitle } from "./exercise-catalog.ts";

export type SetMeasures = { weightKg: number; repetitions: number; rir: number };

/**
 * Одно упражнение внутри тренировки. Нумерация подходов принадлежит именно
 * ему, а не тренировке: третий подход в жиме и третий подход в тяге — разные
 * подходы, и оба обязаны доехать до сервера.
 */
export type ActiveExercise = {
  exerciseId: string;
  title: string;
  /** Сколько подходов уже записано — источник номера следующего подхода. */
  completedSets: number;
  lastSetAt: string | null;
  lastSet: SetMeasures | null;
};

/**
 * Снимок активной тренировки: то, что должно пережить сворачивание и
 * убийство приложения посреди подхода. Хранится по пользователю и стирается
 * при выходе вместе с очередью (находка H1).
 *
 * Тренировка — это список упражнений: реальная силовая сессия не бывает из
 * одного движения. Снимок несёт их все со своими счётчиками, поэтому после
 * перезапуска нумерация каждого упражнения продолжается с того же места, а
 * clientMutationId повторённого подхода совпадает с прежним.
 */
export type ActiveWorkout = {
  workoutId: string;
  title: string;
  status: WorkoutStatus;
  startedAt: string;
  exercises: ActiveExercise[];
  /** Упражнение, открытое на экране; всегда есть в exercises. */
  currentExerciseId: string;
  /** Момент последнего подхода тренировки: от него идёт таймер отдыха. */
  lastSetAt: string | null;
  lastSet: SetMeasures | null;
};

export type ExerciseSeed = { exerciseId: string; title?: string };

export type StartWorkoutInput = {
  workoutId: string;
  title: string;
  /** Одно упражнение — короткая запись для `exercises: [{ exerciseId }]`. */
  exerciseId?: string;
  exercises?: readonly ExerciseSeed[];
};

const blankExercise = (seed: ExerciseSeed): ActiveExercise => ({
  exerciseId: seed.exerciseId,
  title: seed.title ?? exerciseTitle(seed.exerciseId),
  completedSets: 0,
  lastSetAt: null,
  lastSet: null
});

/** Первые упражнения тренировки: явный список, одиночный id или дефолт. */
function seedExercises(input: StartWorkoutInput): ActiveExercise[] {
  const seeds =
    input.exercises && input.exercises.length > 0
      ? input.exercises
      : [{ exerciseId: input.exerciseId ?? DEFAULT_EXERCISE_ID }];
  const seen = new Set<string>();
  const exercises: ActiveExercise[] = [];
  for (const seed of seeds) {
    if (seen.has(seed.exerciseId)) continue;
    seen.add(seed.exerciseId);
    exercises.push(blankExercise(seed));
  }
  return exercises;
}

export function startActiveWorkout(input: StartWorkoutInput, now: Date = new Date()): ActiveWorkout {
  const exercises = seedExercises(input);
  return {
    workoutId: input.workoutId,
    title: input.title,
    status: "active",
    startedAt: now.toISOString(),
    exercises,
    currentExerciseId: exercises[0]?.exerciseId ?? DEFAULT_EXERCISE_ID,
    lastSetAt: null,
    lastSet: null
  };
}

export const findExercise = (workout: ActiveWorkout, exerciseId: string): ActiveExercise | null =>
  workout.exercises.find((exercise) => exercise.exerciseId === exerciseId) ?? null;

/** Открытое упражнение. Если снимок указывает на выпавшее — берём первое. */
export function currentExercise(workout: ActiveWorkout): ActiveExercise {
  return (
    findExercise(workout, workout.currentExerciseId) ??
    workout.exercises[0] ??
    blankExercise({ exerciseId: workout.currentExerciseId })
  );
}

/**
 * Номер следующего подхода — свой у каждого упражнения. Общий счётчик
 * тренировки означал бы, что второе упражнение начинается с подхода №5 и
 * сервер не может отличить его подходы от подходов первого.
 */
export const nextSetNumber = (workout: ActiveWorkout, exerciseId: string = workout.currentExerciseId): number =>
  (findExercise(workout, exerciseId)?.completedSets ?? 0) + 1;

/**
 * Идентификатор мутации детерминирован: тренировка + упражнение + номер
 * подхода. Повтор того же подхода после перезапуска даст тот же id, а значит
 * сервер узнает его и ответит 409 вместо второй записи (ADR 0002).
 */
export const mutationIdFor = (
  workout: ActiveWorkout,
  setNumber: number,
  exerciseId: string = workout.currentExerciseId
): string => `${workout.workoutId}:${exerciseId}:${setNumber}`;

export function buildSetInput(
  workout: ActiveWorkout,
  measures: SetMeasures,
  setNumber?: number,
  exerciseId: string = workout.currentExerciseId
): WorkoutSetInput {
  const number = setNumber ?? nextSetNumber(workout, exerciseId);
  return {
    workoutId: workout.workoutId,
    exerciseId,
    setNumber: number,
    weightKg: measures.weightKg,
    repetitions: measures.repetitions,
    rir: measures.rir,
    clientMutationId: mutationIdFor(workout, number, exerciseId)
  };
}

/** Добавляет упражнение и делает его текущим; повтор ничего не ломает. */
export function withExercise(workout: ActiveWorkout, seed: ExerciseSeed): ActiveWorkout {
  const existing = findExercise(workout, seed.exerciseId);
  return {
    ...workout,
    exercises: existing ? workout.exercises : [...workout.exercises, blankExercise(seed)],
    currentExerciseId: seed.exerciseId
  };
}

/** Переключение между упражнениями. Неизвестное — молча игнорируется. */
export function withCurrentExercise(workout: ActiveWorkout, exerciseId: string): ActiveWorkout {
  if (!findExercise(workout, exerciseId)) return workout;
  return { ...workout, currentExerciseId: exerciseId };
}

/** Подход записан — счётчик своего упражнения сдвигается на следующий номер. */
export function withRecordedSet(
  workout: ActiveWorkout,
  input: WorkoutSetInput,
  now: Date = new Date()
): ActiveWorkout {
  const at = now.toISOString();
  const measures: SetMeasures = {
    weightKg: input.weightKg,
    repetitions: input.repetitions,
    rir: input.rir
  };
  const touched = findExercise(workout, input.exerciseId) !== null;
  const exercises = touched
    ? workout.exercises.map((exercise) =>
        exercise.exerciseId === input.exerciseId
          ? {
              ...exercise,
              completedSets: Math.max(exercise.completedSets, input.setNumber),
              lastSetAt: at,
              lastSet: measures
            }
          : exercise
      )
    : [
        ...workout.exercises,
        { ...blankExercise({ exerciseId: input.exerciseId }), completedSets: input.setNumber, lastSetAt: at, lastSet: measures }
      ];

  return { ...workout, exercises, lastSetAt: at, lastSet: measures };
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

/** Сколько подходов записано во всей тренировке. */
export const totalCompletedSets = (workout: ActiveWorkout): number =>
  workout.exercises.reduce((sum, exercise) => sum + exercise.completedSets, 0);

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const asMeasures = (value: unknown): SetMeasures | null => {
  if (!isObject(value)) return null;
  const { weightKg, repetitions, rir } = value;
  if (typeof weightKg !== "number" || typeof repetitions !== "number" || typeof rir !== "number") return null;
  return { weightKg, repetitions, rir };
};

const asString = (value: unknown): string | null => (typeof value === "string" && value !== "" ? value : null);

function reviveExercise(raw: unknown): ActiveExercise | null {
  if (!isObject(raw)) return null;
  const exerciseId = asString(raw["exerciseId"]);
  if (!exerciseId) return null;
  const completedSets = typeof raw["completedSets"] === "number" ? raw["completedSets"] : 0;
  return {
    exerciseId,
    title: asString(raw["title"]) ?? exerciseTitle(exerciseId),
    completedSets: Math.max(0, Math.trunc(completedSets)),
    lastSetAt: asString(raw["lastSetAt"]),
    lastSet: asMeasures(raw["lastSet"])
  };
}

/**
 * Чтение снимка с диска.
 *
 * Снимок мог быть записан прежней версией приложения, где тренировка была
 * ровно одним упражнением. Такой снимок поднимается как тренировка с одним
 * упражнением, а не выбрасывается: пользователь, обновившийся посреди
 * тренировки, обязан вернуться в неё, а не потерять записанное.
 */
export function reviveActiveWorkout(raw: unknown): ActiveWorkout | null {
  if (!isObject(raw)) return null;
  const workoutId = asString(raw["workoutId"]);
  const startedAt = asString(raw["startedAt"]);
  if (!workoutId || !startedAt) return null;

  const rawExercises = Array.isArray(raw["exercises"]) ? raw["exercises"] : [];
  let exercises = rawExercises
    .map(reviveExercise)
    .filter((exercise): exercise is ActiveExercise => exercise !== null);

  if (exercises.length === 0) {
    // Снимок старого формата: одно упражнение и счётчик на самой тренировке.
    const legacyId = asString(raw["exerciseId"]) ?? DEFAULT_EXERCISE_ID;
    const legacySets = typeof raw["completedSets"] === "number" ? Math.max(0, Math.trunc(raw["completedSets"])) : 0;
    exercises = [
      {
        exerciseId: legacyId,
        title: exerciseTitle(legacyId),
        completedSets: legacySets,
        lastSetAt: asString(raw["lastSetAt"]),
        lastSet: asMeasures(raw["lastSet"])
      }
    ];
  }

  const first = exercises[0] as ActiveExercise;
  const current = asString(raw["currentExerciseId"]) ?? asString(raw["exerciseId"]) ?? first.exerciseId;
  const status = raw["status"];
  return {
    workoutId,
    title: asString(raw["title"]) ?? "",
    status:
      status === "active" || status === "paused" || status === "cancelled" || status === "completed"
        ? status
        : "active",
    startedAt,
    exercises,
    currentExerciseId: exercises.some((exercise) => exercise.exerciseId === current) ? current : first.exerciseId,
    lastSetAt: asString(raw["lastSetAt"]),
    lastSet: asMeasures(raw["lastSet"])
  };
}

/** Незавершённая тренировка, в которую можно вернуться (П3). */
export const isResumable = (workout: ActiveWorkout | null): workout is ActiveWorkout =>
  workout !== null && (workout.status === "active" || workout.status === "paused");

export type WorkoutActionResult = { ok: true; workout: ActiveWorkout } | { ok: false; reason: string };

/** Переходы статуса — из домена; экран отвечает только за подтверждение. */
export function actOnWorkout(workout: ActiveWorkout, action: WorkoutAction): WorkoutActionResult {
  const transition = applyWorkoutAction(workout.status, action);
  if (!transition.ok) return { ok: false, reason: transition.reason };
  return { ok: true, workout: { ...workout, status: transition.status } };
}
