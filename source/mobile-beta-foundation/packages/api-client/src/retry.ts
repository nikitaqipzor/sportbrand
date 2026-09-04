export type RetryPolicy = {
  /** Полное число попыток, включая первую. */
  maxAttempts: number;
  /** Задержка перед первым ретраем, мс. */
  baseDelayMs: number;
  /** Потолок задержки, мс. */
  maxDelayMs: number;
  /** Множитель экспоненты. */
  factor: number;
};

export const DEFAULT_RETRY_POLICY: RetryPolicy = {
  maxAttempts: 3,
  baseDelayMs: 300,
  maxDelayMs: 4000,
  factor: 2
};

/**
 * Экспоненциальный backoff с полным джиттером: задержка равномерно
 * распределена в [0, min(base * factor^(attempt-1), maxDelay)]. Джиттер
 * нужен, чтобы очередь не отправила всё разом после восстановления сети.
 */
export function backoffDelayMs(attempt: number, policy: RetryPolicy, random: () => number = Math.random): number {
  const exponent = Math.max(0, attempt - 1);
  const ceiling = Math.min(policy.maxDelayMs, policy.baseDelayMs * policy.factor ** exponent);
  return Math.round(Math.min(1, Math.max(0, random())) * ceiling);
}

export const sleep = (ms: number): Promise<void> =>
  new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
