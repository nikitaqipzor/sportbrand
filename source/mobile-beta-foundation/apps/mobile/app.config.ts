import type { ConfigContext, ExpoConfig } from "expo/config";

/**
 * app.json остаётся статической базой, а этот файл пробрасывает переменные
 * окружения из .env (см. .env.example) в manifest, откуда их читает
 * expo-constants в рантайме.
 */
export default ({ config }: ConfigContext): ExpoConfig => ({
  ...config,
  name: config.name ?? "Athletica AI",
  slug: config.slug ?? "athletica-ai",
  extra: {
    ...config.extra,
    athleticaApiUrl: process.env.ATHLETICA_API_URL ?? "",
    athleticaEnv: process.env.ATHLETICA_ENV ?? "development"
  }
});
