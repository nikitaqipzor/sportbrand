import { describeApiError, type WorkoutSet } from "@athletica/api-client";
import { useLocalSearchParams, useRouter } from "expo-router";
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";

import { useState } from "react";

import { useWorkoutSummary } from "../../../src/features/history/use-history.ts";
import { ConfirmDialog } from "../../../src/features/workout/confirm-dialog.tsx";
import { SetEditor } from "../../../src/features/workout/set-editor.tsx";
import { useSetCorrections, useSyncStatus } from "../../../src/features/workout/use-workout.ts";

const STATUS_LABEL: Record<string, string> = {
  active: "идёт",
  paused: "на паузе",
  completed: "завершена",
  cancelled: "отменена"
};

const kg = (value: number): string =>
  Number.isInteger(value) ? String(value) : value.toFixed(1).replace(".", ",");

function formatTime(iso: string | null | undefined): string {
  if (!iso) return "—";
  const at = new Date(iso);
  return `${String(at.getHours()).padStart(2, "0")}:${String(at.getMinutes()).padStart(2, "0")}`;
}

export default function SummaryScreen() {
  const { workoutId } = useLocalSearchParams<{ workoutId: string }>();
  const router = useRouter();
  const { data, error, loading, reload } = useWorkoutSummary(workoutId);
  const sync = useSyncStatus();
  const corrections = useSetCorrections();
  const [editing, setEditing] = useState<WorkoutSet | null>(null);
  const [deleting, setDeleting] = useState<WorkoutSet | null>(null);

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content}>
      <Text style={styles.kicker}>ИТОГИ ТРЕНИРОВКИ</Text>

      {loading ? <ActivityIndicator testID="summary-spinner" /> : null}

      {error ? (
        <View style={styles.card}>
          <Text testID="summary-error" style={styles.warn}>
            {describeApiError(error)}
          </Text>
          <Pressable testID="summary-retry" accessibilityRole="button" style={styles.secondary} onPress={() => void reload()}>
            <Text style={styles.secondaryText}>Повторить</Text>
          </Pressable>
        </View>
      ) : null}

      {/* Подходы могут ещё ждать отправки: тогда суммы сервера ниже реальных,
          и об этом честнее сказать, чем показать неверный итог как истину. */}
      {sync.pending > 0 ? (
        <Text testID="summary-pending" style={styles.warn}>
          Ждут отправки: {sync.pending}. Итоги обновятся после синхронизации.
        </Text>
      ) : null}

      {data ? (
        <>
          <Text testID="summary-title" style={styles.title}>
            {data.title || "Без названия"}
          </Text>
          <Text testID="summary-status" style={styles.kicker}>
            {(STATUS_LABEL[data.status] ?? data.status).toUpperCase()} · {formatTime(data.createdAt)} —{" "}
            {formatTime(data.endedAt)}
          </Text>

          <View style={styles.totals}>
            <View style={styles.total}>
              <Text style={styles.kicker}>ПОДХОДЫ</Text>
              <Text testID="summary-total-sets" style={styles.metric}>
                {data.totals.sets}
              </Text>
            </View>
            <View style={styles.total}>
              <Text style={styles.kicker}>ПОВТОРЫ</Text>
              <Text testID="summary-total-reps" style={styles.metric}>
                {data.totals.repetitions}
              </Text>
            </View>
            <View style={styles.total}>
              <Text style={styles.kicker}>ОБЪЁМ</Text>
              <Text testID="summary-total-volume" style={styles.metric}>
                {kg(data.totals.volumeKg)}
              </Text>
              <Text style={styles.kicker}>кг</Text>
            </View>
          </View>

          {data.sets.length === 0 ? (
            <Text testID="summary-empty" style={styles.muted}>
              В этой тренировке не записано ни одного подхода.
            </Text>
          ) : (
            data.sets.map((set) => (
              <View key={set.id} testID={`summary-set-${set.setNumber}`} style={styles.setRow}>
                <Text style={styles.setNumber}>{set.setNumber}</Text>
                <View style={styles.setBody}>
                  <Text style={styles.setMain}>
                    {kg(set.weightKg)} кг · {set.repetitions} повт · RIR {set.rir}
                  </Text>
                  <Text style={styles.kicker}>
                    {set.exerciseId} · {formatTime(set.createdAt)}
                  </Text>
                </View>
                {/* Опечатку замечают именно здесь, поэтому править нужно
                    отсюда же, а не искать другой экран. */}
                <Pressable
                  testID={`summary-edit-set-${set.setNumber}`}
                  accessibilityRole="button"
                  accessibilityLabel={`Исправить подход ${set.setNumber}`}
                  style={styles.rowAction}
                  onPress={() => setEditing(set)}
                >
                  <Text style={styles.rowActionText}>Исправить</Text>
                </Pressable>
                <Pressable
                  testID={`summary-delete-set-${set.setNumber}`}
                  accessibilityRole="button"
                  accessibilityLabel={`Удалить подход ${set.setNumber}`}
                  style={styles.rowAction}
                  onPress={() => setDeleting(set)}
                >
                  <Text style={[styles.rowActionText, styles.warn]}>Удалить</Text>
                </Pressable>
              </View>
            ))
          )}
        </>
      ) : null}

      <SetEditor
        testIDPrefix="summary-editor"
        set={editing}
        busy={corrections.busy}
        onDismiss={() => setEditing(null)}
        onSave={async (patch) => {
          if (!editing || !workoutId) return [];
          const issues = await corrections.editSet(workoutId, editing.id, patch);
          if (issues.length === 0) {
            setEditing(null);
            // Правка уехала в очередь; сервер согласится позже, поэтому
            // перечитываем, а не подменяем значение на экране.
            await reload();
          }
          return issues;
        }}
      />

      <ConfirmDialog
        testIDPrefix="summary-delete-confirm"
        visible={deleting !== null}
        title="Удалить подход?"
        message={
          deleting
            ? `Подход ${deleting.setNumber}: ${kg(deleting.weightKg)} кг × ${deleting.repetitions}. Он перестанет учитываться в прогрессе.`
            : ""
        }
        confirmLabel="Удалить"
        onConfirm={async () => {
          const target = deleting;
          setDeleting(null);
          if (!target || !workoutId) return;
          await corrections.deleteSet(workoutId, target.id);
          await reload();
        }}
        onDismiss={() => setDeleting(null)}
      />

      <Pressable testID="summary-open-progress" accessibilityRole="button" style={styles.action} onPress={() => router.push("/progress")}>
        <Text style={styles.actionText}>К прогрессу</Text>
      </Pressable>
      <Pressable testID="summary-home" accessibilityRole="button" style={styles.secondary} onPress={() => router.replace("/")}>
        <Text style={styles.secondaryText}>На главную</Text>
      </Pressable>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: "#FCFBF8" },
  content: { padding: 24, gap: 12 },
  kicker: { fontSize: 12, fontWeight: "700", color: "#59615D" },
  title: { fontSize: 30, fontWeight: "800", color: "#151918" },
  muted: { color: "#59615D" },
  warn: { fontWeight: "700", color: "#C64B2C" },
  card: { padding: 20, borderRadius: 24, backgroundColor: "#E5ECE9", gap: 8 },
  totals: { flexDirection: "row", gap: 12 },
  total: { flex: 1, padding: 16, borderRadius: 20, backgroundColor: "#E5ECE9", gap: 2 },
  metric: { fontSize: 28, fontWeight: "800", color: "#151918" },
  setRow: { flexDirection: "row", gap: 12, alignItems: "center", padding: 14, borderRadius: 16, backgroundColor: "#F1EFE9" },
  setNumber: { width: 28, textAlign: "center", fontWeight: "800", color: "#59615D" },
  setBody: { flex: 1, gap: 2 },
  setMain: { fontWeight: "700", color: "#151918" },
  rowAction: { minHeight: 44, paddingHorizontal: 8, justifyContent: "center" },
  rowActionText: { fontSize: 12, fontWeight: "700", color: "#59615D" },
  action: { minHeight: 52, alignItems: "center", justifyContent: "center", borderRadius: 16, backgroundColor: "#151918" },
  actionText: { color: "#fff", fontWeight: "700" },
  secondary: { minHeight: 48, alignItems: "center", justifyContent: "center", borderRadius: 16 },
  secondaryText: { fontWeight: "700", color: "#59615D" }
});
