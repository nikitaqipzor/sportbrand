import { Modal, Pressable, StyleSheet, Text, View } from "react-native";
import type { ReactNode } from "react";

/**
 * Подтверждение разрушительного действия (QA-009): завершение и отмена
 * тренировки, выход из аккаунта. Не Alert.alert, а собственное окно —
 * у каждого элемента должен быть testID, иначе действие не проверить в тестах.
 */
export type ConfirmDialogProps = {
  testIDPrefix: string;
  visible: boolean;
  title: string;
  message: string;
  confirmLabel: string;
  cancelLabel?: string;
  onConfirm: () => void;
  onDismiss: () => void;
};

export function ConfirmDialog({
  testIDPrefix,
  visible,
  title,
  message,
  confirmLabel,
  cancelLabel = "Назад",
  onConfirm,
  onDismiss
}: ConfirmDialogProps): ReactNode {
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onDismiss}>
      <View testID={`${testIDPrefix}-backdrop`} style={styles.backdrop}>
        <View style={styles.card}>
          <Text style={styles.title}>{title}</Text>
          <Text style={styles.message}>{message}</Text>
          <Pressable
            testID={`${testIDPrefix}-confirm`}
            accessibilityRole="button"
            style={styles.confirm}
            onPress={onConfirm}
          >
            <Text style={styles.confirmText}>{confirmLabel}</Text>
          </Pressable>
          <Pressable
            testID={`${testIDPrefix}-cancel`}
            accessibilityRole="button"
            style={styles.cancel}
            onPress={onDismiss}
          >
            <Text style={styles.cancelText}>{cancelLabel}</Text>
          </Pressable>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: { flex: 1, padding: 24, justifyContent: "center", backgroundColor: "rgba(10,12,11,0.6)" },
  card: { padding: 24, gap: 12, borderRadius: 24, backgroundColor: "#FCFBF8" },
  title: { fontSize: 20, fontWeight: "800", color: "#151918" },
  message: { fontSize: 14, color: "#59615D" },
  confirm: {
    minHeight: 52,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 16,
    backgroundColor: "#C64B2C"
  },
  confirmText: { color: "#fff", fontWeight: "800" },
  cancel: { minHeight: 48, alignItems: "center", justifyContent: "center", borderRadius: 16 },
  cancelText: { color: "#59615D", fontWeight: "700" }
});
