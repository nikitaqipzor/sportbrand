import type { SignOutReason } from "@athletica/api-client";

export type SessionCleanupEvent = {
  /** id пользователя, чья сессия закончилась; null — сессии уже не было. */
  userId: string | null;
  reason: SignOutReason;
};

export type SessionCleanupHandler = (event: SessionCleanupEvent) => void | Promise<void>;

const handlers = new Set<SessionCleanupHandler>();

/**
 * ТОЧКА РАСШИРЕНИЯ для соседних модулей (офлайн-очередь, кэш тренировок).
 *
 * Обработчик вызывается один раз при завершении сессии — и по кнопке «Выйти»
 * (reason: "user"), и при неудачном обновлении токена (reason:
 * "refresh_failed"), ПОСЛЕ очистки защищённого хранилища. Возвращает функцию
 * отписки. Ошибка внутри обработчика не ломает выход из аккаунта.
 */
export function registerSessionCleanup(handler: SessionCleanupHandler): () => void {
  handlers.add(handler);
  return () => {
    handlers.delete(handler);
  };
}

/** Вызывается слоем авторизации; прикладному коду напрямую не нужна. */
export async function runSessionCleanup(event: SessionCleanupEvent): Promise<void> {
  for (const handler of [...handlers]) {
    try {
      await handler(event);
    } catch {
      // Очистка чужого модуля не должна мешать выходу из аккаунта.
    }
  }
}

/** Только для тестов: снимает все обработчики. */
export function resetSessionCleanup(): void {
  handlers.clear();
}
