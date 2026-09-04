import type { WorkoutSetInput } from "@athletica/domain";
import { useLocalSearchParams } from "expo-router";
import { useState } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";

import { logSet } from "../../../src/features/workout/log-set.ts";
import { itemsForUser, type OutboxItem } from "../../../src/platform/offline/outbox.ts";

// Заглушка до появления сессии/авторизации (следующий спринт).
const CURRENT_USER_ID = "local-user";
const EXERCISE_ID = "lat-pulldown";

export default function WorkoutScreen() {
  const { workoutId } = useLocalSearchParams<{ workoutId: string }>();
  const [outbox, setOutbox] = useState<OutboxItem<WorkoutSetInput>[]>([]);
  const [issues, setIssues] = useState<string[]>([]);

  const pending = itemsForUser(outbox, CURRENT_USER_ID);
  const setNumber = pending.length + 1;

  function completeSet() {
    const input: WorkoutSetInput = {
      workoutId: workoutId ?? "unknown",
      exerciseId: EXERCISE_ID,
      setNumber,
      weightKg: 62.5,
      repetitions: 10,
      rir: 2,
      clientMutationId: `${workoutId}:${EXERCISE_ID}:${setNumber}`
    };
    const result = logSet(outbox, CURRENT_USER_ID, input);
    if (!result.ok) {
      setIssues(result.issues);
      return;
    }
    setIssues([]);
    setOutbox(result.outbox);
  }

  return (
    <View style={styles.screen}>
      <Text style={styles.kicker}>АКТИВНАЯ ТРЕНИРОВКА · {workoutId}</Text>
      <Text style={styles.title}>Тяга верхнего блока</Text>
      <View style={styles.timer}>
        <Text style={styles.kicker}>ОТДЫХ</Text>
        <Text style={styles.time}>01:18</Text>
      </View>
      <View style={styles.set}>
        <Text style={styles.setText}>Подход {setNumber} · 62,5 кг · 10 повторов · RIR 2</Text>
      </View>
      <Text style={styles.kicker}>В ОЧЕРЕДИ НА СИНХРОНИЗАЦИЮ: {pending.length}</Text>
      {issues.length > 0 ? <Text style={styles.error}>{issues.join(" · ")}</Text> : null}
      <Pressable style={styles.action} onPress={completeSet}>
        <Text style={styles.actionText}>Завершить подход</Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, padding: 24, gap: 16, backgroundColor: "#101413" },
  kicker: { fontSize: 12, fontWeight: "700", color: "#AAB3AE" },
  title: { fontSize: 32, fontWeight: "800", color: "#F8F7F3" },
  timer: { padding: 20, borderRadius: 24, backgroundColor: "#1A201E" },
  time: { fontSize: 56, fontWeight: "800", color: "#F8F7F3" },
  set: { padding: 20, borderRadius: 18, backgroundColor: "#1A201E" },
  setText: { color: "#F8F7F3" },
  error: { color: "#F0A08C", fontWeight: "700" },
  action: {
    minHeight: 56,
    borderRadius: 16,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "#C64B2C"
  },
  actionText: { fontWeight: "800", color: "#fff" }
});
