import type { ApiError } from "@athletica/api-client";

import type { FieldErrors } from "./validation.ts";

/**
 * Текст для пользователя. Сообщения сервера сюда не подставляются дословно:
 * они на английском и могут содержать эхо запроса, включая токены.
 */
export function authErrorMessage(error: ApiError): string {
  switch (error.kind) {
    case "network":
      return "Нет связи с сервером. Проверьте интернет и попробуйте ещё раз.";
    case "timeout":
      return "Сервер не ответил вовремя. Попробуйте ещё раз.";
    case "session_expired":
      return "Сессия истекла, войдите заново.";
    case "server":
      return "На сервере сбой. Мы уже знаем, попробуйте позже.";
    case "client":
      switch (error.code) {
        case "invalid_credentials":
          return "Неверная почта или пароль.";
        case "email_taken":
          return "Этот адрес нельзя зарегистрировать.";
        case "rate_limited":
          return error.retryAfterSeconds
            ? `Слишком много попыток. Повторите через ${error.retryAfterSeconds} с.`
            : "Слишком много попыток. Повторите позже.";
        case "validation_failed":
          return "Проверьте правильность заполнения полей.";
        case "unauthorized":
          return "Сессия истекла, войдите заново.";
        default:
          return "Запрос отклонён. Проверьте данные и попробуйте снова.";
      }
  }
}

/** Раскладывает details из 422 по полям формы. */
export function fieldErrorsFrom(error: ApiError): FieldErrors {
  if (error.kind !== "client") return {};
  const errors: FieldErrors = {};
  for (const detail of error.details) {
    if (detail.field === "email") errors.email = "Адрес выглядит некорректно";
    if (detail.field === "password") errors.password = "Пароль не соответствует требованиям";
  }
  if (error.code === "invalid_credentials") return {};
  return errors;
}
