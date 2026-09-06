import { createCrashReporter, type CrashReporter } from "./crash-log.ts";
import { createSqliteCrashStore } from "../offline/sqlite.ts";
import { openLocalDatabase } from "../offline/sqlite.ts";

let instance: CrashReporter | null = null;

/** Один журнал на приложение: падения не должны разъезжаться по копиям. */
export function getCrashReporter(): CrashReporter {
  if (instance) return instance;
  instance = createCrashReporter({
    store: {
      list: async () => createSqliteCrashStore(await openLocalDatabase()).list(),
      append: async (record) => createSqliteCrashStore(await openLocalDatabase()).append(record),
      clear: async () => createSqliteCrashStore(await openLocalDatabase()).clear()
    }
  });
  return instance;
}
