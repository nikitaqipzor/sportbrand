/**
 * Типы границы, зеркалящие services/api/api/openapi.yaml (версия 0.2.0).
 * Контракт — источник истины; менять эти типы можно только вслед за ним.
 */

export type HealthStatus = "ok" | "degraded";
export type DatabaseStatus = "up" | "down";

export type Health = {
  status: HealthStatus;
  database: DatabaseStatus;
  version?: string;
  time: string;
};

export type Credentials = {
  email: string;
  password: string;
};

export type User = {
  id: string;
  email: string;
  createdAt: string;
};

/** Ответ /auth/login, /auth/register и /auth/refresh. */
export type Session = {
  user: User;
  accessToken: string;
  tokenType: "Bearer";
  /** Время жизни access-токена в секундах (по умолчанию сервера — 900). */
  expiresIn: number;
  refreshToken: string;
  refreshExpiresIn: number;
};

export type WorkoutStatus = "active" | "paused" | "cancelled" | "completed";

export type Workout = {
  id: string;
  title: string;
  status: WorkoutStatus;
  createdAt: string;
  /** Добавлены в контракте 0.3.0 — аддитивно, старые ответы их не несли. */
  updatedAt?: string;
  /** Момент завершения; есть ровно у терминальных статусов. */
  endedAt?: string | null;
};

/**
 * Тело POST /workouts/{workoutId}/sets. Границы совпадают с validateSet()
 * из packages/domain; workoutId живёт в пути, а не в теле.
 */
export type WorkoutSetInput = {
  exerciseId: string;
  setNumber: number;
  weightKg: number;
  repetitions: number;
  rir: number;
  clientMutationId: string;
};

export type WorkoutSet = {
  id: string;
  workoutId: string;
  exerciseId: string;
  setNumber: number;
  weightKg: number;
  repetitions: number;
  rir: number;
  clientMutationId: string;
  createdAt: string;
};

/** Коды ошибок из контракта. Неизвестный код не ломает клиент. */
export type KnownErrorCode =
  | "invalid_request"
  | "validation_failed"
  | "unauthorized"
  | "invalid_credentials"
  | "email_taken"
  | "not_found"
  | "duplicate_client_mutation"
  | "rate_limited"
  | "internal_error";

export type ErrorCode = KnownErrorCode | (string & {});

export type ErrorDetail = { field: string; message: string };

export type ErrorPayload = {
  code: ErrorCode;
  message: string;
  details?: ErrorDetail[];
};

export type ErrorEnvelope = { error: ErrorPayload };

/**
 * Исход идемпотентной записи подхода. 409 — не ошибка: сервер уже принял эту
 * мутацию и возвращает ранее сохранённый подход, поэтому офлайн-очередь
 * обязана считать такой ответ успешным и снять элемент с отправки.
 */
export type LogSetOutcome =
  | { outcome: "created"; set: WorkoutSet }
  | { outcome: "duplicate"; set: WorkoutSet };

/**
 * Страница истории тренировок. nextCursor непрозрачен: клиент обязан вернуть
 * его нетронутым и никогда не собирать свой — курсор лишь двигает позицию
 * внутри строк владельца.
 */
export type WorkoutPage = { items: Workout[]; nextCursor: string | null };

export type WorkoutListQuery = {
  status?: WorkoutStatus[];
  from?: string;
  to?: string;
  limit?: number;
  cursor?: string;
};

/** Σ вес×повторы по подходам тренировки. */
export type WorkoutTotals = { sets: number; repetitions: number; volumeKg: number };

/** Тренировка со своими подходами — данные экрана «Итоги». */
export type WorkoutDetail = Workout & { sets: WorkoutSet[]; totals: WorkoutTotals };

/** Разрешённое окно, выровненное по целым ISO-неделям; to — верхняя граница исключительно. */
export type ProgressWindow = { from: string; to: string };

export type BestWeight = { weightKg: number; repetitions: number; achievedAt: string };

/** Оценка Эпли: weightKg × (1 + repetitions / 30). */
export type BestEstimated1Rm = {
  estimated1RmKg: number;
  weightKg: number;
  repetitions: number;
  achievedAt: string;
};

export type ExerciseRecord = {
  exerciseId: string;
  sets: number;
  repetitions: number;
  volumeKg: number;
  bestWeight: BestWeight;
  bestEstimated1Rm: BestEstimated1Rm;
  lastPerformedAt: string;
};

export type WeeklyVolume = {
  /** Понедельник 00:00 UTC соответствующей ISO-недели. */
  weekStart: string;
  sets: number;
  repetitions: number;
  volumeKg: number;
  workouts: number;
};

/** Чем закончились тренировки, НАЧАТЫЕ в этой неделе. */
export type WeeklyAdherence = {
  weekStart: string;
  started: number;
  completed: number;
  cancelled: number;
  inProgress: number;
  completionRate: number;
};

export type AdherenceTotals = {
  started: number;
  completed: number;
  cancelled: number;
  inProgress: number;
  completionRate: number;
  weeksInWindow: number;
  weeksWithTraining: number;
};

/** Данные экрана «Прогресс» одним раунд-трипом. */
export type Progress = {
  window: ProgressWindow;
  strength: ExerciseRecord[];
  weeklyVolume: WeeklyVolume[];
  adherence: { weeks: WeeklyAdherence[]; totals: AdherenceTotals };
};

export type ProgressQuery = { from?: string; to?: string; exerciseLimit?: number };
