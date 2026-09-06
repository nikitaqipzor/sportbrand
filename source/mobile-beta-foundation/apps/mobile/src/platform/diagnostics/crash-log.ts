/**
 * Журнал падений на устройстве.
 *
 * Внешнего приёмника отчётов у проекта пока нет: аккаунта Sentry или
 * Crashlytics не заведено, а притворяться, что отчёты куда-то уходят, хуже,
 * чем честно хранить их локально. Журнал лежит на телефоне, виден в
 * «Профиле» и может быть скопирован тестировщиком вручную. Интерфейс
 * `CrashReporter` оставлен так, чтобы настоящий приёмник подключился одной
 * реализацией, не трогая места вызова.
 */
export type CrashRecord = {
  id: string;
  at: string;
  /** Где случилось: экран, фоновая задача, синхронизация. */
  scope: string;
  message: string;
  stack: string | null;
};

/** Сколько записей храним. Журнал не должен расти без границ на устройстве. */
export const CRASH_LOG_LIMIT = 20;

/**
 * Секреты не попадают в журнал.
 *
 * Отчёт о падении — самое соблазнительное место для утечки: в стеке и в
 * сообщении легко оказывается токен, тело запроса или адрес почты. Токена под
 * рукой в момент падения может не быть, поэтому вырезаем по форме, а не по
 * значению.
 */
const PATTERNS: { pattern: RegExp; replacement: string }[] = [
  // JWT: три части base64url через точку.
  { pattern: /\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\b/g, replacement: "[токен]" },
  { pattern: /\bBearer\s+[A-Za-z0-9._~+/=-]{8,}/gi, replacement: "Bearer [токен]" },
  { pattern: /[\w.+-]+@[\w-]+\.[\w.-]+/g, replacement: "[почта]" },
  // Длинные непрерывные base64/hex — обычно refresh-токен или ключ.
  { pattern: /\b[A-Za-z0-9_-]{40,}\b/g, replacement: "[секрет]" }
];

export function redactCrashText(text: string): string {
  return PATTERNS.reduce((acc, { pattern, replacement }) => acc.replace(pattern, replacement), text);
}

/** Стек обрезаем: длинный хвост ничего не добавляет, а место занимает. */
export const STACK_LINES = 12;

export function toCrashRecord(scope: string, error: unknown, at: Date, id: string): CrashRecord {
  const raw = error instanceof Error ? error : new Error(String(error));
  const stack = raw.stack ? raw.stack.split("\n").slice(0, STACK_LINES).join("\n") : null;
  return {
    id,
    at: at.toISOString(),
    scope: redactCrashText(scope),
    message: redactCrashText(raw.message || "неизвестная ошибка"),
    stack: stack === null ? null : redactCrashText(stack)
  };
}

/** Новые записи впереди; старые вытесняются за границей лимита. */
export function appendCrash(log: CrashRecord[], record: CrashRecord, limit = CRASH_LOG_LIMIT): CrashRecord[] {
  return [record, ...log.filter((entry) => entry.id !== record.id)].slice(0, limit);
}

export type CrashStore = {
  list: () => Promise<CrashRecord[]>;
  append: (record: CrashRecord) => Promise<void>;
  clear: () => Promise<void>;
};

export type CrashMemoryDb = { records: CrashRecord[] };

export const createCrashMemoryDb = (): CrashMemoryDb => ({ records: [] });

export function createMemoryCrashStore(db: CrashMemoryDb = createCrashMemoryDb(), limit = CRASH_LOG_LIMIT): CrashStore {
  return {
    list: async () => [...db.records],
    append: async (record) => {
      db.records = appendCrash(db.records, record, limit);
    },
    clear: async () => {
      db.records = [];
    }
  };
}

export type CrashReporter = {
  capture: (scope: string, error: unknown) => Promise<void>;
  recent: () => Promise<CrashRecord[]>;
  clear: () => Promise<void>;
};

export function createCrashReporter(deps: {
  store: CrashStore;
  now?: () => Date;
  id?: () => string;
}): CrashReporter {
  const now = deps.now ?? (() => new Date());
  let counter = 0;
  const id = deps.id ?? (() => `crash-${Date.now()}-${(counter += 1)}`);

  return {
    capture: async (scope, error) => {
      // Падение при записи падения не должно ронять приложение второй раз.
      try {
        await deps.store.append(toCrashRecord(scope, error, now(), id()));
      } catch {
        /* журнал недоступен — молчим, это не повод потерять исходную ошибку */
      }
    },
    recent: () => deps.store.list(),
    clear: () => deps.store.clear()
  };
}
