/**
 * Клиентская валидация повторяет границы контракта (Credentials в
 * openapi.yaml): адрес до 254 символов, пароль 10..72 БАЙТ — bcrypt считает
 * байты, поэтому кириллический пароль обрезается вдвое раньше.
 */
export const EMAIL_MAX_LENGTH = 254;
export const PASSWORD_MIN_BYTES = 10;
export const PASSWORD_MAX_BYTES = 72;

const EMAIL_PATTERN = /^[^\s@]+@[^\s@.]+(\.[^\s@.]+)+$/;

export const byteLength = (value: string): number => new TextEncoder().encode(value).length;

export function validateEmail(value: string): string | null {
  const email = value.trim();
  if (!email) return "Введите адрес электронной почты";
  if (email.length > EMAIL_MAX_LENGTH) return `Адрес длиннее ${EMAIL_MAX_LENGTH} символов`;
  if (!EMAIL_PATTERN.test(email)) return "Адрес выглядит некорректно";
  return null;
}

export function validatePassword(value: string): string | null {
  const size = byteLength(value);
  if (size === 0) return "Введите пароль";
  if (size < PASSWORD_MIN_BYTES) return `Пароль короче ${PASSWORD_MIN_BYTES} символов`;
  if (size > PASSWORD_MAX_BYTES) return `Пароль длиннее ${PASSWORD_MAX_BYTES} символов`;
  return null;
}

export type FieldErrors = { email?: string; password?: string };

export function validateCredentials(input: { email: string; password: string }): FieldErrors {
  const errors: FieldErrors = {};
  const email = validateEmail(input.email);
  if (email) errors.email = email;
  const password = validatePassword(input.password);
  if (password) errors.password = password;
  return errors;
}

export const hasErrors = (errors: FieldErrors): boolean => Object.keys(errors).length > 0;
