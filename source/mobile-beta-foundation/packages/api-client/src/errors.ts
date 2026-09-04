import type { ErrorCode, ErrorDetail } from "./types.ts";

/**
 * Ошибки — типизированный union, а не брошенные строки: вызывающий код
 * обязан разобрать вариант, а не парсить текст.
 */
export type ApiError =
  | { kind: "network"; message: string; attempts: number }
  | { kind: "timeout"; message: string; timeoutMs: number; attempts: number }
  | {
      kind: "client";
      status: number;
      code: ErrorCode;
      message: string;
      details: ErrorDetail[];
      retryAfterSeconds?: number;
    }
  | { kind: "server"; status: number; code: ErrorCode; message: string; attempts: number }
  /** Refresh не удался — сессия завершена, требуется повторный вход. */
  | { kind: "session_expired"; message: string };

export type ApiResult<T> = { ok: true; value: T } | { ok: false; error: ApiError };

export const ok = <T>(value: T): ApiResult<T> => ({ ok: true, value });
export const fail = <T = never>(error: ApiError): ApiResult<T> => ({ ok: false, error });

/** Ретраить можно только сетевые сбои, таймауты и 5xx. Никогда не 4xx. */
export function isRetryable(error: ApiError): boolean {
  return error.kind === "network" || error.kind === "timeout" || error.kind === "server";
}

export const REDACTED = "[redacted]";

/**
 * Вырезает секреты из любого текста, который может попасть в лог или на
 * экран: сервер (или сетевой стек) способен вернуть эхо заголовка с токеном.
 */
export function redactSecrets(text: string, secrets: readonly (string | null | undefined)[]): string {
  let result = text;
  for (const secret of secrets) {
    if (!secret || secret.length < 8) continue;
    while (result.includes(secret)) result = result.replace(secret, REDACTED);
  }
  return result;
}

/** Та же чистка, но для целой ошибки: сообщения — единственное текстовое поле. */
export function redactApiError(error: ApiError, secrets: readonly (string | null | undefined)[]): ApiError {
  const message = redactSecrets(error.message, secrets);
  if (error.kind === "client") {
    return {
      ...error,
      message,
      details: error.details.map((detail) => ({
        field: detail.field,
        message: redactSecrets(detail.message, secrets)
      }))
    };
  }
  return { ...error, message };
}

/** Человекочитаемое описание без секретов — для UI и отчётов об ошибках. */
export function describeApiError(error: ApiError): string {
  switch (error.kind) {
    case "network":
      return "нет связи с сервером";
    case "timeout":
      return `сервер не ответил за ${error.timeoutMs} мс`;
    case "client":
      return `${error.status} ${error.code}`;
    case "server":
      return `сервер вернул ${error.status}`;
    case "session_expired":
      return "сессия истекла";
  }
}
