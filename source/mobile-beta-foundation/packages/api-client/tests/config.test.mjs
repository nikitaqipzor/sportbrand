import test from "node:test";
import assert from "node:assert/strict";

import { resolveApiConfig } from "../src/index.ts";

test("подставляет dev-fallback, если baseUrl не задан", () => {
  assert.deepEqual(resolveApiConfig({}), {
    environment: "development",
    baseUrl: "http://10.0.2.2:8080/api/v1"
  });
});

test("неизвестное окружение схлопывается в development", () => {
  assert.equal(resolveApiConfig({ environment: "qa" }).environment, "development");
  assert.equal(resolveApiConfig({ environment: undefined }).environment, "development");
});

test("staging и production сохраняются как есть", () => {
  assert.equal(
    resolveApiConfig({ environment: "staging", baseUrl: "http://stage.local/api" }).environment,
    "staging"
  );
  assert.equal(
    resolveApiConfig({ environment: "production", baseUrl: "https://api.athletica.ai" }).environment,
    "production"
  );
});

test("production отклоняет URL без HTTPS", () => {
  assert.throws(
    () => resolveApiConfig({ environment: "production", baseUrl: "http://api.athletica.ai" }),
    /production API must use HTTPS/
  );
  assert.throws(
    () => resolveApiConfig({ environment: "production", baseUrl: "ws://api.athletica.ai" }),
    /production API must use HTTPS/
  );
});

test("production принимает HTTPS", () => {
  assert.equal(
    resolveApiConfig({ environment: "production", baseUrl: "https://api.athletica.ai/v1" }).baseUrl,
    "https://api.athletica.ai/v1"
  );
});

test("режет хвостовой слэш", () => {
  assert.equal(
    resolveApiConfig({ environment: "production", baseUrl: "https://api.athletica.ai/v1/" }).baseUrl,
    "https://api.athletica.ai/v1"
  );
  assert.equal(resolveApiConfig({ baseUrl: "http://10.0.2.2:8080/api/v1/" }).baseUrl, "http://10.0.2.2:8080/api/v1");
});

test("пустой baseUrl падает для staging и production", () => {
  assert.throws(
    () => resolveApiConfig({ environment: "staging" }),
    /ATHLETICA_API_URL is required for staging/
  );
  assert.throws(
    () => resolveApiConfig({ environment: "production", baseUrl: "   " }),
    /ATHLETICA_API_URL is required for production/
  );
});
