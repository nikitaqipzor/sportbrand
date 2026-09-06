import type { ApiConfig } from "./config.ts";
import { fail, ok, redactApiError, type ApiError, type ApiResult } from "./errors.ts";
import { backoffDelayMs, DEFAULT_RETRY_POLICY, sleep, type RetryPolicy } from "./retry.ts";
import type { ErrorCode, ErrorDetail, ErrorEnvelope } from "./types.ts";

export type FetchLike = (input: string, init: RequestInit) => Promise<Response>;

export type HttpMethod = "GET" | "POST" | "PATCH" | "DELETE";

export type HttpRequest = {
  method: HttpMethod;
  /** Путь относительно baseUrl, начинается со слэша: "/auth/login". */
  path: string;
  body?: unknown;
  headers?: Record<string, string>;
  /** Таймаут этого запроса; по умолчанию — таймаут клиента. */
  timeoutMs?: number;
  /** Статусы, которые вызывающий считает успехом помимо 2xx (например 409). */
  acceptStatuses?: readonly number[];
  /** Отключает ретраи для неидемпотентных запросов. */
  retry?: boolean;
  signal?: AbortSignal;
};

export type HttpSuccess = { status: number; body: unknown };

export type HttpClientDeps = {
  fetch?: FetchLike;
  /** Задержка между попытками; подменяется в тестах. */
  sleep?: (ms: number) => Promise<void>;
  random?: () => number;
  /** Секреты, которые нельзя показывать в тексте ошибок. */
  secrets?: () => readonly (string | null | undefined)[];
};

export type HttpClientOptions = {
  config: ApiConfig;
  timeoutMs?: number;
  retryPolicy?: RetryPolicy;
} & HttpClientDeps;

export const DEFAULT_TIMEOUT_MS = 15_000;

export type HttpClient = {
  request: (spec: HttpRequest) => Promise<ApiResult<HttpSuccess>>;
  readonly baseUrl: string;
};

const isErrorEnvelope = (body: unknown): body is ErrorEnvelope =>
  typeof body === "object" &&
  body !== null &&
  "error" in body &&
  typeof (body as { error: unknown }).error === "object" &&
  (body as { error: unknown }).error !== null;

function payloadOf(body: unknown): { code: ErrorCode; message: string; details: ErrorDetail[] } {
  if (isErrorEnvelope(body)) {
    const payload = body.error;
    return {
      code: typeof payload.code === "string" ? payload.code : "internal_error",
      message: typeof payload.message === "string" ? payload.message : "",
      details: Array.isArray(payload.details) ? payload.details : []
    };
  }
  return { code: "internal_error", message: "", details: [] };
}

function retryAfterOf(response: Response): number | undefined {
  const raw = response.headers?.get?.("retry-after");
  if (!raw) return undefined;
  const seconds = Number.parseInt(raw, 10);
  return Number.isFinite(seconds) && seconds >= 0 ? seconds : undefined;
}

async function readBody(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) return null;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return text;
  }
}

/**
 * Единственное место, где проект ходит в сеть: таймаут на каждый запрос,
 * ретраи только на сетевые сбои и 5xx, ошибки — типизированный union.
 */
export function createHttpClient(options: HttpClientOptions): HttpClient {
  const doFetch: FetchLike = options.fetch ?? ((input, init) => globalThis.fetch(input, init));
  const wait = options.sleep ?? sleep;
  const random = options.random ?? Math.random;
  const policy = options.retryPolicy ?? DEFAULT_RETRY_POLICY;
  const defaultTimeout = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const secretsOf = options.secrets ?? (() => []);
  const baseUrl = options.config.baseUrl;

  async function attempt(spec: HttpRequest, attemptNumber: number): Promise<ApiResult<HttpSuccess>> {
    const timeoutMs = spec.timeoutMs ?? defaultTimeout;
    const controller = new AbortController();
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, timeoutMs);
    const onExternalAbort = () => controller.abort();
    spec.signal?.addEventListener("abort", onExternalAbort);

    try {
      const headers: Record<string, string> = { accept: "application/json", ...spec.headers };
      const init: RequestInit = { method: spec.method, headers, signal: controller.signal };
      if (spec.body !== undefined) {
        headers["content-type"] = "application/json";
        init.body = JSON.stringify(spec.body);
      }

      const response = await doFetch(`${baseUrl}${spec.path}`, init);
      const body = await readBody(response);
      const accepted = spec.acceptStatuses ?? [];

      if ((response.status >= 200 && response.status < 300) || accepted.includes(response.status)) {
        return ok({ status: response.status, body });
      }

      const payload = payloadOf(body);
      if (response.status >= 500) {
        return fail({
          kind: "server",
          status: response.status,
          code: payload.code,
          message: payload.message || `server error ${response.status}`,
          attempts: attemptNumber
        });
      }
      return fail({
        kind: "client",
        status: response.status,
        code: payload.code,
        message: payload.message || `request rejected with ${response.status}`,
        details: payload.details,
        retryAfterSeconds: retryAfterOf(response)
      });
    } catch (cause) {
      if (timedOut) {
        return fail({
          kind: "timeout",
          message: `request timed out after ${timeoutMs}ms`,
          timeoutMs,
          attempts: attemptNumber
        });
      }
      const reason = cause instanceof Error ? cause.message : String(cause);
      return fail({ kind: "network", message: reason || "network request failed", attempts: attemptNumber });
    } finally {
      clearTimeout(timer);
      spec.signal?.removeEventListener("abort", onExternalAbort);
    }
  }

  async function request(spec: HttpRequest): Promise<ApiResult<HttpSuccess>> {
    const retriesAllowed = spec.retry !== false;
    const maxAttempts = retriesAllowed ? Math.max(1, policy.maxAttempts) : 1;
    let last: ApiResult<HttpSuccess> = fail({ kind: "network", message: "no attempt was made", attempts: 0 });

    for (let attemptNumber = 1; attemptNumber <= maxAttempts; attemptNumber += 1) {
      last = await attempt(spec, attemptNumber);
      if (last.ok) return last;
      const error: ApiError = last.error;
      // 4xx — окончательный ответ сервера, повтор ничего не изменит.
      const retryable = error.kind === "network" || error.kind === "timeout" || error.kind === "server";
      if (!retryable || attemptNumber === maxAttempts || spec.signal?.aborted) break;
      await wait(backoffDelayMs(attemptNumber, policy, random));
    }

    if (!last.ok) return fail(redactApiError(last.error, secretsOf()));
    return last;
  }

  return { request, baseUrl };
}
