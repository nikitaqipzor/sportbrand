import { useRouter } from "expo-router";
import { useEffect, useState } from "react";
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native";

import { readApiConfig } from "../../src/config/env.ts";
import { useAuth } from "../../src/features/auth/auth-context.tsx";
import { ConfirmDialog } from "../../src/features/workout/confirm-dialog.tsx";
import { useSyncStatus } from "../../src/features/workout/use-workout.ts";
import type { CrashRecord } from "../../src/platform/diagnostics/crash-log.ts";
import { getCrashReporter } from "../../src/platform/diagnostics/runtime.ts";

const api = readApiConfig();

export default function ProfileScreen() {
  const { session, signOut } = useAuth();
  const router = useRouter();
  const sync = useSyncStatus();
  const [confirmSignOut, setConfirmSignOut] = useState(false);
  const [crashes, setCrashes] = useState<CrashRecord[]>([]);

  useEffect(() => {
    let alive = true;
    void getCrashReporter()
      .recent()
      .then((records) => {
        if (alive) setCrashes(records);
      });
    return () => {
      alive = false;
    };
  }, []);

  const email = session?.user.email ?? "";

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content}>
      <Text style={styles.kicker}>ПРОФИЛЬ</Text>
      <Text testID="profile-email" style={styles.title}>
        {email}
      </Text>

      {/* Состояние синхронизации живёт здесь, а не только на «Сегодня»:
          человек приходит сюда именно тогда, когда что-то пошло не так. */}
      <View style={styles.card}>
        <Text style={styles.kicker}>СИНХРОНИЗАЦИЯ</Text>
        <Text testID="profile-sync-pending">Ждут отправки: {sync.pending}</Text>
        {sync.dead > 0 ? (
          <Text testID="profile-sync-dead" style={styles.warn}>
            Отклонено сервером: {sync.dead}
          </Text>
        ) : null}
        {sync.paused ? (
          <Text testID="profile-sync-paused" style={styles.warn}>
            Отправка на паузе до входа
          </Text>
        ) : null}
        {sync.lastFailure ? (
          <Text testID="profile-sync-failure" style={styles.muted}>
            Последняя ошибка: {sync.lastFailure}
          </Text>
        ) : null}
      </View>

      <View style={styles.card}>
        <Text style={styles.kicker}>ПОДКЛЮЧЕНИЕ</Text>
        <Text testID="profile-api" style={styles.muted}>
          {api.environment.toUpperCase()} · {api.baseUrl}
        </Text>
      </View>

      {/* Разделы прототипа, которых пока нет. Названы честно: пустое место
          лучше кнопки, которая ничего не делает. */}
      {/* Журнал падений живёт здесь, потому что приёмника отчётов у проекта
          пока нет: отправить некуда, но показать человеку — можно и нужно. */}
      <View style={styles.card}>
        <Text style={styles.kicker}>ЖУРНАЛ ОШИБОК</Text>
        {crashes.length === 0 ? (
          <Text testID="profile-crashes-empty" style={styles.muted}>
            Падений не записано
          </Text>
        ) : (
          <>
            <Text testID="profile-crashes-count" style={styles.warn}>
              Записано падений: {crashes.length}
            </Text>
            {crashes.slice(0, 3).map((crash) => (
              <Text key={crash.id} testID={`profile-crash-${crash.id}`} style={styles.muted}>
                {crash.at.slice(0, 16).replace("T", " ")} · {crash.scope} · {crash.message}
              </Text>
            ))}
            <Pressable
              testID="profile-crashes-clear"
              accessibilityRole="button"
              style={styles.secondary}
              onPress={() => {
                void getCrashReporter()
                  .clear()
                  .then(() => setCrashes([]));
              }}
            >
              <Text style={styles.secondaryText}>Очистить журнал</Text>
            </Pressable>
          </>
        )}
      </View>

      <View style={styles.card}>
        <Text style={styles.kicker}>ПОКА НЕДОСТУПНО</Text>
        <Text testID="profile-pending-sections" style={styles.muted}>
          Цели и виды спорта, ограничения, устройства и подписка появятся вместе
          с соответствующими разделами.
        </Text>
      </View>

      <Pressable
        testID="profile-open-history"
        accessibilityRole="button"
        style={styles.action}
        onPress={() => router.push("/history")}
      >
        <Text style={styles.actionText}>История тренировок</Text>
      </Pressable>

      <Pressable
        testID="profile-sign-out"
        accessibilityRole="button"
        style={styles.secondary}
        onPress={() => setConfirmSignOut(true)}
      >
        <Text style={styles.secondaryText}>Выйти из аккаунта</Text>
      </Pressable>

      <Pressable
        testID="profile-home"
        accessibilityRole="button"
        style={styles.secondary}
        onPress={() => router.replace("/")}
      >
        <Text style={styles.secondaryText}>На главную</Text>
      </Pressable>

      <ConfirmDialog
        testIDPrefix="profile-sign-out-confirm"
        visible={confirmSignOut}
        title="Выйти из аккаунта?"
        message={
          sync.pending > 0
            ? `Неотправленных подходов: ${sync.pending}. Они будут удалены с устройства вместе с активной тренировкой.`
            : "Активная тренировка и очередь будут удалены с устройства."
        }
        confirmLabel="Выйти"
        onConfirm={() => {
          setConfirmSignOut(false);
          void signOut();
        }}
        onDismiss={() => setConfirmSignOut(false)}
      />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: "#FCFBF8" },
  content: { padding: 24, gap: 12 },
  kicker: { fontSize: 12, fontWeight: "700", color: "#59615D" },
  title: { fontSize: 26, fontWeight: "800", color: "#151918" },
  muted: { color: "#59615D" },
  warn: { fontWeight: "700", color: "#C64B2C" },
  card: { padding: 20, borderRadius: 24, backgroundColor: "#E5ECE9", gap: 4 },
  action: { minHeight: 52, alignItems: "center", justifyContent: "center", borderRadius: 16, backgroundColor: "#151918" },
  actionText: { color: "#fff", fontWeight: "700" },
  secondary: { minHeight: 48, alignItems: "center", justifyContent: "center", borderRadius: 16 },
  secondaryText: { fontWeight: "700", color: "#59615D" }
});
