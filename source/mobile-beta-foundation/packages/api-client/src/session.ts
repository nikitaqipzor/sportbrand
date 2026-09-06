import { createApiClient, type ApiClient, type ApiClientOptions } from "./endpoints.ts";
import { fail, ok, redactSecrets, type ApiError, type ApiResult } from "./errors.ts";
import type {
  Credentials,
  DeleteSetOutcome,
  EditSetOutcome,
  ExerciseCard,
  ExerciseDictionary,
  ExercisePage,
  ExerciseQuery,
  LogSetOutcome,
  Progress,
  ProgressQuery,
  Session,
  User,
  Workout,
  WorkoutDetail,
  WorkoutListQuery,
  WorkoutPage,
  WorkoutSet,
  WorkoutSetInput,
  WorkoutSetPatch,
  WorkoutStatus
} from "./types.ts";

/** Почему сессия закончилась. Для аналитики и для очистки локальных данных. */
export type SignOutReason = "user" | "refresh_failed";

/** Наблюдаемые события сессии: молча падать нельзя (QA-001). */
export type SessionEvent =
  | { type: "signed_in"; session: Session }
  | { type: "refreshed"; session: Session }
  | { type: "signed_out"; reason: SignOutReason; userId: string | null; error?: ApiError };

export type SessionListener = (event: SessionEvent) => void;

/**
 * Хранилище сессии. Интерфейс намеренно узкий: в приложении за ним стоит
 * expo-secure-store, в тестах — объект в памяти.
 */
export type SessionStore = {
  load: () => Promise<Session | null>;
  save: (session: Session) => Promise<void>;
  clear: () => Promise<void>;
};

export type AuthClientOptions = Omit<ApiClientOptions, "secrets"> & {
  store: SessionStore;
};

export type AuthClient = {
  api: ApiClient;
  /** Текущая сессия в памяти; null — пользователь не авторизован. */
  getSession: () => Session | null;
  /** Поднимает сессию из защищённого хранилища при старте приложения. */
  restore: () => Promise<Session | null>;
  signIn: (credentials: Credentials) => Promise<ApiResult<Session>>;
  signUp: (credentials: Credentials) => Promise<ApiResult<Session>>;
  signOut: (reason?: SignOutReason) => Promise<void>;
  /** Принудительное обновление, тоже single-flight. */
  refreshNow: () => Promise<ApiResult<Session>>;
  subscribe: (listener: SessionListener) => () => void;
  me: () => Promise<ApiResult<User>>;
  createWorkout: (input?: { id?: string; title?: string }) => Promise<ApiResult<Workout>>;
  logSet: (workoutId: string, input: WorkoutSetInput) => Promise<ApiResult<LogSetOutcome>>;
  listSets: (workoutId: string) => Promise<ApiResult<WorkoutSet[]>>;
  listWorkouts: (query?: WorkoutListQuery) => Promise<ApiResult<WorkoutPage>>;
  getWorkout: (workoutId: string) => Promise<ApiResult<WorkoutDetail>>;
  setWorkoutStatus: (workoutId: string, status: WorkoutStatus) => Promise<ApiResult<Workout>>;
  progress: (query?: ProgressQuery) => Promise<ApiResult<Progress>>;
  editSet: (workoutId: string, setId: string, patch: WorkoutSetPatch) => Promise<ApiResult<EditSetOutcome>>;
  deleteSet: (workoutId: string, setId: string, clientMutationId: string) => Promise<ApiResult<DeleteSetOutcome>>;
  listExercises: (query?: ExerciseQuery) => Promise<ApiResult<ExercisePage>>;
  getExercise: (exerciseId: string) => Promise<ApiResult<ExerciseCard>>;
  exerciseDictionaries: () => Promise<ApiResult<ExerciseDictionary[]>>;
};

const isUnauthorized = (error: ApiError): boolean => error.kind === "client" && error.status === 401;

/**
 * Сессия с single-flight обновлением access-токена.
 *
 * Access живёт 15 минут, поэтому 401 посреди тренировки — норма, а не сбой.
 * Если несколько запросов получили 401 одновременно, /auth/refresh вызывается
 * ровно один раз: остальные ждут тот же промис и повторяются с новым токеном.
 * Это критично ещё и потому, что refresh-токен ротируется — параллельные
 * обновления сожгли бы семейство токенов и выкинули пользователя.
 */
export function createAuthClient(options: AuthClientOptions): AuthClient {
  let current: Session | null = null;
  let inflightRefresh: Promise<ApiResult<Session>> | null = null;
  const listeners = new Set<SessionListener>();

  const api = createApiClient({
    ...options,
    secrets: () => [current?.accessToken, current?.refreshToken]
  });

  const emit = (event: SessionEvent): void => {
    for (const listener of [...listeners]) listener(event);
  };

  const secrets = (): readonly (string | null | undefined)[] => [current?.accessToken, current?.refreshToken];

  async function adopt(session: Session, type: "signed_in" | "refreshed"): Promise<Session> {
    current = session;
    await options.store.save(session);
    emit({ type, session });
    return session;
  }

  async function signOut(reason: SignOutReason = "user", error?: ApiError): Promise<void> {
    const userId = current?.user.id ?? null;
    current = null;
    inflightRefresh = null;
    await options.store.clear();
    emit({ type: "signed_out", reason, userId, ...(error ? { error } : {}) });
  }

  async function performRefresh(refreshToken: string): Promise<ApiResult<Session>> {
    const result = await api.refresh(refreshToken);
    if (result.ok) return ok(await adopt(result.value, "refreshed"));
    // Обновиться не удалось — сессия закончилась, и это видимое событие.
    await signOut("refresh_failed", result.error);
    return fail({
      kind: "session_expired",
      message: redactSecrets(`не удалось обновить сессию: ${result.error.message}`, secrets())
    });
  }

  /**
   * Ровно одно обновление на любое количество параллельных 401. Если токен уже
   * успели обновить, вызывающий просто получает свежую сессию без запроса.
   */
  function refreshOnce(staleAccessToken: string | null): Promise<ApiResult<Session>> {
    if (current && staleAccessToken !== null && current.accessToken !== staleAccessToken) {
      return Promise.resolve(ok(current));
    }
    if (inflightRefresh) return inflightRefresh;
    const refreshToken = current?.refreshToken;
    if (!refreshToken) {
      return signOut("refresh_failed").then(() =>
        fail<Session>({ kind: "session_expired", message: "нет refresh-токена" })
      );
    }
    const started = performRefresh(refreshToken).finally(() => {
      if (inflightRefresh === started) inflightRefresh = null;
    });
    inflightRefresh = started;
    return started;
  }

  /** Один повтор с обновлённым токеном; второй 401 означает конец сессии. */
  async function authorized<T>(call: (accessToken: string) => Promise<ApiResult<T>>): Promise<ApiResult<T>> {
    const session = current;
    if (!session) return fail({ kind: "session_expired", message: "нет активной сессии" });

    const first = await call(session.accessToken);
    if (first.ok || !isUnauthorized(first.error)) return first;

    const refreshed = await refreshOnce(session.accessToken);
    if (!refreshed.ok) return fail(refreshed.error);

    const second = await call(refreshed.value.accessToken);
    if (!second.ok && isUnauthorized(second.error)) {
      await signOut("refresh_failed", second.error);
      return fail({ kind: "session_expired", message: "сервер отклонил обновлённый токен" });
    }
    return second;
  }

  const startSession = async (result: ApiResult<Session>): Promise<ApiResult<Session>> =>
    result.ok ? ok(await adopt(result.value, "signed_in")) : result;

  return {
    api,
    getSession: () => current,

    restore: async () => {
      const stored = await options.store.load();
      current = stored;
      if (stored) emit({ type: "signed_in", session: stored });
      return current;
    },

    signIn: async (credentials) => startSession(await api.login(credentials)),
    signUp: async (credentials) => startSession(await api.register(credentials)),
    signOut: (reason: SignOutReason = "user") => signOut(reason),
    refreshNow: () => refreshOnce(null),

    subscribe: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },

    me: () => authorized((token) => api.me(token)),
    createWorkout: (input) => authorized((token) => api.createWorkout(token, input)),
    logSet: (workoutId, input) => authorized((token) => api.logSet(token, workoutId, input)),
    listSets: (workoutId) => authorized((token) => api.listSets(token, workoutId)),
    listWorkouts: (query) => authorized((token) => api.listWorkouts(token, query)),
    getWorkout: (workoutId) => authorized((token) => api.getWorkout(token, workoutId)),
    setWorkoutStatus: (workoutId, status) => authorized((token) => api.setWorkoutStatus(token, workoutId, status)),
    progress: (query) => authorized((token) => api.progress(token, query)),
    editSet: (workoutId, setId, patch) => authorized((token) => api.editSet(token, workoutId, setId, patch)),
    deleteSet: (workoutId, setId, mutationId) =>
      authorized((token) => api.deleteSet(token, workoutId, setId, mutationId)),
    listExercises: (query) => authorized((token) => api.listExercises(token, query)),
    getExercise: (exerciseId) => authorized((token) => api.getExercise(token, exerciseId)),
    exerciseDictionaries: () => authorized((token) => api.exerciseDictionaries(token))
  };
}
