export type { ApiConfig, AppEnvironment } from "./config.ts";
export { resolveApiConfig } from "./config.ts";

export type {
  Credentials,
  DatabaseStatus,
  ErrorCode,
  ErrorDetail,
  ErrorEnvelope,
  ErrorPayload,
  Health,
  HealthStatus,
  KnownErrorCode,
  LogSetOutcome,
  Session,
  User,
  Workout,
  WorkoutSet,
  WorkoutSetInput,
  WorkoutStatus,
  AdherenceTotals,
  BestEstimated1Rm,
  BestWeight,
  ExerciseRecord,
  Progress,
  ProgressQuery,
  ProgressWindow,
  WeeklyAdherence,
  WeeklyVolume,
  WorkoutDetail,
  WorkoutListQuery,
  WorkoutPage,
  WorkoutTotals
} from "./types.ts";

export type { ApiError, ApiResult } from "./errors.ts";
export { describeApiError, fail, isRetryable, ok, REDACTED, redactApiError, redactSecrets } from "./errors.ts";

export type { RetryPolicy } from "./retry.ts";
export { backoffDelayMs, DEFAULT_RETRY_POLICY, sleep } from "./retry.ts";

export type {
  FetchLike,
  HttpClient,
  HttpClientDeps,
  HttpClientOptions,
  HttpMethod,
  HttpRequest,
  HttpSuccess
} from "./http.ts";
export { createHttpClient, DEFAULT_TIMEOUT_MS } from "./http.ts";

export type { ApiClient, ApiClientOptions } from "./endpoints.ts";
export { createApiClient } from "./endpoints.ts";

export type {
  AuthClient,
  AuthClientOptions,
  SessionEvent,
  SessionListener,
  SessionStore,
  SignOutReason
} from "./session.ts";
export { createAuthClient } from "./session.ts";
