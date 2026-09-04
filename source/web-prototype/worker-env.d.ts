// Типы биндингов Cloudflare Workers для этого проекта.
//
// Обычно такой файл генерирует `wrangler types`, но конфигурация воркера здесь
// живёт не в `wrangler.jsonc`, а внутри `vite.config.ts` (объект
// `localBindingConfig`), поэтому биндинги описаны вручную. Список должен
// совпадать с `localBindingConfig` в `vite.config.ts` и с `.openai/hosting.json`.
//
// Сами определения типов (`Fetcher`, `D1Database`, модуль `cloudflare:workers`)
// приходят из `@cloudflare/workers-types`, подключённого в `tsconfig.json`.

declare namespace Cloudflare {
  interface Env {
    /** Статические ассеты, которые отдаёт воркер. */
    ASSETS: Fetcher;
    /** D1 присутствует, только если он объявлен в `.openai/hosting.json`. */
    DB?: D1Database;
  }
}
