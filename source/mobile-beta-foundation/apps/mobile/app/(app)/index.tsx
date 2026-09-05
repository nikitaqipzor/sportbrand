import { useRouter } from "expo-router";
import { useState } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";

import { readApiConfig } from "../../src/config/env.ts";
import { useAuth } from "../../src/features/auth/auth-context.tsx";
import { ConfirmDialog } from "../../src/features/workout/confirm-dialog.tsx";
import { newWorkoutId } from "../../src/features/workout/new-workout-id.ts";
import { useSyncStatus } from "../../src/features/workout/use-workout.ts";

const api = readApiConfig();

export default function TodayScreen() {
  const { session, signOut } = useAuth();
  const router = useRouter();
  const status = useSyncStatus();
  const [confirmSignOut, setConfirmSignOut] = useState(false);

  const email = session?.user.email ?? "";

  return (
    <View style={styles.screen}>
      <Text style={styles.kicker}>СЕГОДНЯ</Text>
      <Text style={styles.title}>Добрый день</Text>
      <Text testID="today-user" style={styles.kicker}>
        {email}
      </Text>

      <View style={styles.card}>
        <Text style={styles.kicker}>ГОТОВНОСТЬ</Text>
        <Text style={styles.score}>78</Text>
        <Text>Сон снизил интенсивность на 5%</Text>
      </View>

      <View style={styles.card}>
        <Text style={styles.kicker}>СИНХРОНИЗАЦИЯ</Text>
        <Text testID="today-sync-pending">Ждут отправки: {status.pending}</Text>
        {status.dead > 0 ? (
          <Text testID="today-sync-dead" style={styles.warn}>
            Отклонено сервером: {status.dead}
          </Text>
        ) : null}
        {status.paused ? (
          <Text testID="today-sync-paused" style={styles.warn}>
            Отправка на паузе до входа
          </Text>
        ) : null}
      </View>

      {/* Идентификатор рождается здесь, до всякой сети: тренировку можно
          начать в зале без связи, и подходам будет к чему привязаться. */}
      <Pressable
        testID="today-start-workout"
        accessibilityRole="button"
        style={styles.action}
        onPress={() => router.push(`/workout/${newWorkoutId()}`)}
      >
        <Text style={styles.actionText}>Начать силовую тренировку</Text>
      </Pressable>

      <Pressable
        testID="today-open-progress"
        accessibilityRole="button"
        style={styles.secondary}
        onPress={() => router.push("/progress")}
      >
        <Text style={styles.secondaryText}>Прогресс и история</Text>
      </Pressable>

      <Pressable
        testID="today-sign-out"
        accessibilityRole="button"
        style={styles.secondary}
        onPress={() => setConfirmSignOut(true)}
      >
        <Text style={styles.secondaryText}>Выйти из аккаунта</Text>
      </Pressable>

      <Text style={styles.kicker}>
        API · {api.environment.toUpperCase()} · {api.baseUrl}
      </Text>

      <ConfirmDialog
        testIDPrefix="today-sign-out-confirm"
        visible={confirmSignOut}
        title="Выйти из аккаунта?"
        message="Неотправленные подходы и активная тренировка будут удалены с устройства."
        confirmLabel="Выйти"
        onConfirm={() => {
          setConfirmSignOut(false);
          void signOut();
        }}
        onDismiss={() => setConfirmSignOut(false)}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, padding: 24, gap: 16, backgroundColor: "#FCFBF8" },
  kicker: { fontSize: 12, fontWeight: "700", color: "#59615D" },
  title: { fontSize: 32, fontWeight: "800", color: "#151918" },
  card: { padding: 20, borderRadius: 24, backgroundColor: "#E5ECE9", gap: 4 },
  score: { fontSize: 56, fontWeight: "800", color: "#151918" },
  warn: { fontWeight: "700", color: "#C64B2C" },
  action: {
    minHeight: 52,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 16,
    backgroundColor: "#151918"
  },
  actionText: { color: "#fff", fontWeight: "700" },
  secondary: { minHeight: 48, alignItems: "center", justifyContent: "center", borderRadius: 16 },
  secondaryText: { fontWeight: "700", color: "#59615D" }
});
