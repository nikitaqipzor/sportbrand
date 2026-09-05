import { describeApiError } from "@athletica/api-client";
import { useRouter } from "expo-router";
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";

import { useProgress } from "../../src/features/history/use-history.ts";

const kg = (value: number): string =>
  Number.isInteger(value) ? String(value) : value.toFixed(1).replace(".", ",");

const percent = (rate: number): string => `${Math.round(rate * 100)}%`;

const weekLabel = (iso: string): string => {
  const at = new Date(iso);
  return `${String(at.getUTCDate()).padStart(2, "0")}.${String(at.getUTCMonth() + 1).padStart(2, "0")}`;
};

/**
 * Столбик недельного объёма. Значения нормируются к максимуму окна: важна
 * форма динамики, а абсолютные килограммы стоят рядом цифрой.
 */
function VolumeBar({ value, max }: { value: number; max: number }) {
  const share = max > 0 ? Math.max(0.04, value / max) : 0;
  return (
    <View style={styles.barTrack}>
      <View style={[styles.barFill, { width: `${share * 100}%` }]} />
    </View>
  );
}

export default function ProgressScreen() {
  const router = useRouter();
  const { data, error, loading, reload } = useProgress();

  const maxVolume = data ? Math.max(0, ...data.weeklyVolume.map((week) => week.volumeKg)) : 0;
  const nothingYet = data && data.strength.length === 0 && data.weeklyVolume.length === 0;

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content}>
      <Text style={styles.kicker}>ПРОГРЕСС</Text>
      <Text style={styles.title}>Что изменилось</Text>

      {loading ? <ActivityIndicator testID="progress-spinner" /> : null}

      {error ? (
        <View style={styles.card}>
          <Text testID="progress-error" style={styles.warn}>
            {describeApiError(error)}
          </Text>
          <Pressable testID="progress-retry" accessibilityRole="button" style={styles.secondary} onPress={() => void reload()}>
            <Text style={styles.secondaryText}>Повторить</Text>
          </Pressable>
        </View>
      ) : null}

      {/* Пустой прогресс — обычное состояние нового аккаунта, а не ошибка. */}
      {nothingYet ? (
        <View style={styles.card}>
          <Text testID="progress-empty" style={styles.muted}>
            Пока нечего показать. Проведи первую тренировку — рекорды и объём появятся здесь.
          </Text>
        </View>
      ) : null}

      {data && !nothingYet ? (
        <>
          <View style={styles.card}>
            <Text style={styles.kicker}>СОБЛЮДЕНИЕ</Text>
            <Text testID="progress-completion" style={styles.metric}>
              {percent(data.adherence.totals.completionRate)}
            </Text>
            <Text style={styles.muted}>
              Доведено до конца {data.adherence.totals.completed} из {data.adherence.totals.started} начатых
            </Text>
            <Text testID="progress-weeks" style={styles.kicker}>
              Тренировки были в {data.adherence.totals.weeksWithTraining} из {data.adherence.totals.weeksInWindow} недель
            </Text>
          </View>

          {data.weeklyVolume.length > 0 ? (
            <View style={styles.card}>
              <Text style={styles.kicker}>ОБЪЁМ ПО НЕДЕЛЯМ</Text>
              {data.weeklyVolume.map((week) => (
                <View key={week.weekStart} testID={`progress-week-${week.weekStart}`} style={styles.weekRow}>
                  <Text style={styles.weekLabel}>{weekLabel(week.weekStart)}</Text>
                  <VolumeBar value={week.volumeKg} max={maxVolume} />
                  <Text style={styles.weekValue}>{kg(week.volumeKg)} кг</Text>
                </View>
              ))}
              <Text style={styles.kicker}>Недели без тренировок в ответе отсутствуют</Text>
            </View>
          ) : null}

          {data.strength.length > 0 ? (
            <View style={styles.card}>
              <Text style={styles.kicker}>РЕКОРДЫ</Text>
              {data.strength.map((record) => (
                <View key={record.exerciseId} testID={`progress-exercise-${record.exerciseId}`} style={styles.record}>
                  <Text style={styles.recordName}>{record.exerciseId}</Text>
                  <Text style={styles.recordMain}>
                    {kg(record.bestWeight.weightKg)} кг × {record.bestWeight.repetitions}
                  </Text>
                  {/* Оценка Эпли: weightKg × (1 + повторы / 30). Это расчёт, а
                      не поднятый вес, поэтому подписано отдельно. */}
                  <Text style={styles.kicker}>
                    Оценка 1ПМ {kg(record.bestEstimated1Rm.estimated1RmKg)} кг · объём {kg(record.volumeKg)} кг ·{" "}
                    {record.sets} подходов
                  </Text>
                </View>
              ))}
            </View>
          ) : null}
        </>
      ) : null}

      <Pressable testID="progress-home" accessibilityRole="button" style={styles.secondary} onPress={() => router.replace("/")}>
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
  card: { padding: 20, borderRadius: 24, backgroundColor: "#E5ECE9", gap: 6 },
  metric: { fontSize: 44, fontWeight: "800", color: "#151918" },
  weekRow: { flexDirection: "row", alignItems: "center", gap: 10 },
  weekLabel: { width: 46, fontWeight: "700", color: "#59615D", fontSize: 12 },
  weekValue: { width: 84, textAlign: "right", fontWeight: "700", color: "#151918", fontSize: 12 },
  barTrack: { flex: 1, height: 10, borderRadius: 5, backgroundColor: "#CFDAD4", overflow: "hidden" },
  barFill: { height: 10, borderRadius: 5, backgroundColor: "#151918" },
  record: { paddingVertical: 8, gap: 2 },
  recordName: { fontWeight: "800", color: "#151918" },
  recordMain: { fontWeight: "700", color: "#151918" },
  secondary: { minHeight: 48, alignItems: "center", justifyContent: "center", borderRadius: 16 },
  secondaryText: { fontWeight: "700", color: "#59615D" }
});
