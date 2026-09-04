import * as SecureStore from "expo-secure-store";

import type { SecureStorage } from "./secure-storage.ts";

/**
 * expo-secure-store: Android Keystore / iOS Keychain. Единственный файл в
 * проекте, который импортирует нативный модуль, — поэтому всё остальное
 * (включая тесты) работает через интерфейс SecureStorage.
 */
export function createExpoSecureStorage(): SecureStorage {
  const options: SecureStore.SecureStoreOptions = {
    keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY
  };
  return {
    getItem: (key) => SecureStore.getItemAsync(key, options),
    setItem: (key, value) => SecureStore.setItemAsync(key, value, options),
    removeItem: (key) => SecureStore.deleteItemAsync(key, options)
  };
}
