/**
 * Узкий интерфейс защищённого хранилища. Реализация на устройстве — Keystore
 * через expo-secure-store; в тестах подставляется объект в памяти.
 *
 * Здесь лежат ТОЛЬКО секреты (access и refresh). В AsyncStorage они не
 * попадают никогда, в логи и в тексты ошибок — тоже: значения не печатаются.
 */
export type SecureStorage = {
  getItem: (key: string) => Promise<string | null>;
  setItem: (key: string, value: string) => Promise<void>;
  removeItem: (key: string) => Promise<void>;
};

/** Подменяемая реализация для тестов и для web, где Keystore недоступен. */
export function createMemorySecureStorage(seed: Record<string, string> = {}): SecureStorage {
  const values = new Map<string, string>(Object.entries(seed));
  return {
    getItem: async (key) => values.get(key) ?? null,
    setItem: async (key, value) => {
      values.set(key, value);
    },
    removeItem: async (key) => {
      values.delete(key);
    }
  };
}
