import { useRouter } from "expo-router";
import { Pressable, StyleSheet, Text, View } from "react-native";

import { readApiConfig } from "../../src/config/env.ts";
import { useAuth } from "../../src/features/auth/auth-context.tsx";
import { newWorkoutId } from "../../src/features/workout/new-workout-id.ts";
import { totalCompletedSets } from "../../src/features/workout/active-workout.ts";
import { useResumableWorkout, useSyncStatus } from "../../src/features/workout/use-workout.ts";

const api = readApiConfig();

export default function TodayScreen() {
  const { session } = useAuth();
  const router = useRouter();
  const status = useSyncStatus();
  // Приложение могли закрыть посреди тренировки: снимок пережил перезапуск,
  // и вернуться в неё важнее, чем начать новую поверх (П3).
  const { workout: unfinished } = useResumableWorkout();

  const email = session?.user.email ?? "";

  return (
    <View style={styles.screen}>
      <Text style={styles.kicker}>СЕГОДНЯ</Text>
      <Text style={styles.title}>Добрый день</Text>
      <Text testID="today-user" style={styles.kicker}>
        {email}
      </Text>

      {/* Здесь была «ГОТОВНОСТЬ 78» и «сон снизил интенсивность на 5%» —
          числа, которых никто не считал: домена готовности на сервере нет.
          Правдоподобная подделка в фитнес-приложении опаснее пустого места:
          по такому числу человек планирует нагрузку. Вернётся настоящей,
          когда появится домен готовности и восстановления. */}
      <View style={styles.card}>
        <Text style={styles.kicker}>ГОТОВНОСТЬ</Text>
        <Text testID="today-readiness-absent" style={styles.muted}>
          Пока не считаем. Появится, когда подключим сон и восстановление.
        </Text>
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

      {/* Незавершённая тренировка предлагается первой: старт новой затрёт её
          снимок, поэтому кнопка возврата обязана стоять выше. */}
      {unfinished ? (
        <View testID="today-unfinished" style={styles.card}>
          <Text style={styles.kicker}>НЕЗАВЕРШЁННАЯ ТРЕНИРОВКА</Text>
          <Text testID="today-unfinished-title" style={styles.resumeTitle}>
            {unfinished.title || "Силовая тренировка"}
          </Text>
          <Text style={styles.kicker}>
            {unfinished.exercises.length} упражнений · {totalCompletedSets(unfinished)} подходов записано
          </Text>
          <Pressable
            testID="today-resume-workout"
            accessibilityRole="button"
            style={styles.action}
            onPress={() => router.push(`/workout/${unfinished.workoutId}`)}
          >
            <Text style={styles.actionText}>Вернуться в тренировку</Text>
          </Pressable>
        </View>
      ) : null}

      {/* Идентификатор рождается здесь, до всякой сети: тренировку можно
          начать в зале без связи, и подходам будет к чему привязаться. */}
      <Pressable
        testID="today-start-workout"
        accessibilityRole="button"
        style={unfinished ? styles.secondary : styles.action}
        onPress={() => router.push(`/workout/${newWorkoutId()}`)}
      >
        <Text style={unfinished ? styles.secondaryText : styles.actionText}>
          {unfinished ? "Начать новую тренировку" : "Начать силовую тренировку"}
        </Text>
      </Pressable>

      <Pressable
        testID="today-open-history"
        accessibilityRole="button"
        style={styles.secondary}
        onPress={() => router.push("/history")}
      >
        <Text style={styles.secondaryText}>История тренировок</Text>
      </Pressable>

      <Pressable
        testID="today-open-progress"
        accessibilityRole="button"
        style={styles.secondary}
        onPress={() => router.push("/progress")}
      >
        <Text style={styles.secondaryText}>Прогресс</Text>
      </Pressable>

      <Pressable
        testID="today-open-profile"
        accessibilityRole="button"
        style={styles.secondary}
        onPress={() => router.push("/profile")}
      >
        <Text style={styles.secondaryText}>Профиль</Text>
      </Pressable>

      <Text style={styles.kicker}>
        API · {api.environment.toUpperCase()} · {api.baseUrl}
      </Text>

    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, padding: 24, gap: 16, backgroundColor: "#FCFBF8" },
  kicker: { fontSize: 12, fontWeight: "700", color: "#59615D" },
  title: { fontSize: 32, fontWeight: "800", color: "#151918" },
  card: { padding: 20, borderRadius: 24, backgroundColor: "#E5ECE9", gap: 4 },
  score: { fontSize: 56, fontWeight: "800", color: "#151918" },
  resumeTitle: { fontSize: 22, fontWeight: "800", color: "#151918" },
  warn: { fontWeight: "700", color: "#C64B2C" },
  muted: { color: "#59615D" },
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
