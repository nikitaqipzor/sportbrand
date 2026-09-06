import { Stack, useRouter, useSegments } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { useEffect } from "react";
import { ActivityIndicator, StyleSheet, Text, View } from "react-native";

import { AuthProvider, useAuth } from "../src/features/auth/auth-context.tsx";
import { ErrorBoundary } from "../src/platform/diagnostics/error-boundary.tsx";

/**
 * Гейт авторизации. Пока сессия поднимается из Keystore — заставка;
 * неавторизованный пользователь уходит на вход из любой ветки, авторизованный
 * — внутрь приложения. Сюда же приводит завершение сессии после неудачного
 * обновления токена (QA-001).
 */
function AuthGate() {
  const { status } = useAuth();
  const segments = useSegments();
  const router = useRouter();

  useEffect(() => {
    if (status === "loading") return;
    const inAuthGroup = segments[0] === "(auth)";
    if (status === "signed-out" && !inAuthGroup) router.replace("/sign-in");
    if (status === "signed-in" && inAuthGroup) router.replace("/");
  }, [status, segments, router]);

  return (
    <>
      <Stack screenOptions={{ headerShown: false }} />
      {status === "loading" ? (
        <View testID="auth-splash" style={styles.splash}>
          <ActivityIndicator color="#151918" />
          <Text style={styles.splashText}>Восстанавливаем сессию…</Text>
        </View>
      ) : null}
    </>
  );
}

export default function RootLayout() {
  return (
    // Граница снаружи провайдера: падение самого провайдера тоже обязано быть
    // поймано, иначе приложение просто исчезнет с экрана.
    <ErrorBoundary scope="root">
      <AuthProvider>
        <StatusBar style="auto" />
        <AuthGate />
      </AuthProvider>
    </ErrorBoundary>
  );
}

const styles = StyleSheet.create({
  splash: {
    ...StyleSheet.absoluteFillObject,
    alignItems: "center",
    justifyContent: "center",
    gap: 12,
    backgroundColor: "#FCFBF8"
  },
  splashText: { fontSize: 13, fontWeight: "700", color: "#59615D" }
});
