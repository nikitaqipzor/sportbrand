import { createHttpClient, type HttpClient, type HttpClientOptions, type HttpSuccess } from "./http.ts";
import { fail, ok, type ApiResult } from "./errors.ts";
import type {
  Credentials,
  Health,
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
  WorkoutStatus
} from "./types.ts";

export type ApiClientOptions = HttpClientOptions;

const bearer = (accessToken: string): Record<string, string> => ({ authorization: `Bearer ${accessToken}` });

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

/** Ответ разобран как объект — иначе это сломанный сервер, а не наши данные. */
function asObject<T>(result: ApiResult<HttpSuccess>): ApiResult<{ status: number; body: T }> {
  if (!result.ok) return fail(result.error);
  if (!isObject(result.value.body)) {
    return fail({
      kind: "server",
      status: result.value.status,
      code: "internal_error",
      message: "response body is not a JSON object",
      attempts: 1
    });
  }
  return ok({ status: result.value.status, body: result.value.body as T });
}

export type ApiClient = {
  http: HttpClient;
  health: () => Promise<ApiResult<Health>>;
  register: (credentials: Credentials) => Promise<ApiResult<Session>>;
  login: (credentials: Credentials) => Promise<ApiResult<Session>>;
  refresh: (refreshToken: string) => Promise<ApiResult<Session>>;
  me: (accessToken: string) => Promise<ApiResult<User>>;
  /**
   * Создание идемпотентно, когда клиент сам называет тренировку: повтор с тем
   * же id возвращает сохранённую (200), а не заводит вторую сессию. Без этого
   * тренировку нельзя начать без связи.
   */
  createWorkout: (accessToken: string, input?: { id?: string; title?: string }) => Promise<ApiResult<Workout>>;
  logSet: (accessToken: string, workoutId: string, input: WorkoutSetInput) => Promise<ApiResult<LogSetOutcome>>;
  listSets: (accessToken: string, workoutId: string) => Promise<ApiResult<WorkoutSet[]>>;
  logout: (refreshToken: string, allSessions?: boolean) => Promise<ApiResult<void>>;
  logoutAll: (accessToken: string) => Promise<ApiResult<void>>;
  listWorkouts: (accessToken: string, query?: WorkoutListQuery) => Promise<ApiResult<WorkoutPage>>;
  getWorkout: (accessToken: string, workoutId: string) => Promise<ApiResult<WorkoutDetail>>;
  setWorkoutStatus: (accessToken: string, workoutId: string, status: WorkoutStatus) => Promise<ApiResult<Workout>>;
  progress: (accessToken: string, query?: ProgressQuery) => Promise<ApiResult<Progress>>;
};

/**
 * Строка запроса из необязательных параметров. Курсор проходит насквозь,
 * закодированный, — клиент его не разбирает и не собирает.
 */
function queryString(params: Record<string, string | number | string[] | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined) continue;
    if (Array.isArray(value)) {
      for (const entry of value) search.append(key, entry);
      continue;
    }
    search.append(key, String(value));
  }
  const rendered = search.toString();
  return rendered ? `?${rendered}` : "";
}

/**
 * Методы контракта поверх HTTP-клиента. Токен передаётся явным аргументом:
 * хранением и обновлением сессии занимается createAuthClient.
 */
export function createApiClient(options: ApiClientOptions): ApiClient {
  const http = createHttpClient(options);

  const health = async (): Promise<ApiResult<Health>> => {
    // 503 — это заполненное тело Health со status: degraded, а не сбой связи,
    // поэтому ретраить его бессмысленно.
    const result = asObject<Health>(
      await http.request({ method: "GET", path: "/health", acceptStatuses: [503], retry: false })
    );
    return result.ok ? ok(result.value.body) : fail(result.error);
  };

  const session = async (path: string, body: unknown): Promise<ApiResult<Session>> => {
    // Запись без идемпотентности (регистрация, вход, ротация refresh) не
    // ретраится: повтор после обрыва может потратить токен или создать счёт.
    const result = asObject<Session>(await http.request({ method: "POST", path, body, retry: false }));
    return result.ok ? ok(result.value.body) : fail(result.error);
  };

  return {
    http,
    health,
    register: (credentials) => session("/auth/register", credentials),
    login: (credentials) => session("/auth/login", credentials),
    refresh: (refreshToken) => session("/auth/refresh", { refreshToken }),

    me: async (accessToken) => {
      const result = asObject<User>(
        await http.request({ method: "GET", path: "/auth/me", headers: bearer(accessToken) })
      );
      return result.ok ? ok(result.value.body) : fail(result.error);
    },

    createWorkout: async (accessToken, input = {}) => {
      const result = asObject<Workout>(
        await http.request({
          method: "POST",
          path: "/workouts",
          body: input,
          headers: bearer(accessToken),
          // Названная клиентом тренировка идемпотентна — повтор после обрыва
          // вернёт ту же сессию, поэтому ретрай безопасен. Безымянная завела бы
          // вторую тренировку, и ретраить её нельзя.
          retry: input.id !== undefined
        })
      );
      return result.ok ? ok(result.value.body) : fail(result.error);
    },

    logSet: async (accessToken, workoutId, input) => {
      // 409 duplicate_client_mutation — успешный исход: сервер уже принял эту
      // мутацию и вернул сохранённый подход. Ретраи здесь безопасны, потому
      // что идемпотентность гарантирует уникальный индекс в базе.
      const result = asObject<Record<string, unknown>>(
        await http.request({
          method: "POST",
          path: `/workouts/${encodeURIComponent(workoutId)}/sets`,
          body: input,
          headers: bearer(accessToken),
          acceptStatuses: [409]
        })
      );
      if (!result.ok) return fail(result.error);
      if (result.value.status === 409) {
        const stored = result.value.body["set"];
        if (!isObject(stored)) {
          return fail({
            kind: "server",
            status: 409,
            code: "duplicate_client_mutation",
            message: "409 without the stored set",
            attempts: 1
          });
        }
        return ok({ outcome: "duplicate", set: stored as unknown as WorkoutSet });
      }
      return ok({ outcome: "created", set: result.value.body as unknown as WorkoutSet });
    },

    listSets: async (accessToken, workoutId) => {
      const result = asObject<{ items?: WorkoutSet[] }>(
        await http.request({
          method: "GET",
          path: `/workouts/${encodeURIComponent(workoutId)}/sets`,
          headers: bearer(accessToken)
        })
      );
      if (!result.ok) return fail(result.error);
      return ok(Array.isArray(result.value.body.items) ? result.value.body.items : []);
    },

    // Выход намеренно не требует access-токена: выходящий клиент вполне может
    // держать протухший. Сервер отвечает 204 на любой вход, поэтому неизвестный
    // и живой токен неотличимы — эндпоинт не работает оракулом.
    logout: async (refreshToken, allSessions = false) => {
      const result = await http.request({
        method: "POST",
        path: "/auth/logout",
        body: { refreshToken, allSessions },
        retry: false
      });
      return result.ok ? ok(undefined) : fail(result.error);
    },

    logoutAll: async (accessToken) => {
      const result = await http.request({
        method: "POST",
        path: "/auth/logout-all",
        headers: bearer(accessToken),
        retry: false
      });
      return result.ok ? ok(undefined) : fail(result.error);
    },

    listWorkouts: async (accessToken, query = {}) => {
      const result = asObject<WorkoutPage>(
        await http.request({
          method: "GET",
          path: `/workouts${queryString({
            status: query.status,
            from: query.from,
            to: query.to,
            limit: query.limit,
            cursor: query.cursor
          })}`,
          headers: bearer(accessToken)
        })
      );
      if (!result.ok) return fail(result.error);
      const body = result.value.body;
      return ok({
        items: Array.isArray(body.items) ? body.items : [],
        nextCursor: typeof body.nextCursor === "string" ? body.nextCursor : null
      });
    },

    getWorkout: async (accessToken, workoutId) => {
      const result = asObject<WorkoutDetail>(
        await http.request({
          method: "GET",
          path: `/workouts/${encodeURIComponent(workoutId)}`,
          headers: bearer(accessToken)
        })
      );
      if (!result.ok) return fail(result.error);
      const body = result.value.body;
      return ok({
        ...body,
        sets: Array.isArray(body.sets) ? body.sets : [],
        totals: body.totals ?? { sets: 0, repetitions: 0, volumeKg: 0 }
      });
    },

    // Недопустимый переход — 409 invalid_transition, обычная клиентская
    // ошибка: экран показывает причину, а не считает это сбоем связи.
    setWorkoutStatus: async (accessToken, workoutId, status) => {
      const result = asObject<Workout>(
        await http.request({
          method: "POST",
          path: `/workouts/${encodeURIComponent(workoutId)}/status`,
          headers: bearer(accessToken),
          body: { status },
          retry: false
        })
      );
      return result.ok ? ok(result.value.body) : fail(result.error);
    },

    progress: async (accessToken, query = {}) => {
      const result = asObject<Progress>(
        await http.request({
          method: "GET",
          path: `/progress${queryString({ from: query.from, to: query.to, exerciseLimit: query.exerciseLimit })}`,
          headers: bearer(accessToken)
        })
      );
      if (!result.ok) return fail(result.error);
      const body = result.value.body;
      return ok({
        ...body,
        strength: Array.isArray(body.strength) ? body.strength : [],
        weeklyVolume: Array.isArray(body.weeklyVolume) ? body.weeklyVolume : [],
        adherence: {
          weeks: Array.isArray(body.adherence?.weeks) ? body.adherence.weeks : [],
          totals: body.adherence?.totals ?? {
            started: 0,
            completed: 0,
            cancelled: 0,
            inProgress: 0,
            completionRate: 0,
            weeksInWindow: 0,
            weeksWithTraining: 0
          }
        }
      });
    }
  };
}
