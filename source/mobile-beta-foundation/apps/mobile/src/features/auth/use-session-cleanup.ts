import { useEffect } from "react";

import { registerSessionCleanup, type SessionCleanupHandler } from "./session-cleanup.ts";

/**
 * React-обёртка над registerSessionCleanup: подписка живёт столько же,
 * сколько компонент. Ссылку на обработчик стабилизируйте через useCallback.
 */
export function useSessionCleanup(handler: SessionCleanupHandler): void {
  useEffect(() => registerSessionCleanup(handler), [handler]);
}
