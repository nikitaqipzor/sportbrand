export type AppEnvironment = "development" | "staging" | "production";

export type ApiConfig = { environment: AppEnvironment; baseUrl: string };

export function resolveApiConfig(input: { environment?: string; baseUrl?: string }): ApiConfig {
  const environment = input.environment === "production" || input.environment === "staging" ? input.environment : "development";
  const fallback = environment === "development" ? "http://10.0.2.2:8080/api/v1" : "";
  const baseUrl = input.baseUrl?.trim() || fallback;
  if (!baseUrl) throw new Error(`ATHLETICA_API_URL is required for ${environment}`);
  if (environment === "production" && !baseUrl.startsWith("https://")) throw new Error("production API must use HTTPS");
  return { environment, baseUrl: baseUrl.replace(/\/$/, "") };
}
