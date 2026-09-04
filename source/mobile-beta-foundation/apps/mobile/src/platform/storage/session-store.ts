import type { Session, SessionStore } from "@athletica/api-client";

import type { SecureStorage } from "./secure-storage.ts";

/** v1 в ключе: сменится форма сессии — старое значение просто не прочитается. */
export const SESSION_STORAGE_KEY = "athletica.session.v1";

const isSession = (value: unknown): value is Session => {
  if (typeof value !== "object" || value === null) return false;
  const candidate = value as Partial<Session> & { user?: { id?: unknown } };
  return (
    typeof candidate.accessToken === "string" &&
    candidate.accessToken.length > 0 &&
    typeof candidate.refreshToken === "string" &&
    candidate.refreshToken.length > 0 &&
    typeof candidate.user?.id === "string"
  );
};

/**
 * SessionStore для @athletica/api-client поверх защищённого хранилища.
 * Битое или устаревшее значение молча удаляется: пользователь увидит экран
 * входа, а не падение при старте.
 */
export function createSecureSessionStore(storage: SecureStorage): SessionStore {
  return {
    load: async () => {
      const raw = await storage.getItem(SESSION_STORAGE_KEY);
      if (!raw) return null;
      try {
        const parsed: unknown = JSON.parse(raw);
        if (isSession(parsed)) return parsed;
      } catch {
        // повреждённая запись — ниже удаляем
      }
      await storage.removeItem(SESSION_STORAGE_KEY);
      return null;
    },
    save: async (session) => {
      await storage.setItem(SESSION_STORAGE_KEY, JSON.stringify(session));
    },
    clear: async () => {
      await storage.removeItem(SESSION_STORAGE_KEY);
    }
  };
}
