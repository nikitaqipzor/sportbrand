import { describeApiError } from "@athletica/api-client";
import { useLocalSearchParams, useRouter } from "expo-router";
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";

import { useExerciseCard } from "../../../src/features/catalog/use-catalog.ts";

function Codes({ label, codes, testID }: { label: string; codes: string[]; testID: string }) {
  if (codes.length === 0) return null;
  return (
    <View style={styles.card}>
      <Text style={styles.kicker}>{label}</Text>
      <Text testID={testID}>{codes.join(", ")}</Text>
    </View>
  );
}

export default function ExerciseScreen() {
  const { exerciseId } = useLocalSearchParams<{ exerciseId: string }>();
  const router = useRouter();
  const { card, error, loading, reload } = useExerciseCard(exerciseId);

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content}>
      <Text style={styles.kicker}>УПРАЖНЕНИЕ</Text>

      {loading ? <ActivityIndicator testID="exercise-spinner" /> : null}

      {error ? (
        <View style={styles.card}>
          <Text testID="exercise-error" style={styles.warn}>
            {describeApiError(error)}
          </Text>
          <Pressable testID="exercise-retry" accessibilityRole="button" style={styles.secondary} onPress={() => void reload()}>
            <Text style={styles.secondaryText}>Повторить</Text>
          </Pressable>
        </View>
      ) : null}

      {card ? (
        <>
          <Text testID="exercise-name" style={styles.title}>
            {card.nameRu}
          </Text>
          <Text testID="exercise-classification" style={styles.kicker}>
            {card.section} · {card.sport}
            {card.movementPattern ? ` · ${card.movementPattern}` : ""}
          </Text>

          <Codes label="ОБОРУДОВАНИЕ" codes={card.equipment} testID="exercise-equipment" />
          <Codes label="ОСНОВНЫЕ МЫШЦЫ" codes={card.primaryMuscles} testID="exercise-primary-muscles" />
          <Codes label="ВТОРИЧНЫЕ МЫШЦЫ" codes={card.secondaryMuscles} testID="exercise-secondary-muscles" />

          {/* Техника и безопасность в исходной энциклопедии отсутствуют.
              Показать здесь правдоподобный текст было бы опаснее пустоты:
              по нему человек выполняет движение. */}
          <View style={styles.card}>
            <Text style={styles.kicker}>ТЕХНИКА</Text>
            {card.hasTechnique ? (
              <View testID="exercise-technique" style={styles.steps}>
                {card.technique.setup ? <Text>{card.technique.setup}</Text> : null}
                {card.technique.executionSteps.map((step, index) => (
                  <Text key={step}>
                    {index + 1}. {step}
                  </Text>
                ))}
              </View>
            ) : (
              <Text testID="exercise-technique-absent" style={styles.muted}>
                Пока нет данных. Описание техники проходит экспертную проверку — выдумывать
                его здесь нельзя.
              </Text>
            )}
          </View>

          <View style={styles.card}>
            <Text style={styles.kicker}>БЕЗОПАСНОСТЬ</Text>
            {card.hasSafety ? (
              <View testID="exercise-safety" style={styles.steps}>
                {card.safety.commonErrors.map((item) => (
                  <Text key={item}>· {item}</Text>
                ))}
                {card.safety.contraindications.map((item) => (
                  <Text key={item} style={styles.warn}>
                    · {item}
                  </Text>
                ))}
              </View>
            ) : (
              <Text testID="exercise-safety-absent" style={styles.muted}>
                Типичные ошибки и противопоказания пока не заполнены. Придуманное
                противопоказание хуже отсутствующего.
              </Text>
            )}
          </View>

          {card.difficulty ? (
            <Text testID="exercise-difficulty" style={styles.kicker}>
              УРОВЕНЬ · {card.difficulty}
            </Text>
          ) : (
            <Text testID="exercise-difficulty-absent" style={styles.muted}>
              Уровень не заявлен в источнике
            </Text>
          )}
        </>
      ) : null}

      <Pressable testID="exercise-back" accessibilityRole="button" style={styles.secondary} onPress={() => router.back()}>
        <Text style={styles.secondaryText}>Назад</Text>
      </Pressable>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: "#FCFBF8" },
  content: { padding: 24, gap: 12 },
  kicker: { fontSize: 12, fontWeight: "700", color: "#59615D" },
  title: { fontSize: 28, fontWeight: "800", color: "#151918" },
  muted: { color: "#59615D" },
  warn: { fontWeight: "700", color: "#C64B2C" },
  card: { padding: 20, borderRadius: 24, backgroundColor: "#E5ECE9", gap: 6 },
  steps: { gap: 4 },
  secondary: { minHeight: 48, alignItems: "center", justifyContent: "center", borderRadius: 16 },
  secondaryText: { fontWeight: "700", color: "#59615D" }
});
