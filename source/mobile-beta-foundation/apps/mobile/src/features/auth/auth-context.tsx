import type { ApiError, AuthClient, Credentials, Session } from "@athletica/api-client";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { getCrashReporter } from "../../platform/diagnostics/runtime.ts";
import { getAuthClient } from "./auth-client.ts";

export type AuthStatus = "loading" | "signed-out" | "signed-in";

export type AuthState = {
  status: AuthStatus;
  session: Session | null;
  /** true, пока выполняется вход или регистрация. */
  pending: boolean;
  error: ApiError | null;
};

export type AuthContextValue = AuthState & {
  /**
   * Клиент с автообновлением токена. Экраны чтения ходят через него, а не
   * собирают свой: обновление сессии обязано оставаться single-flight.
   */
  client: AuthClient;
  signIn: (credentials: Credentials) => Promise<boolean>;
  signUp: (credentials: Credentials) => Promise<boolean>;
  signOut: () => Promise<void>;
  clearError: () => void;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children, client }: { children: ReactNode; client?: AuthClient }): ReactNode {
  const auth = useRef<AuthClient | null>(client ?? null);
  auth.current ??= getAuthClient();
  const authClient = auth.current;

  const [state, setState] = useState<AuthState>({
    status: "loading",
    session: null,
    pending: false,
    error: null
  });

  useEffect(() => {
    let alive = true;
    // Поднимаем сессию из Keystore до первого кадра навигации.
    //
    // Отказ обязан быть обработан. Без catch любая ошибка чтения Keystore
    // оставляла статус в "loading" навсегда: заставка «Восстанавливаем
    // сессию…» накрывала приложение, и до экрана входа было не добраться —
    // приложение выглядело намертво зависшим при запуске.
    void authClient
      .restore()
      .then((session) => {
        if (alive) {
          setState((prev) => ({ ...prev, status: session ? "signed-in" : "signed-out", session }));
        }
      })
      .catch((error: unknown) => {
        if (!alive) return;
        void getCrashReporter().capture("auth-restore", error);
        // Нет доступа к хранилищу — значит сессии нет. Вход доступен.
        setState((prev) => ({ ...prev, status: "signed-out", session: null }));
      });
    // Сессия может закончиться и без действий пользователя: неудачный refresh
    // посреди тренировки обязан довести до экрана входа, а не молча упасть.
    const unsubscribe = authClient.subscribe((event) => {
      if (!alive) return;
      if (event.type === "signed_out") {
        setState((prev) => ({
          ...prev,
          status: "signed-out",
          session: null,
          pending: false,
          error: event.reason === "refresh_failed" ? (event.error ?? prev.error) : null
        }));
        return;
      }
      setState((prev) => ({ ...prev, status: "signed-in", session: event.session }));
    });
    return () => {
      alive = false;
      unsubscribe();
    };
  }, [authClient]);

  const run = useCallback(
    async (call: () => Promise<{ ok: true; value: Session } | { ok: false; error: ApiError }>) => {
      setState((prev) => ({ ...prev, pending: true, error: null }));
      const result = await call();
      if (result.ok) {
        setState({ status: "signed-in", session: result.value, pending: false, error: null });
        return true;
      }
      setState((prev) => ({ ...prev, pending: false, error: result.error }));
      return false;
    },
    []
  );

  const value = useMemo<AuthContextValue>(
    () => ({
      ...state,
      client: authClient,
      signIn: (credentials) => run(() => authClient.signIn(credentials)),
      signUp: (credentials) => run(() => authClient.signUp(credentials)),
      signOut: async () => {
        await authClient.signOut("user");
      },
      clearError: () => setState((prev) => ({ ...prev, error: null }))
    }),
    [state, run, authClient]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext);
  if (!value) throw new Error("useAuth должен вызываться внутри <AuthProvider>");
  return value;
}
