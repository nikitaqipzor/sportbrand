import { createAuthClient, type AuthClient, type SessionStore } from "@athletica/api-client";

import { readApiConfig } from "../../config/env.ts";
import { createExpoSecureStorage, createSecureSessionStore } from "../../platform/storage/index.ts";
import { runSessionCleanup } from "./session-cleanup.ts";

/**
 * Единственный экземпляр на приложение: single-flight обновление токена имеет
 * смысл только тогда, когда все запросы идут через один клиент.
 */
let instance: AuthClient | null = null;

export function createAppAuthClient(store: SessionStore): AuthClient {
  const client = createAuthClient({ config: readApiConfig(), store });
  client.subscribe((event) => {
    if (event.type === "signed_out") {
      void runSessionCleanup({ userId: event.userId, reason: event.reason });
    }
  });
  return client;
}

export function getAuthClient(): AuthClient {
  instance ??= createAppAuthClient(createSecureSessionStore(createExpoSecureStorage()));
  return instance;
}
