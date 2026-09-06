import type { WorkoutSet } from "@athletica/api-client";
import { useEffect, useState } from "react";
import { Modal, Pressable, StyleSheet, Text, TextInput, View } from "react-native";

import type { SetMeasures } from "./active-workout.ts";

type Draft = { weightKg: string; repetitions: string; rir: string };

const toDraft = (set: WorkoutSet): Draft => ({
  weightKg: String(set.weightKg),
  repetitions: String(set.repetitions),
  rir: String(set.rir)
});

/** Запятая — обычный десятичный разделитель на русской клавиатуре. */
const toNumber = (raw: string): number => Number(raw.replace(",", ".").trim());

export type SetEditorProps = {
  testIDPrefix: string;
  set: WorkoutSet | null;
  busy: boolean;
  onDismiss: () => void;
  /** Возвращает список проблем; пустой список означает, что правка принята. */
  onSave: (patch: SetMeasures) => Promise<string[]>;
};

/**
 * Правка записанного подхода.
 *
 * Меняются только вес, повторы и RIR. Упражнение и номер подхода не
 * редактируются: они образуют идентичность записи в офлайн-очереди, и правка
 * не должна её переписывать — подход, записанный не на то упражнение,
 * удаляется и записывается заново.
 */
export function SetEditor({ testIDPrefix, set, busy, onDismiss, onSave }: SetEditorProps) {
  const [draft, setDraft] = useState<Draft>({ weightKg: "", repetitions: "", rir: "" });
  const [issues, setIssues] = useState<string[]>([]);

  useEffect(() => {
    if (set) {
      setDraft(toDraft(set));
      setIssues([]);
    }
  }, [set]);

  if (!set) return null;

  const field = (key: keyof Draft, label: string, keyboard: "decimal-pad" | "number-pad") => (
    <View style={styles.field}>
      <Text style={styles.kicker}>{label}</Text>
      <TextInput
        testID={`${testIDPrefix}-${key}`}
        style={styles.input}
        value={draft[key]}
        onChangeText={(value) => setDraft((prev) => ({ ...prev, [key]: value }))}
        keyboardType={keyboard}
        editable={!busy}
        accessibilityLabel={label}
      />
    </View>
  );

  return (
    <Modal visible transparent animationType="fade" onRequestClose={onDismiss}>
      <View style={styles.backdrop}>
        <View style={styles.sheet}>
          <Text style={styles.kicker}>ПОДХОД {set.setNumber}</Text>
          <Text style={styles.title}>{set.exerciseId}</Text>

          <View style={styles.fields}>
            {field("weightKg", "ВЕС, КГ", "decimal-pad")}
            {field("repetitions", "ПОВТОРЫ", "number-pad")}
            {field("rir", "RIR", "number-pad")}
          </View>

          {issues.length > 0 ? (
            <Text testID={`${testIDPrefix}-issues`} style={styles.warn}>
              {issues.join(". ")}
            </Text>
          ) : null}

          <Pressable
            testID={`${testIDPrefix}-save`}
            accessibilityRole="button"
            style={styles.action}
            disabled={busy}
            onPress={async () => {
              const patch: SetMeasures = {
                weightKg: toNumber(draft.weightKg),
                repetitions: toNumber(draft.repetitions),
                rir: toNumber(draft.rir)
              };
              setIssues(await onSave(patch));
            }}
          >
            <Text style={styles.actionText}>{busy ? "Сохраняем…" : "Сохранить"}</Text>
          </Pressable>

          <Pressable
            testID={`${testIDPrefix}-cancel`}
            accessibilityRole="button"
            style={styles.secondary}
            disabled={busy}
            onPress={onDismiss}
          >
            <Text style={styles.secondaryText}>Отмена</Text>
          </Pressable>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: { flex: 1, backgroundColor: "#15191899", justifyContent: "flex-end" },
  sheet: { padding: 24, gap: 12, backgroundColor: "#FCFBF8", borderTopLeftRadius: 28, borderTopRightRadius: 28 },
  kicker: { fontSize: 12, fontWeight: "700", color: "#59615D" },
  title: { fontSize: 22, fontWeight: "800", color: "#151918" },
  fields: { flexDirection: "row", gap: 12 },
  field: { flex: 1, gap: 4 },
  input: {
    minHeight: 52,
    borderRadius: 16,
    paddingHorizontal: 14,
    backgroundColor: "#E5ECE9",
    fontSize: 18,
    fontWeight: "700",
    color: "#151918"
  },
  warn: { fontWeight: "700", color: "#C64B2C" },
  action: { minHeight: 52, alignItems: "center", justifyContent: "center", borderRadius: 16, backgroundColor: "#151918" },
  actionText: { color: "#fff", fontWeight: "700" },
  secondary: { minHeight: 48, alignItems: "center", justifyContent: "center", borderRadius: 16 },
  secondaryText: { fontWeight: "700", color: "#59615D" }
});
