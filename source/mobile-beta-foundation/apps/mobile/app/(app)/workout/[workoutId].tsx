import { useLocalSearchParams, useRouter } from "expo-router";
import { useEffect, useMemo, useState } from "react";
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native";

import { formatClock, restSeconds } from "../../../src/features/workout/active-workout.ts";
import { ConfirmDialog } from "../../../src/features/workout/confirm-dialog.tsx";
import { useActiveWorkout, type SetMeasures } from "../../../src/features/workout/use-workout.ts";

const EXERCISE_ID = "lat-pulldown";
const EXERCISE_TITLE = "Тяга верхнего блока";

type Pending = "complete" | "cancel" | null;

export default function WorkoutScreen() {
  const params = useLocalSearchParams<{ workoutId: string }>();
  const workoutId = params.workoutId ?? "demo-strength";
  const router = useRouter();

  const start = useMemo(
    () => ({ workoutId, title: EXERCISE_TITLE, exerciseId: EXERCISE_ID }),
    [workoutId]
  );
  const { workout, setNumber, status, queue, issues, busy, recordSet, finish, syncNow } = useActiveWorkout(start);

  const [measures, setMeasures] = useState<SetMeasures>({ weightKg: 62.5, repetitions: 10, rir: 2 });
  const [pending, setPending] = useState<Pending>(null);
  const [tick, setTick] = useState(0);

  // Таймер отдыха идёт от последнего записанного подхода, а не от константы.
  useEffect(() => {
    const timer = setInterval(() => setTick((value) => value + 1), 1000);
    return () => clearInterval(timer);
  }, []);
  const rest = workout ? formatClock(restSeconds(workout)) : "00:00";
  void tick;

  const step = (field: keyof SetMeasures, delta: number): void =>
    setMeasures((prev) => ({ ...prev, [field]: Math.max(0, Math.round((prev[field] + delta) * 10) / 10) }));

  const dead = queue.filter((record) => record.state === "dead");

  async function applyFinish(action: "complete" | "cancel"): Promise<void> {
    setPending(null);
    await finish(action);
    router.replace("/");
  }

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content}>
      <Text style={styles.kicker}>АКТИВНАЯ ТРЕНИРОВКА · {workoutId}</Text>
      <Text style={styles.title}>{EXERCISE_TITLE}</Text>

      <View style={styles.timer}>
        <Text style={styles.kicker}>ОТДЫХ</Text>
        <Text testID="workout-rest-timer" style={styles.time}>
          {rest}
        </Text>
      </View>

      <View style={styles.set}>
        <Text testID="workout-set-summary" style={styles.setText}>
          Подход {setNumber} · {measures.weightKg} кг · {measures.repetitions} повторов · RIR {measures.rir}
        </Text>
        <View style={styles.steppers}>
          <Stepper
            testID="workout-weight"
            label="Вес"
            onDecrease={() => step("weightKg", -2.5)}
            onIncrease={() => step("weightKg", 2.5)}
          />
          <Stepper
            testID="workout-reps"
            label="Повторы"
            onDecrease={() => step("repetitions", -1)}
            onIncrease={() => step("repetitions", 1)}
          />
          <Stepper
            testID="workout-rir"
            label="RIR"
            onDecrease={() => step("rir", -1)}
            onIncrease={() => step("rir", 1)}
          />
        </View>
      </View>

      <View style={styles.sync}>
        <Text testID="workout-sync-pending" style={styles.kicker}>
          В ОЧЕРЕДИ НА СИНХРОНИЗАЦИЮ: {status.pending}
        </Text>
        {status.dead > 0 ? (
          <Text testID="workout-sync-dead" style={styles.error}>
            Отклонено сервером: {status.dead}
            {dead[0]?.failure ? ` · ${dead[0].failure}` : ""}
          </Text>
        ) : null}
        {status.paused ? (
          <Text testID="workout-sync-paused" style={styles.error}>
            Сессия истекла — отправка возобновится после входа
          </Text>
        ) : null}
        {status.lastFailure && !status.paused ? (
          <Text testID="workout-sync-failure" style={styles.warn}>
            Последняя ошибка: {status.lastFailure}
          </Text>
        ) : null}
        <Pressable
          testID="workout-sync-now"
          accessibilityRole="button"
          style={styles.secondary}
          onPress={() => void syncNow()}
        >
          <Text style={styles.secondaryText}>{status.syncing ? "Синхронизируем…" : "Синхронизировать сейчас"}</Text>
        </Pressable>
      </View>

      {issues.length > 0 ? (
        <Text testID="workout-issues" style={styles.error}>
          {issues.join(" · ")}
        </Text>
      ) : null}

      <Pressable
        testID="workout-complete-set"
        accessibilityRole="button"
        accessibilityState={{ disabled: busy, busy }}
        disabled={busy}
        style={[styles.action, busy ? styles.actionBusy : null]}
        onPress={() => void recordSet(measures)}
      >
        <Text style={styles.actionText}>Завершить подход</Text>
      </Pressable>

      <Pressable
        testID="workout-finish"
        accessibilityRole="button"
        style={styles.finish}
        onPress={() => setPending("complete")}
      >
        <Text style={styles.finishText}>Завершить тренировку</Text>
      </Pressable>

      <Pressable
        testID="workout-cancel"
        accessibilityRole="button"
        style={styles.secondary}
        onPress={() => setPending("cancel")}
      >
        <Text style={styles.secondaryText}>Отменить тренировку</Text>
      </Pressable>

      <ConfirmDialog
        testIDPrefix="workout-finish-confirm"
        visible={pending === "complete"}
        title="Завершить тренировку?"
        message="Записанные подходы останутся и досинхронизируются с сервером."
        confirmLabel="Завершить"
        onConfirm={() => void applyFinish("complete")}
        onDismiss={() => setPending(null)}
      />

      <ConfirmDialog
        testIDPrefix="workout-cancel-confirm"
        visible={pending === "cancel"}
        title="Отменить тренировку?"
        message="Неотправленные подходы этой тренировки будут удалены и не попадут в статистику."
        confirmLabel="Отменить тренировку"
        onConfirm={() => void applyFinish("cancel")}
        onDismiss={() => setPending(null)}
      />
    </ScrollView>
  );
}

function Stepper({
  testID,
  label,
  onDecrease,
  onIncrease
}: {
  testID: string;
  label: string;
  onDecrease: () => void;
  onIncrease: () => void;
}) {
  return (
    <View style={styles.stepper}>
      <Text style={styles.kicker}>{label}</Text>
      <View style={styles.stepperRow}>
        <Pressable
          testID={`${testID}-minus`}
          accessibilityRole="button"
          accessibilityLabel={`${label}: меньше`}
          style={styles.stepButton}
          onPress={onDecrease}
        >
          <Text style={styles.stepText}>−</Text>
        </Pressable>
        <Pressable
          testID={`${testID}-plus`}
          accessibilityRole="button"
          accessibilityLabel={`${label}: больше`}
          style={styles.stepButton}
          onPress={onIncrease}
        >
          <Text style={styles.stepText}>+</Text>
        </Pressable>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: "#101413" },
  content: { padding: 24, gap: 16 },
  kicker: { fontSize: 12, fontWeight: "700", color: "#AAB3AE" },
  title: { fontSize: 32, fontWeight: "800", color: "#F8F7F3" },
  timer: { padding: 20, borderRadius: 24, backgroundColor: "#1A201E" },
  time: { fontSize: 56, fontWeight: "800", color: "#F8F7F3" },
  set: { padding: 20, borderRadius: 18, backgroundColor: "#1A201E", gap: 12 },
  setText: { color: "#F8F7F3" },
  steppers: { flexDirection: "row", gap: 12 },
  stepper: { flex: 1, gap: 6 },
  stepperRow: { flexDirection: "row", gap: 8 },
  stepButton: {
    flex: 1,
    minHeight: 44,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 12,
    backgroundColor: "#2A322F"
  },
  stepText: { color: "#F8F7F3", fontSize: 18, fontWeight: "800" },
  sync: { gap: 6 },
  error: { color: "#F0A08C", fontWeight: "700" },
  warn: { color: "#D9C48A", fontWeight: "700" },
  action: {
    minHeight: 56,
    borderRadius: 16,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "#C64B2C"
  },
  actionBusy: { backgroundColor: "#7C3A26" },
  actionText: { fontWeight: "800", color: "#fff" },
  finish: {
    minHeight: 52,
    borderRadius: 16,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "#2A322F"
  },
  finishText: { fontWeight: "800", color: "#F8F7F3" },
  secondary: { minHeight: 48, alignItems: "center", justifyContent: "center", borderRadius: 16 },
  secondaryText: { fontWeight: "700", color: "#AAB3AE" }
});
