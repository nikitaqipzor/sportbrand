import { resolveApiConfig, type ApiConfig } from "@athletica/api-client";
import Constants from "expo-constants";

type AthleticaExtra = { athleticaApiUrl?: string; athleticaEnv?: string };

/**
 * ATHLETICA_API_URL / ATHLETICA_ENV: .env -> app.config.ts -> manifest extra
 * -> expo-constants -> resolveApiConfig.
 */
export function readApiConfig(): ApiConfig {
  const extra = (Constants.expoConfig?.extra ?? {}) as AthleticaExtra;
  return resolveApiConfig({ environment: extra.athleticaEnv, baseUrl: extra.athleticaApiUrl });
}
